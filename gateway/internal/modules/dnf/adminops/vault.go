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

// Both the cargo acknowledgement and character gold notification use int32.
const MaxVaultGold int64 = math.MaxInt32

var ErrVaultInput = errors.New("invalid account vault operation")
var ErrVaultNotCreated = errors.New("account vault has not been created")
var ErrVaultDecrease = errors.New("vault shrink requires item migration")

type VaultGoldChange struct {
	Action string `json:"action"`
	Amount string `json:"amount"`
}
type VaultCommand struct {
	DefinitionRevision string           `json:"definitionRevision"`
	AccountID          string           `json:"accountId"`
	Gold               *VaultGoldChange `json:"gold,omitempty"`
	Capacity           *int             `json:"capacity,omitempty"`
	Revision           string           `json:"revision"`
	Reason             string           `json:"reason"`
	Actor              string           `json:"actor"`
	PlanToken          string           `json:"planToken,omitempty"`
	Key                string           `json:"-"`
}
type VaultValues struct {
	Gold     string `json:"gold"`
	Capacity int    `json:"capacity"`
	Created  bool   `json:"created"`
}
type VaultReceipt struct {
	DefinitionRevision string           `json:"definitionRevision"`
	RequestID          string           `json:"requestId,omitempty"`
	Kind               string           `json:"kind"`
	AccountID          string           `json:"accountId"`
	Gold               *VaultGoldChange `json:"gold,omitempty"`
	Capacity           *int             `json:"capacity,omitempty"`
	Before             VaultValues      `json:"before"`
	After              VaultValues      `json:"after"`
	Delta              string           `json:"delta"`
	Revision           string           `json:"revision"`
	Reason             string           `json:"reason"`
	Actor              string           `json:"actor"`
	CreatedAt          time.Time        `json:"createdAt"`
}
type vaultStored struct {
	Receipt     VaultReceipt `json:"receipt"`
	ContentHash string       `json:"contentHash"`
}

func VaultRevision(s repository.AccountVaultState) string {
	raw, _ := json.Marshal([]any{s.AccountID, s.UpdatedAt.UTC(), s.Values})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func VaultIdentity(c VaultCommand) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func vaultInteger(values map[string]string, key string, max int64) (int64, error) {
	raw, found := values[key]
	if !found {
		return 0, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" && key != repository.VaultSequenceKey {
		return 0, nil
	}
	n, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || n < 0 || n > max {
		return 0, repository.ErrVaultState
	}
	return n, nil
}
func ReadVaultValues(s repository.AccountVaultState) (VaultValues, error) {
	var v VaultValues
	gold, e := vaultInteger(s.Values, repository.VaultGoldKey, MaxVaultGold)
	if e != nil {
		return v, e
	}
	capacity, e := vaultInteger(s.Values, repository.VaultCapacityKey, math.MaxInt16)
	if e != nil {
		return v, e
	}
	if _, e = vaultInteger(s.Values, repository.VaultSequenceKey, math.MaxInt64); e != nil {
		return v, e
	}
	created := false
	switch strings.ToLower(strings.TrimSpace(s.Values[repository.VaultCreatedKey])) {
	case "1", "true", "yes", "on":
		created = true
	case "", "0", "false", "no", "off":
	default:
		return v, repository.ErrVaultState
	}
	if capacity == 0 && (created || gold > 0) {
		return v, repository.ErrVaultState
	}
	return VaultValues{Gold: strconv.FormatInt(gold, 10), Capacity: int(capacity), Created: capacity > 0}, nil
}
func validVaultCommand(c VaultCommand, commit bool) error {
	if c.AccountID == "" || len(c.AccountID) > 128 || strings.TrimSpace(c.AccountID) != c.AccountID || c.Actor == "" || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 500 || len(c.Revision) != 64 || len(c.DefinitionRevision) != 64 || (c.Gold == nil && c.Capacity == nil) {
		return ErrVaultInput
	}
	if _, e := hex.DecodeString(c.Revision); e != nil {
		return ErrVaultInput
	}
	if c.Capacity != nil && (*c.Capacity <= 0 || *c.Capacity > math.MaxInt16) {
		return ErrVaultInput
	}
	if c.Gold != nil {
		n, e := strconv.ParseInt(c.Gold.Amount, 10, 64)
		if e != nil || n < 0 || n > MaxVaultGold || strconv.FormatInt(n, 10) != c.Gold.Amount {
			return ErrVaultInput
		}
		switch c.Gold.Action {
		case "add", "subtract":
			if n == 0 {
				return ErrVaultInput
			}
		case "set":
		default:
			return ErrVaultInput
		}
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128) {
		return ErrVaultInput
	}
	return nil
}
func loadVaultReceipt(ctx context.Context, r repository.ManagementReceiptStore, c VaultCommand) (VaultReceipt, bool, error) {
	var v vaultStored
	raw, found, e := r.Load(ctx, c.Key)
	if e != nil || !found {
		return v.Receipt, found, e
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		return v.Receipt, true, e
	}
	if v.ContentHash != VaultIdentity(c) {
		return VaultReceipt{}, true, ErrIdempotency
	}
	return v.Receipt, true, nil
}
func LookupVaultOperation(ctx context.Context, u repository.AccountVaultUnitOfWork, c VaultCommand) (VaultReceipt, bool, error) {
	var result VaultReceipt
	var found bool
	if e := validVaultCommand(c, true); e != nil {
		return result, false, e
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	e := u.WithinAccountVault(ctx, c.AccountID, func(_ repository.AccountVaultRepository, r repository.ManagementReceiptStore) error {
		var e error
		result, found, e = loadVaultReceipt(ctx, r, c)
		return e
	})
	return result, found, e
}
func vaultOffline(ctx context.Context, repo repository.AccountVaultRepository, check QuestOfflineCheck) error {
	if check == nil {
		return ErrRuntimeUnavailable
	}
	ids, e := repo.VaultCharacters(ctx)
	if e != nil {
		return e
	}
	if len(ids) > repository.DefaultCharacterSlotLimit {
		return ErrAccount
	}
	// An account can be connected before creating its first character.
	// Empty character identity asks the account-scoped checker for that case.
	if len(ids) == 0 {
		return check(ctx, "")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		n, e := strconv.ParseUint(id, 10, 16)
		if e != nil || n == 0 || strconv.FormatUint(n, 10) != id || seen[id] {
			return ErrAccount
		}
		seen[id] = true
		if e = check(ctx, id); e != nil {
			return e
		}
	}
	return nil
}
func RunVaultOperation(ctx context.Context, u repository.AccountVaultUnitOfWork, c VaultCommand, definition VaultDefinition, commit bool, offline QuestOfflineCheck) (VaultReceipt, error) {
	var result VaultReceipt
	if e := validVaultCommand(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	e := u.WithinAccountVault(ctx, c.AccountID, func(repo repository.AccountVaultRepository, r repository.ManagementReceiptStore) error {
		if commit {
			old, found, e := loadVaultReceipt(ctx, r, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		if c.DefinitionRevision != definition.Revision {
			return ErrVaultDefinitionChanged
		}
		s, e := repo.LoadAccountVault(ctx, c.AccountID)
		if e != nil {
			return e
		}
		if s.AccountID != c.AccountID {
			return ErrAccount
		}
		before, e := ReadVaultValues(s)
		if e != nil {
			return e
		}
		if VaultRevision(s) != c.Revision {
			return ErrConflict
		}
		seq, e := vaultInteger(s.Values, repository.VaultSequenceKey, math.MaxInt64)
		if e != nil {
			return e
		}
		if seq == math.MaxInt64 {
			return repository.ErrVaultState
		}
		after := before
		keys := []string{}
		next := s
		next.Values = make(map[string]string, len(s.Values)+4)
		for k, v := range s.Values {
			next.Values[k] = v
		}
		if c.Capacity != nil {
			if _, supported := definition.GoldLimit(*c.Capacity); !supported {
				return ErrVaultInput
			}
			if *c.Capacity < before.Capacity {
				return ErrVaultDecrease
			}
			after.Capacity = *c.Capacity
			after.Created = true
			next.Values[repository.VaultCapacityKey] = strconv.Itoa(after.Capacity)
			next.Values[repository.VaultCreatedKey] = "true"
			keys = append(keys, repository.VaultCapacityKey, repository.VaultCreatedKey)
		}
		if !after.Created {
			return ErrVaultNotCreated
		}
		limit, supported := definition.GoldLimit(after.Capacity)
		if !supported {
			return ErrVaultInput
		}
		goldBefore, _ := strconv.ParseInt(before.Gold, 10, 64)
		goldAfter := goldBefore
		if c.Gold != nil {
			amount, _ := strconv.ParseInt(c.Gold.Amount, 10, 64)
			switch c.Gold.Action {
			case "add":
				goldAfter += amount
			case "subtract":
				goldAfter -= amount
			case "set":
				goldAfter = amount
			}
			if goldAfter < 0 || goldAfter > MaxVaultGold {
				return ErrVaultInput
			}
			after.Gold = strconv.FormatInt(goldAfter, 10)
			next.Values[repository.VaultGoldKey] = after.Gold
			keys = append(keys, repository.VaultGoldKey)
		} else if !before.Created {
			next.Values[repository.VaultGoldKey] = "0"
			keys = append(keys, repository.VaultGoldKey)
		}
		if goldAfter > limit {
			return ErrVaultInput
		}
		result = VaultReceipt{DefinitionRevision: definition.Revision, Kind: "account-vault-operation", AccountID: c.AccountID, Gold: c.Gold, Capacity: c.Capacity, Before: before, After: after, Delta: strconv.FormatInt(goldAfter-goldBefore, 10), Revision: c.Revision, Reason: c.Reason, Actor: c.Actor, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if !commit {
			return nil
		}
		if e = vaultOffline(ctx, repo, offline); e != nil {
			return e
		}
		next.Values[repository.VaultSequenceKey] = strconv.FormatInt(seq+1, 10)
		next.UpdatedAt = result.CreatedAt
		if !next.UpdatedAt.After(s.UpdatedAt) {
			next.UpdatedAt = s.UpdatedAt.Add(time.Microsecond)
		}
		if e = repo.SaveAccountVault(ctx, next, keys); e != nil {
			return e
		}
		if e = vaultOffline(ctx, repo, offline); e != nil {
			return e
		}
		persisted, e := repo.LoadAccountVault(ctx, c.AccountID)
		if e != nil {
			return e
		}
		if VaultRevision(persisted) != VaultRevision(next) {
			return ErrConflict
		}
		result.Revision = VaultRevision(persisted)
		result.RequestID = c.Key
		raw, e := json.Marshal(vaultStored{Receipt: result, ContentHash: VaultIdentity(c)})
		if e != nil {
			return e
		}
		return r.Append(ctx, c.Key, raw)
	})
	return result, e
}
func DecodeVaultReceipt(raw []byte) (VaultReceipt, error) {
	var v vaultStored
	e := json.Unmarshal(raw, &v)
	if e == nil && v.Receipt.Kind != "account-vault-operation" {
		e = errors.New("invalid vault receipt")
	}
	return v.Receipt, e
}
