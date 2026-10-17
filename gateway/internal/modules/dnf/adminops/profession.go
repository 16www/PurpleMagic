package adminops

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/profession"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/skill"
)

var ErrProfessionInput = errors.New("invalid profession operation")
var ErrProfessionDefinition = errors.New("profession definition changed")

type ProfessionCommand struct {
	AccountID          string `json:"accountId"`
	CharacterID        string `json:"characterId"`
	TargetJob          int    `json:"targetJob"`
	TargetGrowType     int    `json:"targetGrowType"`
	Revision           string `json:"revision"`
	PVFChecksum        string `json:"pvfChecksum"`
	DefinitionRevision string `json:"definitionRevision"`
	Reason             string `json:"reason"`
	Actor              string `json:"actor"`
	PlanToken          string `json:"planToken,omitempty"`
	Key                string `json:"-"`
}
type ProfessionDefinition struct {
	PVFChecksum      string               `json:"pvfChecksum"`
	ManifestHash     string               `json:"manifestHash"`
	Job              int                  `json:"job"`
	GrowType         int                  `json:"growType"`
	Name             string               `json:"name"`
	Growth           LevelDefinition      `json:"growth"`
	QuestIDs         []int64              `json:"questIds"`
	PreviousQuestIDs []int64              `json:"previousQuestIds"`
	AllQuestIDs      []int64              `json:"-"`
	Profiles         *profession.Profiles `json:"-"`
	Skills           *skill.Table         `json:"-"`
}
type ProfessionResolver func(context.Context, repository.CharacterRecord, int, int) (ProfessionDefinition, error)
type ProfessionBalance struct {
	Job           int          `json:"job"`
	GrowType      int          `json:"growType"`
	Growth        LevelBalance `json:"growth"`
	LearnedSkills int          `json:"learnedSkills"`
}
type ProfessionSkillChange struct {
	ID     int64 `json:"id"`
	Before int   `json:"before"`
	After  int   `json:"after"`
}
type ProfessionReceipt struct {
	RequestID          string                  `json:"requestId"`
	Kind               string                  `json:"kind"`
	AccountID          string                  `json:"accountId"`
	CharacterID        string                  `json:"characterId"`
	Before             ProfessionBalance       `json:"before"`
	After              ProfessionBalance       `json:"after"`
	Definition         ProfessionDefinition    `json:"definition"`
	DefinitionRevision string                  `json:"definitionRevision"`
	ResetSkills        bool                    `json:"resetSkills"`
	SkillChanges       []ProfessionSkillChange `json:"skillChanges"`
	CreatedQuestIDs    []int64                 `json:"createdQuestIds"`
	CompletedQuestIDs  []int64                 `json:"completedQuestIds"`
	RetainedQuestIDs   []int64                 `json:"retainedQuestIds"`
	Revision           string                  `json:"revision"`
	Reason             string                  `json:"reason"`
	Actor              string                  `json:"actor"`
	CreatedAt          time.Time               `json:"createdAt"`
}
type professionStored struct {
	Receipt     ProfessionReceipt `json:"receipt"`
	ContentHash string            `json:"contentHash"`
}

func ProfessionIdentity(c ProfessionCommand) string              { return skillPointHash(c) }
func ProfessionDefinitionRevision(d ProfessionDefinition) string { return skillPointHash(d) }
func ProfessionRevision(v repository.ProfessionManagementState) string {
	return skillPointHash([]any{LevelRevision(v.Growth), v.Growth.Skill, v.Quests})
}
func ProfessionValues(v repository.ProfessionManagementState) (ProfessionBalance, error) {
	b, e := LevelValues(v.Growth)
	if e != nil {
		return ProfessionBalance{}, e
	}
	job, _ := strconv.Atoi(v.Growth.Character.Job)
	return ProfessionBalance{Job: job, GrowType: int(v.Growth.Character.Stats["grow_type"]), Growth: b, LearnedSkills: len(v.Growth.Skill.Skills)}, nil
}
func validProfessionCommand(c ProfessionCommand, commit bool) error {
	if c.TargetJob < 0 || c.TargetJob > 255 || c.TargetGrowType < 0 || c.TargetGrowType > 255 {
		return ErrProfessionInput
	}
	e := validLevelCommand(LevelCommand{AccountID: c.AccountID, CharacterID: c.CharacterID, TargetLevel: 1, Revision: c.Revision, PVFChecksum: c.PVFChecksum, DefinitionRevision: c.DefinitionRevision, Reason: c.Reason, Actor: c.Actor, Key: c.Key}, commit)
	if e != nil {
		return ErrProfessionInput
	}
	return nil
}
func professionReceipt(ctx context.Context, r repository.ManagementReceiptStore, c ProfessionCommand) (ProfessionReceipt, bool, error) {
	raw, found, e := r.Load(ctx, c.Key)
	if e != nil || !found {
		return ProfessionReceipt{}, found, e
	}
	var v professionStored
	if e = json.Unmarshal(raw, &v); e != nil {
		return ProfessionReceipt{}, true, e
	}
	if v.ContentHash != ProfessionIdentity(c) {
		return ProfessionReceipt{}, true, ErrIdempotency
	}
	return v.Receipt, true, nil
}
func LookupProfessionOperation(ctx context.Context, u repository.ProfessionManagementUnitOfWork, c ProfessionCommand) (ProfessionReceipt, bool, error) {
	var result ProfessionReceipt
	var found bool
	if e := validProfessionCommand(c, true); e != nil {
		return result, false, e
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	e := u.WithinProfessionManagement(ctx, c.AccountID, c.CharacterID, func(r repository.ProfessionManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, e := r.LoadProfessionManagement(ctx, c.CharacterID, nil)
		if e != nil {
			return e
		}
		if v.Growth.Character.AccountID != c.AccountID || v.Growth.Character.Stats["delete_flag"] != 0 {
			return ErrAccount
		}
		result, found, e = professionReceipt(ctx, receipts, c)
		return e
	})
	return result, found, e
}
func professionCompleted(q repository.QuestState) bool {
	switch strings.ToLower(strings.TrimSpace(q.Status)) {
	case "complete", "completed", "cleared", "finished", "done":
		return true
	}
	return false
}
func RunProfessionOperation(ctx context.Context, u repository.ProfessionManagementUnitOfWork, c ProfessionCommand, commit bool, resolve ProfessionResolver, offline QuestOfflineCheck) (ProfessionReceipt, error) {
	var result ProfessionReceipt
	if e := validProfessionCommand(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	e := u.WithinProfessionManagement(ctx, c.AccountID, c.CharacterID, func(r repository.ProfessionManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, e := r.LoadProfessionManagement(ctx, c.CharacterID, nil)
		if e != nil {
			return e
		}
		if v.Growth.Character.AccountID != c.AccountID || v.Growth.Character.Stats["delete_flag"] != 0 {
			return ErrAccount
		}
		if commit {
			old, found, e := professionReceipt(ctx, receipts, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		if resolve == nil {
			return ErrProfessionDefinition
		}
		d, e := resolve(ctx, v.Growth.Character, c.TargetJob, c.TargetGrowType)
		if e != nil {
			return e
		}
		if d.PVFChecksum != c.PVFChecksum || d.Job != c.TargetJob || d.GrowType != c.TargetGrowType || d.Profiles == nil || d.Skills == nil || len(d.AllQuestIDs) == 0 || len(d.AllQuestIDs) > 4096 || len(d.ManifestHash) != 64 || d.Name == "" {
			return ErrProfessionDefinition
		}
		if e = validateLevelDefinition(d.Growth, LevelCommand{PVFChecksum: c.PVFChecksum, TargetLevel: v.Growth.Character.Level}); e != nil {
			return e
		}
		v, e = r.LoadProfessionManagement(ctx, c.CharacterID, d.AllQuestIDs)
		if e != nil {
			return e
		}
		before, e := ProfessionValues(v)
		if e != nil {
			return e
		}
		if ProfessionRevision(v) != c.Revision {
			return ErrConflict
		}
		if c.DefinitionRevision != "" && c.DefinitionRevision != ProfessionDefinitionRevision(d) {
			return ErrProfessionDefinition
		}
		if v.Growth.Character.Stats["gm_level_sequence_v1"] == math.MaxInt64 {
			return ErrLevelState
		}
		k := repository.CloneSkill(v.Growth.Skill)
		k.Points.TotalSP = d.Growth.SP + int(before.Growth.Points.BonusSP)
		k.Points.TotalTP = d.Growth.TP + int(before.Growth.Points.BonusTP)
		k.Points.RemainingSP = k.Points.TotalSP - (before.Growth.Points.TotalSP - before.Growth.Points.RemainingSP)
		k.Points.RemainingTP = k.Points.TotalTP - (before.Growth.Points.TotalTP - before.Growth.Points.RemainingTP)
		oldBranch, oldStage := profession.Decode(byte(before.GrowType))
		branch, stage := profession.Decode(byte(d.GrowType))
		if before.Job != d.Job || oldBranch != branch || stage < oldStage {
			// Reset operations refund the whole new baseline, even after a PVF
			// change reduced it below the old spent amount.
			k.Points.RemainingSP, k.Points.RemainingTP = k.Points.TotalSP, k.Points.TotalTP
		}
		plan, e := d.Profiles.PlanManagementTransition(d.Skills, byte(before.Job), byte(before.GrowType), byte(d.Job), byte(d.GrowType), v.Growth.Character.Level, k)
		if e != nil {
			return e
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		for _, old := range []time.Time{v.Growth.Character.UpdatedAt, v.Growth.Skill.UpdatedAt, v.Quests.UpdatedAt} {
			if !at.After(old) {
				at = old.Add(time.Microsecond)
			}
		}
		result = ProfessionReceipt{Kind: "profession-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, Before: before, Definition: d, DefinitionRevision: ProfessionDefinitionRevision(d), ResetSkills: plan.ResetSkills, Revision: c.Revision, Reason: c.Reason, Actor: c.Actor, CreatedAt: at, SkillChanges: []ProfessionSkillChange{}, CreatedQuestIDs: []int64{}, CompletedQuestIDs: []int64{}, RetainedQuestIDs: []int64{}}
		ids := map[int64]bool{}
		for id := range k.Skills {
			ids[id] = true
		}
		for id := range plan.Skills.Skills {
			ids[id] = true
		}
		for id := range ids {
			a, b := k.Skills[id].Level, plan.Skills.Skills[id].Level
			if a != b {
				result.SkillChanges = append(result.SkillChanges, ProfessionSkillChange{id, a, b})
			}
		}
		sort.Slice(result.SkillChanges, func(i, j int) bool { return result.SkillChanges[i].ID < result.SkillChanges[j].ID })
		v.Growth.Character = repository.CloneCharacter(v.Growth.Character)
		v.Growth.Character.Job = strconv.Itoa(d.Job)
		v.Growth.Character.Stats["grow_type"] = int64(d.GrowType)
		v.Growth.Character.Stats["gm_level_sequence_v1"]++
		for key, n := range d.Growth.Stats {
			v.Growth.Character.Stats[key] = n
		}
		v.Growth.Skill = plan.Skills
		v.Growth.Character.UpdatedAt = at
		v.Growth.Skill.UpdatedAt = at
		v.Quests = repository.CloneQuest(v.Quests)
		v.Quests.UpdatedAt = at
		if v.Quests.States == nil {
			v.Quests.States = map[int64]repository.QuestState{}
		}
		changed := []int64{}
		target := map[int64]bool{}
		for _, id := range d.QuestIDs {
			target[id] = true
			a, okA := v.Quests.States[id]
			b, okB := v.Quests.Progress[id]
			if (!okA || professionCompleted(a)) && (!okB || professionCompleted(b)) && (okA || okB) {
				continue
			}
			changed = append(changed, id)
			if !okA && !okB {
				a = repository.QuestState{Extra: map[string]string{"gm_override": "gm_set_grow_type"}}
				okA = true
				result.CreatedQuestIDs = append(result.CreatedQuestIDs, id)
			} else {
				result.CompletedQuestIDs = append(result.CompletedQuestIDs, id)
			}
			if okA {
				a.Status = "completed"
				a.ProgressValue = 0
				a.UpdatedAt = at
				v.Quests.States[id] = a
			}
			if okB {
				b.Status = "completed"
				b.ProgressValue = 0
				b.UpdatedAt = at
				v.Quests.Progress[id] = b
			}
		}
		for _, id := range d.PreviousQuestIDs {
			if !target[id] && (professionCompleted(v.Quests.States[id]) || professionCompleted(v.Quests.Progress[id])) {
				result.RetainedQuestIDs = append(result.RetainedQuestIDs, id)
			}
		}
		result.After, e = ProfessionValues(v)
		if e != nil {
			return e
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
		if e = r.SaveProfessionManagement(ctx, v, changed); e != nil {
			return e
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		result.RequestID = c.Key
		raw, e := json.Marshal(professionStored{result, ProfessionIdentity(c)})
		if e != nil {
			return e
		}
		return receipts.Append(ctx, c.Key, raw)
	})
	return result, e
}
func DecodeProfessionReceipt(raw []byte) (ProfessionReceipt, error) {
	var v professionStored
	e := json.Unmarshal(raw, &v)
	if e == nil && v.Receipt.Kind != "profession-operation" {
		e = ErrProfessionInput
	}
	return v.Receipt, e
}
