package adminops

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const MaximumCharacterLevel = 90

var (
	ErrLevelInput      = errors.New("invalid level operation")
	ErrLevelState      = errors.New("invalid character level state")
	ErrLevelDefinition = errors.New("level PVF definition changed")
)

type LevelDefinition struct {
	PVFChecksum       string           `json:"pvfChecksum"`
	Version           int              `json:"version"`
	Level             int              `json:"level"`
	MinimumExperience uint32           `json:"minimumExperience"`
	NextExperience    uint32           `json:"nextExperience"`
	SP                int              `json:"sp"`
	TP                int              `json:"tp"`
	Stats             map[string]int64 `json:"stats"`
}
type LevelResolver func(context.Context, repository.CharacterRecord, int) (LevelDefinition, error)
type LevelCommand struct {
	AccountID          string `json:"accountId"`
	CharacterID        string `json:"characterId"`
	TargetLevel        int    `json:"targetLevel"`
	Revision           string `json:"revision"`
	PVFChecksum        string `json:"pvfChecksum"`
	DefinitionRevision string `json:"definitionRevision"`
	Reason             string `json:"reason"`
	Actor              string `json:"actor"`
	PlanToken          string `json:"planToken,omitempty"`
	Key                string `json:"-"`
}
type LevelBalance struct {
	Level      int               `json:"level"`
	Experience string            `json:"experience"`
	Points     SkillPointBalance `json:"points"`
	Stats      map[string]int64  `json:"stats"`
}
type LevelReceipt struct {
	RequestID          string          `json:"requestId"`
	Kind               string          `json:"kind"`
	AccountID          string          `json:"accountId"`
	CharacterID        string          `json:"characterId"`
	TargetLevel        int             `json:"targetLevel"`
	Before             LevelBalance    `json:"before"`
	After              LevelBalance    `json:"after"`
	Definition         LevelDefinition `json:"definition"`
	DefinitionRevision string          `json:"definitionRevision"`
	Revision           string          `json:"revision"`
	Reason             string          `json:"reason"`
	Actor              string          `json:"actor"`
	CreatedAt          time.Time       `json:"createdAt"`
}
type levelStored struct {
	Receipt     LevelReceipt `json:"receipt"`
	ContentHash string       `json:"contentHash"`
}

func LevelIdentity(c LevelCommand) string              { return skillPointHash(c) }
func LevelDefinitionRevision(d LevelDefinition) string { return skillPointHash(d) }
func LevelRevision(v repository.SkillPointManagementState) string {
	return skillPointHash([]any{SkillPointRevision(v), v.Character.Job, v.Character.Stats["grow_type"], v.Character.Stats["exp"], v.Character.Stats["gm_level_sequence_v1"], levelStats(v.Character)})
}
func levelStats(c repository.CharacterRecord) map[string]int64 {
	m := make(map[string]int64, len(repository.LevelStatKeys))
	for _, k := range repository.LevelStatKeys {
		m[k] = c.Stats[k]
	}
	return m
}
func LevelValues(v repository.SkillPointManagementState) (LevelBalance, error) {
	p, e := SkillPointValues(v)
	if e != nil {
		return LevelBalance{}, e
	}
	c := v.Character
	job, e := strconv.ParseUint(c.Job, 10, 8)
	if e != nil || strconv.FormatUint(job, 10) != c.Job || c.Level > MaximumCharacterLevel || c.Stats["grow_type"] < 0 || c.Stats["grow_type"] > 255 || c.Stats["exp"] < 0 || c.Stats["exp"] > math.MaxUint32 || c.Stats["gm_level_sequence_v1"] < 0 {
		return LevelBalance{}, ErrLevelState
	}
	return LevelBalance{Level: c.Level, Experience: strconv.FormatInt(c.Stats["exp"], 10), Points: p, Stats: levelStats(c)}, nil
}
func validLevelCommand(c LevelCommand, commit bool) error {
	n, e := strconv.ParseUint(c.CharacterID, 10, 16)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != c.CharacterID || c.AccountID == "" || c.Actor == "" || c.TargetLevel < 1 || c.TargetLevel > MaximumCharacterLevel || len(c.Reason) > 500 || strings.TrimSpace(c.Reason) == "" {
		return ErrLevelInput
	}
	hashes := []string{c.Revision, c.PVFChecksum}
	if commit || c.DefinitionRevision != "" {
		hashes = append(hashes, c.DefinitionRevision)
	}
	for _, h := range hashes {
		v, e := hex.DecodeString(h)
		if e != nil || len(v) != 32 {
			return ErrLevelInput
		}
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128) {
		return ErrLevelInput
	}
	return nil
}
func levelOwner(v repository.SkillPointManagementState, c LevelCommand) error {
	if v.Character.CharacterID != c.CharacterID || v.Character.AccountID != c.AccountID || v.Character.Stats["delete_flag"] != 0 {
		return ErrAccount
	}
	return nil
}
func levelReceipt(ctx context.Context, r repository.ManagementReceiptStore, c LevelCommand) (LevelReceipt, bool, error) {
	raw, found, e := r.Load(ctx, c.Key)
	if e != nil || !found {
		return LevelReceipt{}, found, e
	}
	var old levelStored
	if e = json.Unmarshal(raw, &old); e != nil {
		return LevelReceipt{}, true, e
	}
	if old.ContentHash != LevelIdentity(c) {
		return LevelReceipt{}, true, ErrIdempotency
	}
	return old.Receipt, true, nil
}
func LookupLevelOperation(ctx context.Context, u repository.LevelManagementUnitOfWork, c LevelCommand) (LevelReceipt, bool, error) {
	var result LevelReceipt
	var found bool
	if e := validLevelCommand(c, true); e != nil {
		return result, false, e
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	e := u.WithinLevelManagement(ctx, c.AccountID, c.CharacterID, func(r repository.LevelManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, e := r.LoadLevelManagement(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if e = levelOwner(v, c); e != nil {
			return e
		}
		result, found, e = levelReceipt(ctx, receipts, c)
		return e
	})
	return result, found, e
}
func validateLevelDefinition(d LevelDefinition, c LevelCommand) error {
	if d.PVFChecksum != c.PVFChecksum || d.Version != 1 || d.Level != c.TargetLevel || d.SP < 0 || d.SP > MaxSkillPoints || d.TP < 0 || d.TP > MaxSkillPoints || len(d.Stats) != len(repository.LevelStatKeys) {
		return ErrLevelDefinition
	}
	if (d.Level == 1 && d.MinimumExperience != 0) || (d.Level < MaximumCharacterLevel && d.NextExperience <= d.MinimumExperience) {
		return ErrLevelDefinition
	}
	for _, k := range repository.LevelStatKeys {
		n, found := d.Stats[k]
		if !found {
			return ErrLevelDefinition
		}
		switch k {
		case "stat_hp_max", "stat_mp_max", "stat_inventory_limit", "stat_move_speed", "stat_weight":
		case "stat_hp_regen_speed", "stat_mp_regen_speed", "stat_attack_speed", "stat_cast_speed", "stat_hit_recovery", "stat_jump_power":
			if n < 0 || n > math.MaxUint16 {
				return ErrLevelDefinition
			}
		default:
			if n < math.MinInt32 || n > math.MaxInt32 {
				return ErrLevelDefinition
			}
		}
	}
	if c.DefinitionRevision != "" && c.DefinitionRevision != LevelDefinitionRevision(d) {
		return ErrLevelDefinition
	}
	return nil
}
func RunLevelOperation(ctx context.Context, u repository.LevelManagementUnitOfWork, c LevelCommand, commit bool, resolve LevelResolver, offline QuestOfflineCheck) (LevelReceipt, error) {
	var result LevelReceipt
	if e := validLevelCommand(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	e := u.WithinLevelManagement(ctx, c.AccountID, c.CharacterID, func(r repository.LevelManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, e := r.LoadLevelManagement(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if e = levelOwner(v, c); e != nil {
			return e
		}
		if commit {
			old, found, e := levelReceipt(ctx, receipts, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		before, e := LevelValues(v)
		if e != nil {
			return e
		}
		if LevelRevision(v) != c.Revision {
			return ErrConflict
		}
		if v.Character.Stats["gm_level_sequence_v1"] == math.MaxInt64 {
			return ErrLevelState
		}
		if resolve == nil {
			return ErrLevelDefinition
		}
		d, e := resolve(ctx, v.Character, c.TargetLevel)
		if e != nil {
			return e
		}
		if e = validateLevelDefinition(d, c); e != nil {
			return e
		}
		after := before
		after.Level = d.Level
		after.Experience = strconv.FormatUint(uint64(d.MinimumExperience), 10)
		after.Stats = d.Stats
		after.Points.TotalSP = d.SP + int(before.Points.BonusSP)
		after.Points.TotalTP = d.TP + int(before.Points.BonusTP)
		if after.Points.TotalSP > MaxSkillPoints || after.Points.TotalTP > MaxSkillPoints {
			return ErrSkillPointInput
		}
		after.Points.RemainingSP = after.Points.TotalSP - (before.Points.TotalSP - before.Points.RemainingSP)
		after.Points.RemainingTP = after.Points.TotalTP - (before.Points.TotalTP - before.Points.RemainingTP)
		if after.Points.RemainingSP < 0 || after.Points.RemainingTP < 0 {
			return ErrSkillPointSpent
		}
		after.Points.SyncedLevel = d.Level
		at := time.Now().UTC().Truncate(time.Microsecond)
		result = LevelReceipt{Kind: "level-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, TargetLevel: c.TargetLevel, Before: before, After: after, Definition: d, DefinitionRevision: LevelDefinitionRevision(d), Revision: c.Revision, Reason: c.Reason, Actor: c.Actor, CreatedAt: at}
		if !commit {
			return nil
		}
		if offline == nil {
			return ErrRuntimeUnavailable
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		v.Character = repository.CloneCharacter(v.Character)
		v.Character.Level = d.Level
		v.Character.Stats["exp"] = int64(d.MinimumExperience)
		v.Character.Stats["gm_level_sequence_v1"]++
		for k, n := range after.Stats {
			v.Character.Stats[k] = n
		}
		if !at.After(v.Character.UpdatedAt) {
			at = v.Character.UpdatedAt.Add(time.Microsecond)
		}
		if !at.After(v.Skill.UpdatedAt) {
			at = v.Skill.UpdatedAt.Add(time.Microsecond)
		}
		v.Character.UpdatedAt, v.Skill.UpdatedAt = at, at
		v.Skill.Points = repository.SkillPointState{TotalSP: after.Points.TotalSP, RemainingSP: after.Points.RemainingSP, TotalTP: after.Points.TotalTP, RemainingTP: after.Points.RemainingTP, SyncedLevel: d.Level}
		if e = r.SaveLevelManagement(ctx, v); e != nil {
			return e
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		result.RequestID = c.Key
		raw, e := json.Marshal(levelStored{Receipt: result, ContentHash: LevelIdentity(c)})
		if e != nil {
			return e
		}
		return receipts.Append(ctx, c.Key, raw)
	})
	return result, e
}
func DecodeLevelReceipt(raw []byte) (LevelReceipt, error) {
	var v levelStored
	e := json.Unmarshal(raw, &v)
	if e == nil && v.Receipt.Kind != "level-operation" {
		e = ErrLevelState
	}
	return v.Receipt, e
}
