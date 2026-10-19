package gmtool

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/charstat"
	"dnf90.local/gateway/internal/modules/dnf/progression"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func (s *Server) levelDefinition(ctx context.Context, c repository.CharacterRecord, level int) (adminops.LevelDefinition, error) {
	if s.deps.Catalog != nil {
		return s.remoteLevel(ctx, c, level)
	}
	var d adminops.LevelDefinition
	if level < 1 || level > adminops.MaximumCharacterLevel {
		return d, adminops.ErrLevelInput
	}
	if s.pvf == nil || s.deps.PVF == nil {
		return d, errRepositoriesUnavailable
	}
	job, e := strconv.ParseUint(c.Job, 10, 8)
	if e != nil || strconv.FormatUint(job, 10) != c.Job || c.Stats["grow_type"] < 0 || c.Stats["grow_type"] > 255 {
		return d, adminops.ErrLevelState
	}
	source, e := s.source()
	if e != nil {
		return d, e
	}
	hash := itemIndexSourceChecksum(source)
	if hash == "" {
		return d, errRepositoriesUnavailable
	}
	s.pvf.mu.Lock()
	defer s.pvf.mu.Unlock()
	if s.pvf.levelChecksum != hash || s.pvf.levelTables == nil || s.pvf.levelStats == nil {
		tables, e := progression.LoadLevelTables(ctx, source)
		if e != nil {
			return d, e
		}
		stats, e := charstat.Load(ctx, source, charstat.Options{})
		if e != nil {
			return d, e
		}
		s.pvf.levelTables, s.pvf.levelStats, s.pvf.levelChecksum = tables, stats, hash
	}
	d = adminops.LevelDefinition{PVFChecksum: hash, Version: 1, Level: level}
	if level > 1 {
		d.MinimumExperience, e = s.pvf.levelTables.ThresholdToNext(level - 1)
		if e != nil {
			return d, e
		}
	}
	if level < adminops.MaximumCharacterLevel {
		d.NextExperience, e = s.pvf.levelTables.ThresholdToNext(level)
		if e != nil {
			return d, e
		}
	}
	d.SP, e = s.pvf.levelTables.TotalSkillPoints(level)
	if e != nil {
		return d, e
	}
	d.TP, e = s.pvf.levelTables.TotalTechniquePoints(level)
	if e != nil {
		return d, e
	}
	vector, e := s.pvf.levelStats.Compute(byte(job), byte(c.Stats["grow_type"]), level)
	if e != nil {
		return d, e
	}
	d.Stats = statValuesFromVector(withCompatibilityBonus(vector))
	return d, nil
}
func (s *Server) v1Level(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		v1Fail(w, r, 400, "invalid_query", "此接口不接受查询参数", false)
		return
	}
	c, _, ok := s.v1ResourceCharacter(w, r)
	if !ok {
		return
	}
	g, e := s.repositories()
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	u, ok := g.Character.(repository.LevelManagementReader)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	v, e := u.LoadLevelManagement(r.Context(), c.CharacterID)
	if e != nil {
		s.v1LevelResult(w, r, adminops.LevelReceipt{}, e)
		return
	}
	if v.Character.AccountID != c.AccountID || v.Character.Stats["delete_flag"] != 0 {
		s.v1LevelResult(w, r, adminops.LevelReceipt{}, adminops.ErrAccount)
		return
	}
	b, e := adminops.LevelValues(v)
	if e != nil {
		s.v1LevelResult(w, r, adminops.LevelReceipt{}, e)
		return
	}
	d, e := s.levelDefinition(r.Context(), v.Character, v.Character.Level)
	if e != nil {
		s.v1LevelResult(w, r, adminops.LevelReceipt{}, e)
		return
	}
	v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"characterId": c.CharacterID, "accountId": c.AccountID, "balance": b, "definition": d, "revision": adminops.LevelRevision(v), "maximum": adminops.MaximumCharacterLevel}})
}
func (s *Server) v1LevelOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID          string `json:"accountId"`
		TargetLevel        int    `json:"targetLevel"`
		Revision           string `json:"revision"`
		PVFChecksum        string `json:"pvfChecksum"`
		DefinitionRevision string `json:"definitionRevision"`
		Reason             string `json:"reason"`
		PlanToken          string `json:"planToken"`
	}
	if !v1Decode(w, r, &body) {
		return
	}
	commit := !strings.HasSuffix(r.URL.Path, "/preview")
	key := ""
	if commit {
		var ok bool
		key, ok = v1Idempotency(w, r)
		if !ok {
			return
		}
	}
	c, _, ok := s.v1ResourceCharacter(w, r)
	if !ok {
		return
	}
	if c.AccountID != body.AccountID {
		v1Fail(w, r, 409, "owner_conflict", "角色所属账号已变更，请重新打开角色", false)
		return
	}
	g, e := s.repositories()
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	u, ok := g.Character.(repository.LevelManagementUnitOfWork)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	actor := "管理员"
	if s.cfg.LoginKeySHA256 != "" {
		actor = "管理凭据 " + s.cfg.LoginKeySHA256[:min(12, len(s.cfg.LoginKeySHA256))]
	}
	command := adminops.LevelCommand{AccountID: c.AccountID, CharacterID: c.CharacterID, TargetLevel: body.TargetLevel, Revision: body.Revision, PVFChecksum: body.PVFChecksum, DefinitionRevision: body.DefinitionRevision, Reason: body.Reason, Actor: actor, PlanToken: body.PlanToken, Key: key}
	if commit {
		old, found, e := adminops.LookupLevelOperation(r.Context(), u, command)
		if found || e != nil {
			s.v1LevelResult(w, r, old, e)
			return
		}
		identity := command
		identity.PlanToken = ""
		raw, e := s.v1CursorAfter(command.PlanToken, "level-plan", adminops.LevelIdentity(identity))
		at, parseErr := strconv.ParseInt(raw, 10, 64)
		age := time.Since(time.Unix(at, 0))
		if e != nil || parseErr != nil || age < 0 || age > 10*time.Minute {
			v1Fail(w, r, 409, "plan_conflict", "等级预览已过期或内容已变更，请重新预览", false)
			return
		}
	} else {
		command.PlanToken = ""
	}
	result, e := adminops.RunLevelOperation(r.Context(), u, command, commit, s.levelDefinition, s.v1CharacterEditCheck(command.AccountID))
	if e == nil && !commit {
		command.DefinitionRevision = result.DefinitionRevision
		v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"receipt": result, "definitionRevision": result.DefinitionRevision, "planToken": s.v1CursorToken("level-plan", adminops.LevelIdentity(command), strconv.FormatInt(time.Now().Unix(), 10))}})
		return
	}
	s.v1LevelResult(w, r, result, e)
}
func (s *Server) v1LevelResult(w http.ResponseWriter, r *http.Request, result adminops.LevelReceipt, e error) {
	switch {
	case e == nil:
		v1Reply(w, r, 200, v1Envelope{Data: result})
	case errors.Is(e, adminops.ErrConflict):
		v1Fail(w, r, 409, "revision_conflict", "角色成长或技能点已变更，请刷新后重新预览", false)
	case errors.Is(e, adminops.ErrLevelDefinition):
		v1Fail(w, r, 409, "pvf_conflict", "PVF成长定义已变更，请刷新后重新预览", false)
	case errors.Is(e, adminops.ErrLevelInput):
		v1Fail(w, r, 422, "invalid_level", "请选择1–90级并填写操作原因", false)
	case errors.Is(e, adminops.ErrLevelState):
		v1Fail(w, r, 422, "invalid_level_state", "角色等级、经验或职业存档超出范围，请核对存档", false)
	default:
		s.v1SkillPointResult(w, r, adminops.SkillPointReceipt{}, e)
	}
}
func (s *Server) v1LevelLog(w http.ResponseWriter, r *http.Request) {
	q, e := v1ParseQuery(r)
	if e != nil || q.Q != "" {
		v1Fail(w, r, 400, "invalid_query", "等级记录查询参数无效", false)
		return
	}
	c, _, ok := s.v1ResourceCharacter(w, r)
	if !ok {
		return
	}
	g, e := s.repositories()
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	u, ok := g.Character.(repository.LevelManagementUnitOfWork)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	filter := v1Filter(q, c.CharacterID+"\x00"+c.AccountID)
	raw, e := s.v1CursorAfter(q.Cursor, "level-log", filter)
	var before int64
	if e == nil && raw != "" {
		before, e = strconv.ParseInt(raw, 10, 64)
	}
	if e != nil || before < 0 {
		v1Fail(w, r, 400, "invalid_cursor", "等级记录分页条件已变更", false)
		return
	}
	rows, e := u.LevelManagementLog(r.Context(), c.CharacterID, before, q.Limit+1)
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	page := v1Page{Limit: q.Limit}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		page.NextCursor = s.v1CursorToken("level-log", filter, strconv.FormatInt(rows[len(rows)-1].Sequence, 10))
	}
	items := []adminops.LevelReceipt{}
	for _, row := range rows {
		v, e := adminops.DecodeLevelReceipt(row.Payload)
		if e != nil {
			s.v1Unavailable(w, r, e)
			return
		}
		items = append(items, v)
	}
	v1Reply(w, r, 200, v1Envelope{Data: items, Page: &page})
}
