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

const MaxSkillPoints = math.MaxUint16

var (
	ErrSkillPointInput      = errors.New("invalid skill point operation")
	ErrSkillPointLedger     = errors.New("invalid skill point ledger")
	ErrSkillPointMissing    = errors.New("skill point ledger missing")
	ErrSkillPointSpent      = errors.New("new skill point total is below already spent points")
	ErrSkillPointDefinition = errors.New("skill point PVF definition changed")
)

type SkillPointDefinition struct {
	PVFChecksum string `json:"pvfChecksum"`
	Version     int    `json:"version"`
	Level       int    `json:"level"`
	SP          int    `json:"sp"`
	TP          int    `json:"tp"`
}
type SkillPointResolver func(context.Context, int) (SkillPointDefinition, error)

type SkillPointCommand struct {
	AccountID          string `json:"accountId"`
	CharacterID        string `json:"characterId"`
	Action             string `json:"action"`
	Target             string `json:"target"`
	Amount             string `json:"amount"`
	Revision           string `json:"revision"`
	DefinitionRevision string `json:"definitionRevision"`
	Reason             string `json:"reason"`
	Actor              string `json:"actor"`
	PlanToken          string `json:"planToken,omitempty"`
	Key                string `json:"-"`
}
type SkillPointBalance struct {
	BonusSP     int64 `json:"bonusSp"`
	BonusTP     int64 `json:"bonusTp"`
	TotalSP     int   `json:"totalSp"`
	RemainingSP int   `json:"remainingSp"`
	TotalTP     int   `json:"totalTp"`
	RemainingTP int   `json:"remainingTp"`
	SyncedLevel int   `json:"syncedLevel"`
}
type SkillPointReceipt struct {
	RequestID   string               `json:"requestId"`
	Kind        string               `json:"kind"`
	AccountID   string               `json:"accountId"`
	CharacterID string               `json:"characterId"`
	Action      string               `json:"action"`
	Target      string               `json:"target"`
	Amount      string               `json:"amount"`
	Before      SkillPointBalance    `json:"before"`
	After       SkillPointBalance    `json:"after"`
	Definition  SkillPointDefinition `json:"definition"`
	Revision    string               `json:"revision"`
	Reason      string               `json:"reason"`
	Actor       string               `json:"actor"`
	CreatedAt   time.Time            `json:"createdAt"`
}
type skillPointStored struct {
	Receipt     SkillPointReceipt `json:"receipt"`
	ContentHash string            `json:"contentHash"`
}

func skillPointHash(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func SkillPointIdentity(c SkillPointCommand) string              { return skillPointHash(c) }
func SkillPointDefinitionRevision(d SkillPointDefinition) string { return skillPointHash(d) }
func SkillPointRevision(v repository.SkillPointManagementState) string {
	c, k := v.Character, v.Skill
	return skillPointHash([]any{c.CharacterID, c.AccountID, c.Level, c.Stats["delete_flag"], c.Stats["bonus_sp"], c.Stats["bonus_tp"], c.Stats["gm_skill_points_sequence_v1"], c.UpdatedAt.UTC(), v.Found, k.Points, k.UpdatedAt.UTC()})
}
func SkillPointValues(v repository.SkillPointManagementState) (SkillPointBalance, error) {
	if !v.Found {
		return SkillPointBalance{}, ErrSkillPointMissing
	}
	c, p := v.Character, v.Skill.Points
	b := SkillPointBalance{c.Stats["bonus_sp"], c.Stats["bonus_tp"], p.TotalSP, p.RemainingSP, p.TotalTP, p.RemainingTP, p.SyncedLevel}
	if c.Level < 1 || p.SyncedLevel < 0 || b.BonusSP < 0 || b.BonusSP > MaxSkillPoints || b.BonusTP < 0 || b.BonusTP > MaxSkillPoints || p.TotalSP < 0 || p.TotalSP > MaxSkillPoints || p.TotalTP < 0 || p.TotalTP > MaxSkillPoints || p.RemainingSP < 0 || p.RemainingSP > p.TotalSP || p.RemainingTP < 0 || p.RemainingTP > p.TotalTP || c.Stats["gm_skill_points_sequence_v1"] < 0 {
		return SkillPointBalance{}, ErrSkillPointLedger
	}
	return b, nil
}
func validSkillPointCommand(c SkillPointCommand, commit bool) error {
	id, err := strconv.ParseUint(c.CharacterID, 10, 16)
	n, e := strconv.ParseInt(c.Amount, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != c.CharacterID || c.AccountID == "" || c.Actor == "" || len(c.Reason) > 500 || strings.TrimSpace(c.Reason) == "" || e != nil || strconv.FormatInt(n, 10) != c.Amount || n < 0 || n > MaxSkillPoints {
		return ErrSkillPointInput
	}
	for _, h := range []string{c.Revision, c.DefinitionRevision} {
		raw, e := hex.DecodeString(h)
		if e != nil || len(raw) != 32 {
			return ErrSkillPointInput
		}
	}
	switch c.Action {
	case "add", "subtract", "set":
		if (c.Target != "sp" && c.Target != "tp") || (c.Action != "set" && n == 0) {
			return ErrSkillPointInput
		}
	case "clear_remaining":
		if (c.Target != "sp" && c.Target != "tp") || n != 0 {
			return ErrSkillPointInput
		}
	case "sync":
		if c.Target != "both" || n != 0 {
			return ErrSkillPointInput
		}
	default:
		return ErrSkillPointInput
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128) {
		return ErrSkillPointInput
	}
	return nil
}
func skillPointReceipt(ctx context.Context, r repository.ManagementReceiptStore, c SkillPointCommand) (SkillPointReceipt, bool, error) {
	raw, found, err := r.Load(ctx, c.Key)
	if err != nil || !found {
		return SkillPointReceipt{}, found, err
	}
	var old skillPointStored
	if err = json.Unmarshal(raw, &old); err != nil {
		return SkillPointReceipt{}, true, err
	}
	if old.ContentHash != SkillPointIdentity(c) {
		return SkillPointReceipt{}, true, ErrIdempotency
	}
	return old.Receipt, true, nil
}
func skillPointOwner(v repository.SkillPointManagementState, c SkillPointCommand) error {
	if v.Character.CharacterID != c.CharacterID || v.Character.AccountID != c.AccountID || v.Character.Stats["delete_flag"] != 0 {
		return ErrAccount
	}
	return nil
}

func LookupSkillPointOperation(ctx context.Context, u repository.SkillPointManagementUnitOfWork, c SkillPointCommand) (SkillPointReceipt, bool, error) {
	var result SkillPointReceipt
	var found bool
	if err := validSkillPointCommand(c, true); err != nil {
		return result, false, err
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	err := u.WithinSkillPointManagement(ctx, c.AccountID, c.CharacterID, func(r repository.SkillPointManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, err := r.LoadSkillPointManagement(ctx, c.CharacterID)
		if err != nil {
			return err
		}
		if err = skillPointOwner(v, c); err != nil {
			return err
		}
		result, found, err = skillPointReceipt(ctx, receipts, c)
		return err
	})
	return result, found, err
}

func RunSkillPointOperation(ctx context.Context, u repository.SkillPointManagementUnitOfWork, c SkillPointCommand, commit bool, resolve SkillPointResolver, offline QuestOfflineCheck) (SkillPointReceipt, error) {
	var result SkillPointReceipt
	if err := validSkillPointCommand(c, commit); err != nil {
		return result, err
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	err := u.WithinSkillPointManagement(ctx, c.AccountID, c.CharacterID, func(r repository.SkillPointManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, err := r.LoadSkillPointManagement(ctx, c.CharacterID)
		if err != nil {
			return err
		}
		if err = skillPointOwner(v, c); err != nil {
			return err
		}
		if commit {
			old, found, e := skillPointReceipt(ctx, receipts, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		before, err := SkillPointValues(v)
		if err != nil {
			return err
		}
		if SkillPointRevision(v) != c.Revision {
			return ErrConflict
		}
		if v.Character.Stats["gm_skill_points_sequence_v1"] == math.MaxInt64 {
			return ErrSkillPointLedger
		}
		if resolve == nil {
			return ErrSkillPointDefinition
		}
		def, err := resolve(ctx, v.Character.Level)
		if err != nil {
			return err
		}
		digest, hashErr := hex.DecodeString(def.PVFChecksum)
		if hashErr != nil || len(digest) != 32 || def.Version != 1 || def.Level != v.Character.Level || def.SP < 0 || def.TP < 0 || def.SP > MaxSkillPoints || def.TP > MaxSkillPoints || SkillPointDefinitionRevision(def) != c.DefinitionRevision {
			return ErrSkillPointDefinition
		}
		after := before
		if c.Action == "add" || c.Action == "subtract" || c.Action == "set" {
			n, _ := strconv.ParseInt(c.Amount, 10, 64)
			bonus := &after.BonusSP
			if c.Target == "tp" {
				bonus = &after.BonusTP
			}
			switch c.Action {
			case "set":
				*bonus = n
			case "add":
				*bonus += n
			case "subtract":
				*bonus -= n
			}
		}
		if after.BonusSP < 0 || after.BonusSP > MaxSkillPoints || after.BonusTP < 0 || after.BonusTP > MaxSkillPoints {
			return ErrSkillPointInput
		}
		after.TotalSP = def.SP + int(after.BonusSP)
		after.TotalTP = def.TP + int(after.BonusTP)
		if after.TotalSP > MaxSkillPoints || after.TotalTP > MaxSkillPoints {
			return ErrSkillPointInput
		}
		after.RemainingSP = after.TotalSP - (before.TotalSP - before.RemainingSP)
		after.RemainingTP = after.TotalTP - (before.TotalTP - before.RemainingTP)
		if after.RemainingSP < 0 || after.RemainingTP < 0 {
			return ErrSkillPointSpent
		}
		after.SyncedLevel = def.Level
		if c.Action == "clear_remaining" {
			if c.Target == "sp" {
				after.RemainingSP = 0
			} else {
				after.RemainingTP = 0
			}
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		result = SkillPointReceipt{Kind: "skill-point-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, Action: c.Action, Target: c.Target, Amount: c.Amount, Before: before, After: after, Definition: def, Revision: c.Revision, Reason: c.Reason, Actor: c.Actor, CreatedAt: at}
		if !commit {
			return nil
		}
		if offline == nil {
			return ErrRuntimeUnavailable
		}
		if err = offline(ctx, c.CharacterID); err != nil {
			return err
		}
		v.Character = repository.CloneCharacter(v.Character)
		v.Character.Stats["bonus_sp"], v.Character.Stats["bonus_tp"] = after.BonusSP, after.BonusTP
		v.Character.Stats["gm_skill_points_sequence_v1"]++
		if !at.After(v.Character.UpdatedAt) {
			at = v.Character.UpdatedAt.Add(time.Microsecond)
		}
		if !at.After(v.Skill.UpdatedAt) {
			at = v.Skill.UpdatedAt.Add(time.Microsecond)
		}
		v.Character.UpdatedAt, v.Skill.UpdatedAt = at, at
		v.Skill.Points = repository.SkillPointState{TotalSP: after.TotalSP, RemainingSP: after.RemainingSP, TotalTP: after.TotalTP, RemainingTP: after.RemainingTP, SyncedLevel: after.SyncedLevel}
		if err = r.SaveSkillPointManagement(ctx, v); err != nil {
			return err
		}
		if err = offline(ctx, c.CharacterID); err != nil {
			return err
		}
		result.RequestID = c.Key
		raw, err := json.Marshal(skillPointStored{Receipt: result, ContentHash: SkillPointIdentity(c)})
		if err != nil {
			return err
		}
		return receipts.Append(ctx, c.Key, raw)
	})
	return result, err
}
func DecodeSkillPointReceipt(raw []byte) (SkillPointReceipt, error) {
	var v skillPointStored
	err := json.Unmarshal(raw, &v)
	if err == nil && v.Receipt.Kind != "skill-point-operation" {
		err = ErrSkillPointLedger
	}
	return v.Receipt, err
}
