package adminops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/expertjob"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var ErrExpertJobState = errors.New("invalid stored expert job state")

type ExpertJobDefinitions struct {
	PVFChecksum string                 `json:"pvfChecksum"`
	Version     int                    `json:"version"`
	Jobs        []expertjob.Definition `json:"jobs"`
	QuestIDs    []int64                `json:"questIds"`
}
type ExpertJobResolver func(context.Context) (ExpertJobDefinitions, error)
type ExpertJobCommand struct {
	AccountID          string `json:"accountId"`
	CharacterID        string `json:"characterId"`
	TargetType         int    `json:"targetType"`
	Mode               string `json:"mode"`
	Value              string `json:"value"`
	Revision           string `json:"revision"`
	PVFChecksum        string `json:"pvfChecksum"`
	DefinitionRevision string `json:"definitionRevision"`
	Reason             string `json:"reason"`
	Actor              string `json:"actor"`
	PlanToken          string `json:"planToken,omitempty"`
	Key                string `json:"-"`
}
type ExpertJobBalance struct {
	Type             int     `json:"type"`
	Name             string  `json:"name"`
	Experience       int64   `json:"experience"`
	Level            int     `json:"level"`
	LearnedRecipes   int     `json:"learnedRecipes"`
	AutoRecipes      []int64 `json:"autoRecipes"`
	MachineGrade     *int64  `json:"machineGrade"`
	MachineEndurance *int64  `json:"machineEndurance"`
	GiveUpState      int64   `json:"giveUpState"`
}
type ExpertJobReceipt struct {
	RequestID          string           `json:"requestId,omitempty"`
	Kind               string           `json:"kind"`
	AccountID          string           `json:"accountId"`
	CharacterID        string           `json:"characterId"`
	Before             ExpertJobBalance `json:"before"`
	After              ExpertJobBalance `json:"after"`
	Reset              bool             `json:"reset"`
	RemovedQuestIDs    []int64          `json:"removedQuestIds"`
	CompletedQuestIDs  []int64          `json:"completedQuestIds"`
	PVFChecksum        string           `json:"pvfChecksum"`
	DefinitionRevision string           `json:"definitionRevision"`
	Revision           string           `json:"revision"`
	Reason             string           `json:"reason"`
	Actor              string           `json:"actor"`
	CreatedAt          time.Time        `json:"createdAt"`
}
type expertJobStored struct {
	Receipt     ExpertJobReceipt `json:"receipt"`
	ContentHash string           `json:"contentHash"`
}

func expertJobHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func ExpertJobRevision(v repository.ExpertJobManagementState) string { return expertJobHash(v) }
func ExpertJobIdentity(c ExpertJobCommand) string                    { return expertJobHash(c) }
func ExpertJobDefinitionRevision(d ExpertJobDefinitions) string      { return expertJobHash(d) }
func validExpertJob(c ExpertJobCommand, commit bool) error {
	id, e := strconv.ParseUint(c.CharacterID, 10, 16)
	value, e2 := strconv.ParseUint(c.Value, 10, 32)
	if e != nil || id == 0 || strconv.FormatUint(id, 10) != c.CharacterID || e2 != nil || strconv.FormatUint(value, 10) != c.Value || c.AccountID == "" || c.Actor == "" || c.TargetType < 0 || c.TargetType > 4 || (c.Mode != "level" && c.Mode != "experience") || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 500 {
		return expertjob.ErrInput
	}
	for _, h := range []string{c.PVFChecksum, c.Revision} {
		b, e := hex.DecodeString(h)
		if e != nil || len(b) != 32 {
			return expertjob.ErrInput
		}
	}
	if c.DefinitionRevision != "" {
		b, e := hex.DecodeString(c.DefinitionRevision)
		if e != nil || len(b) != 32 {
			return expertjob.ErrInput
		}
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128 || c.DefinitionRevision == "") {
		return expertjob.ErrInput
	}
	return nil
}
func ExpertJobValues(v repository.ExpertJobManagementState, d ExpertJobDefinitions) (ExpertJobBalance, error) {
	s := v.Character.Stats
	typ, exp := s["expert_job_type"], s["expert_job_exp"]
	b := ExpertJobBalance{Type: int(typ), Experience: exp, Name: "无副职业", AutoRecipes: []int64{}, GiveUpState: s["expert_job_give_up_state"]}
	if typ < 0 || typ > 4 || exp < 0 || exp > math.MaxUint32 || (typ == 0 && exp != 0) || s["gm_expert_job_sequence_v1"] < 0 || b.GiveUpState < 0 || b.GiveUpState > 255 || len(d.Jobs) != 4 {
		return b, ErrExpertJobState
	}
	for k, n := range s {
		if strings.HasPrefix(k, "expert_job_recipe_") && n != 0 {
			b.LearnedRecipes++
		}
	}
	for key, field := range map[string]**int64{"expert_job_machine_grade": &b.MachineGrade, "expert_job_machine_endurance": &b.MachineEndurance} {
		if n, ok := s[key]; ok {
			*field = &n
		}
	}
	if typ > 0 {
		j := d.Jobs[typ-1]
		if j.Validate() != nil {
			return b, expertjob.ErrDefinition
		}
		b.Name = j.Name
		b.Level = j.Level(exp)
		for l, id := range j.AutoRecipes {
			if l <= b.Level {
				b.AutoRecipes = append(b.AutoRecipes, id)
			}
		}
		sort.Slice(b.AutoRecipes, func(i, j int) bool { return b.AutoRecipes[i] < b.AutoRecipes[j] })
	}
	return b, nil
}
func expertJobReceipt(ctx context.Context, r repository.ManagementReceiptStore, c ExpertJobCommand) (ExpertJobReceipt, bool, error) {
	var old expertJobStored
	raw, found, e := r.Load(ctx, c.Key)
	if e != nil || !found {
		return old.Receipt, found, e
	}
	if e = json.Unmarshal(raw, &old); e != nil {
		return old.Receipt, true, e
	}
	if old.ContentHash != ExpertJobIdentity(c) {
		return old.Receipt, true, ErrIdempotency
	}
	return old.Receipt, true, nil
}
func LookupExpertJobOperation(ctx context.Context, u repository.ExpertJobManagementUnitOfWork, c ExpertJobCommand) (ExpertJobReceipt, bool, error) {
	var result ExpertJobReceipt
	var found bool
	if e := validExpertJob(c, true); e != nil {
		return result, false, e
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	e := u.WithinExpertJobManagement(ctx, c.AccountID, c.CharacterID, func(r repository.ExpertJobManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, e := r.LoadExpertJobManagement(ctx, c.CharacterID, nil)
		if e != nil {
			return e
		}
		if v.Character.AccountID != c.AccountID || v.Character.Stats["delete_flag"] != 0 {
			return ErrAccount
		}
		result, found, e = expertJobReceipt(ctx, receipts, c)
		return e
	})
	return result, found, e
}
func RunExpertJobOperation(ctx context.Context, u repository.ExpertJobManagementUnitOfWork, c ExpertJobCommand, commit bool, resolve ExpertJobResolver, offline QuestOfflineCheck) (ExpertJobReceipt, error) {
	var result ExpertJobReceipt
	if e := validExpertJob(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	e := u.WithinExpertJobManagement(ctx, c.AccountID, c.CharacterID, func(r repository.ExpertJobManagementRepository, receipts repository.ManagementReceiptStore) error {
		v, e := r.LoadExpertJobManagement(ctx, c.CharacterID, nil)
		if e != nil {
			return e
		}
		if v.Character.AccountID != c.AccountID || v.Character.Stats["delete_flag"] != 0 {
			return ErrAccount
		}
		if commit {
			old, found, e := expertJobReceipt(ctx, receipts, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		if resolve == nil {
			return expertjob.ErrDefinition
		}
		d, e := resolve(ctx)
		if e != nil {
			return e
		}
		if d.PVFChecksum != c.PVFChecksum || d.Version != 1 || len(d.Jobs) != 4 || len(d.QuestIDs) == 0 || len(d.QuestIDs) > 256 {
			return expertjob.ErrDefinition
		}
		for i, j := range d.Jobs {
			if j.Type != i+1 || j.Validate() != nil {
				return expertjob.ErrDefinition
			}
		}
		if c.DefinitionRevision != "" && c.DefinitionRevision != ExpertJobDefinitionRevision(d) {
			return expertjob.ErrDefinition
		}
		v, e = r.LoadExpertJobManagement(ctx, c.CharacterID, d.QuestIDs)
		if e != nil {
			return e
		}
		if ExpertJobRevision(v) != c.Revision {
			return ErrConflict
		}
		before, e := ExpertJobValues(v, d)
		if e != nil {
			return e
		}
		if v.Character.Stats["gm_expert_job_sequence_v1"] == math.MaxInt64 {
			return ErrExpertJobState
		}
		exp, _ := strconv.ParseInt(c.Value, 10, 64)
		target := []int64{}
		reset := before.Type != c.TargetType
		if c.TargetType == 0 {
			if exp != 0 {
				return expertjob.ErrInput
			}
		} else {
			j := d.Jobs[c.TargetType-1]
			if reset && v.Character.Level < j.MinimumLevel {
				return expertjob.ErrInput
			}
			target = j.QuestIDs
			if c.Mode == "level" {
				exp, e = j.Experience(int(exp))
				if e != nil {
					return e
				}
			}
		}
		if !reset && before.Experience == exp {
			return expertjob.ErrInput
		}
		at := time.Now().UTC().Truncate(time.Microsecond)
		for _, t := range []time.Time{v.Character.UpdatedAt, v.Quests.UpdatedAt} {
			if !at.After(t) {
				at = t.Add(time.Microsecond)
			}
		}
		result = ExpertJobReceipt{Kind: "expert-job-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, Before: before, Reset: reset, RemovedQuestIDs: []int64{}, CompletedQuestIDs: []int64{}, PVFChecksum: d.PVFChecksum, DefinitionRevision: ExpertJobDefinitionRevision(d), Revision: c.Revision, Reason: c.Reason, Actor: c.Actor, CreatedAt: at}
		v.Character = repository.CloneCharacter(v.Character)
		v.Character.Stats["expert_job_type"] = int64(c.TargetType)
		v.Character.Stats["expert_job_exp"] = exp
		v.Character.Stats["gm_expert_job_sequence_v1"]++
		v.Character.UpdatedAt = at
		if reset {
			for key := range v.Character.Stats {
				if strings.HasPrefix(key, "expert_job_recipe_") || key == "expert_job_machine_grade" || key == "expert_job_machine_endurance" {
					delete(v.Character.Stats, key)
				}
			}
			selected := map[int64]bool{}
			for _, id := range target {
				selected[id] = true
				result.CompletedQuestIDs = append(result.CompletedQuestIDs, id)
			}
			for _, id := range d.QuestIDs {
				_, a := v.Quests.States[id]
				_, b := v.Quests.Progress[id]
				if !selected[id] && (a || b) {
					result.RemovedQuestIDs = append(result.RemovedQuestIDs, id)
				}
			}
		}
		result.After, e = ExpertJobValues(v, d)
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
		if e = r.SaveExpertJobManagement(ctx, v, reset, target); e != nil {
			return e
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		persisted, e := r.LoadExpertJobManagement(ctx, c.CharacterID, d.QuestIDs)
		if e != nil {
			return e
		}
		actual, e := ExpertJobValues(persisted, d)
		if e != nil {
			return e
		}
		if expertJobHash(actual) != expertJobHash(result.After) {
			return ErrConflict
		}
		result.RequestID = c.Key
		result.Revision = ExpertJobRevision(persisted)
		raw, e := json.Marshal(expertJobStored{result, ExpertJobIdentity(c)})
		if e != nil {
			return e
		}
		return receipts.Append(ctx, c.Key, raw)
	})
	return result, e
}
func DecodeExpertJobReceipt(raw []byte) (ExpertJobReceipt, error) {
	var v expertJobStored
	e := json.Unmarshal(raw, &v)
	if e == nil && v.Receipt.Kind != "expert-job-operation" {
		e = expertjob.ErrInput
	}
	return v.Receipt, e
}
