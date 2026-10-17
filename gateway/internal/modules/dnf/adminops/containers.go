package adminops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var ErrContainerInput = errors.New("invalid container operation")
var ErrContainerMissing = errors.New("container state missing")
var ErrContainerDowngrade = errors.New("container shrink requires inventory migration")

type ContainerCommand struct {
	AccountID   string         `json:"accountId"`
	CharacterID string         `json:"characterId"`
	Targets     map[string]int `json:"targets"`
	Revision    string         `json:"revision"`
	Reason      string         `json:"reason"`
	Actor       string         `json:"actor"`
	PlanToken   string         `json:"planToken,omitempty"`
	Key         string         `json:"-"`
}
type ContainerChange struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Unit   string `json:"unit"`
	Before int    `json:"before"`
	After  int    `json:"after"`
}
type ContainerReceipt struct {
	RequestID   string            `json:"requestId,omitempty"`
	Kind        string            `json:"kind"`
	AccountID   string            `json:"accountId"`
	CharacterID string            `json:"characterId"`
	Changes     []ContainerChange `json:"changes"`
	Revision    string            `json:"revision"`
	Reason      string            `json:"reason"`
	Actor       string            `json:"actor"`
	CreatedAt   time.Time         `json:"createdAt"`
}
type containerStored struct {
	Receipt     ContainerReceipt `json:"receipt"`
	ContentHash string           `json:"contentHash"`
}

func ContainerRevision(s repository.ContainerManagementState) string {
	raw, _ := json.Marshal([]any{s.Character.CharacterID, s.Character.AccountID, s.Character.Stats["delete_flag"], s.Found, s.Settings.Scope, s.Settings.UpdatedAt.UTC(), s.Settings.Values})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ContainerIdentity(c ContainerCommand) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ContainerValues(s repository.ContainerManagementState) (map[string]int, error) {
	if !s.Found {
		return nil, ErrContainerMissing
	}
	state, err := repository.CharacterContainerStateFromSettings(s.Settings, s.Character.CharacterID)
	if err != nil {
		return nil, err
	}
	if _, err = containerSequence(s.Settings); err != nil {
		return nil, err
	}
	return map[string]int{"main": int(state.MainSlotCount), "avatars": int(state.AvatarExpansion), "warehouse": int(state.PersonalCargoSlotCount)}, nil
}
func containerSequence(s repository.SettingsRecord) (int64, error) {
	raw, exists := s.Values[repository.ContainerManagementSequence]
	if !exists {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || strconv.FormatInt(n, 10) != raw {
		return 0, repository.ErrCharacterContainerStateInvalid
	}
	return n, nil
}
func validContainerCommand(c ContainerCommand, commit bool) error {
	id, e := strconv.ParseUint(c.CharacterID, 10, 16)
	if e != nil || id == 0 || strconv.FormatUint(id, 10) != c.CharacterID || c.AccountID == "" || c.Actor == "" || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 500 || len(c.Targets) == 0 || len(c.Targets) > 3 || len(c.Revision) != 64 {
		return ErrContainerInput
	}
	if _, e = hex.DecodeString(c.Revision); e != nil {
		return ErrContainerInput
	}
	for id, value := range c.Targets {
		valid := false
		for _, spec := range repository.ManagedContainers() {
			if id == spec.ID {
				for _, n := range spec.Values {
					if n == value {
						valid = true
					}
				}
			}
		}
		if !valid {
			return ErrContainerInput
		}
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128) {
		return ErrContainerInput
	}
	return nil
}
func loadContainerReceipt(ctx context.Context, r repository.ManagementReceiptStore, c ContainerCommand) (ContainerReceipt, bool, error) {
	var old containerStored
	raw, found, e := r.Load(ctx, c.Key)
	if e != nil || !found {
		return old.Receipt, found, e
	}
	if e = json.Unmarshal(raw, &old); e != nil {
		return old.Receipt, true, e
	}
	if old.ContentHash != ContainerIdentity(c) {
		return ContainerReceipt{}, true, ErrIdempotency
	}
	return old.Receipt, true, nil
}
func LookupContainerOperation(ctx context.Context, u repository.ContainerManagementUnitOfWork, c ContainerCommand) (ContainerReceipt, bool, error) {
	var result ContainerReceipt
	var found bool
	if e := validContainerCommand(c, true); e != nil {
		return result, false, e
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	err := u.WithinContainerManagement(ctx, c.AccountID, c.CharacterID, func(repo repository.ContainerManagementRepository, r repository.ManagementReceiptStore) error {
		var e error
		result, found, e = loadContainerReceipt(ctx, r, c)
		return e
	})
	return result, found, err
}
func RunContainerOperation(ctx context.Context, u repository.ContainerManagementUnitOfWork, c ContainerCommand, commit bool, offline QuestOfflineCheck) (ContainerReceipt, error) {
	var result ContainerReceipt
	if e := validContainerCommand(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	err := u.WithinContainerManagement(ctx, c.AccountID, c.CharacterID, func(repo repository.ContainerManagementRepository, r repository.ManagementReceiptStore) error {
		if commit {
			old, found, e := loadContainerReceipt(ctx, r, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		s, e := repo.LoadContainerManagement(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if s.Character.AccountID != c.AccountID || s.Character.CharacterID != c.CharacterID || s.Character.Stats["delete_flag"] != 0 {
			return ErrAccount
		}

		values, e := ContainerValues(s)
		if e != nil {
			return e
		}
		if ContainerRevision(s) != c.Revision {
			return ErrConflict
		}
		seq, e := containerSequence(s.Settings)
		if e != nil {
			return e
		}
		if seq == math.MaxInt64 {
			return repository.ErrCharacterContainerStateInvalid
		}
		result = ContainerReceipt{Kind: "container-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, Revision: c.Revision, Changes: []ContainerChange{}, Reason: c.Reason, Actor: c.Actor, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		next := s
		next.Settings = repository.CloneSettings(s.Settings)
		keys := []string{}
		for _, spec := range repository.ManagedContainers() {
			target, selected := c.Targets[spec.ID]
			if !selected {
				continue
			}
			if target < values[spec.ID] {
				return ErrContainerDowngrade
			}
			result.Changes = append(result.Changes, ContainerChange{ID: spec.ID, Name: spec.Name, Unit: spec.Unit, Before: values[spec.ID], After: target})
			keys = append(keys, spec.Key)
			next.Settings.Values[spec.Key] = strconv.Itoa(target)
		}
		if !commit {
			return nil
		}
		if offline == nil {
			return ErrRuntimeUnavailable
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		next.Settings.Values[repository.ContainerManagementSequence] = strconv.FormatInt(seq+1, 10)
		next.Settings.UpdatedAt = result.CreatedAt
		if !next.Settings.UpdatedAt.After(s.Settings.UpdatedAt) {
			next.Settings.UpdatedAt = s.Settings.UpdatedAt.Add(time.Microsecond)
		}
		if e = repo.SaveContainerManagement(ctx, next, keys); e != nil {
			return e
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		persisted, e := repo.LoadContainerManagement(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if ContainerRevision(persisted) != ContainerRevision(next) {
			return ErrConflict
		}
		result.Revision = ContainerRevision(persisted)
		result.RequestID = c.Key
		raw, e := json.Marshal(containerStored{Receipt: result, ContentHash: ContainerIdentity(c)})
		if e != nil {
			return e
		}
		return r.Append(ctx, c.Key, raw)
	})
	return result, err
}
func DecodeContainerReceipt(raw []byte) (ContainerReceipt, error) {
	var old containerStored
	e := json.Unmarshal(raw, &old)
	if e == nil && old.Receipt.Kind != "container-operation" {
		e = errors.New("invalid container receipt")
	}
	return old.Receipt, e
}
