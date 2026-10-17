package gmtool

import (
	"context"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/expertjob"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) expertJobDefinitions(ctx context.Context) (adminops.ExpertJobDefinitions, error) {
	x, e := s.expertJobResources(ctx)
	if e != nil {
		return adminops.ExpertJobDefinitions{}, e
	}
	return adminops.ExpertJobDefinitions{PVFChecksum: x.PVFChecksum, Version: x.Version, Jobs: x.Jobs, QuestIDs: x.QuestIDs}, nil
}
func (s *Server) v1ExpertJob(w http.ResponseWriter, r *http.Request) {
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
	u, ok := g.Character.(repository.ExpertJobManagementReader)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	d, e := s.expertJobDefinitions(r.Context())
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	v, e := u.LoadExpertJobManagement(r.Context(), c.CharacterID, d.QuestIDs)
	if e != nil {
		s.v1ExpertJobResult(w, r, adminops.ExpertJobReceipt{}, e)
		return
	}
	if v.Character.AccountID != c.AccountID || v.Character.Stats["delete_flag"] != 0 {
		s.v1ExpertJobResult(w, r, adminops.ExpertJobReceipt{}, adminops.ErrAccount)
		return
	}
	b, e := adminops.ExpertJobValues(v, d)
	if e != nil {
		s.v1ExpertJobResult(w, r, adminops.ExpertJobReceipt{}, e)
		return
	}
	v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"accountId": c.AccountID, "characterId": c.CharacterID, "characterLevel": v.Character.Level, "balance": b, "jobs": d.Jobs, "pvfChecksum": d.PVFChecksum, "revision": adminops.ExpertJobRevision(v)}})
}
func (s *Server) v1ExpertJobOperation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID          string `json:"accountId"`
		TargetType         int    `json:"targetType"`
		Mode               string `json:"mode"`
		Value              string `json:"value"`
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
	u, ok := g.Character.(repository.ExpertJobManagementUnitOfWork)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	actor := "管理员"
	if s.cfg.LoginKeySHA256 != "" {
		actor = "管理凭据 " + s.cfg.LoginKeySHA256[:min(12, len(s.cfg.LoginKeySHA256))]
	}
	command := adminops.ExpertJobCommand{AccountID: c.AccountID, CharacterID: c.CharacterID, TargetType: body.TargetType, Mode: body.Mode, Value: body.Value, Revision: body.Revision, PVFChecksum: body.PVFChecksum, DefinitionRevision: body.DefinitionRevision, Reason: body.Reason, Actor: actor, PlanToken: body.PlanToken, Key: key}
	if commit {
		old, found, e := adminops.LookupExpertJobOperation(r.Context(), u, command)
		if found || e != nil {
			s.v1ExpertJobResult(w, r, old, e)
			return
		}
		identity := command
		identity.PlanToken = ""
		raw, e := s.v1CursorAfter(command.PlanToken, "expert-job-plan", adminops.ExpertJobIdentity(identity))
		at, parseErr := strconv.ParseInt(raw, 10, 64)
		age := time.Since(time.Unix(at, 0))
		if e != nil || parseErr != nil || age < 0 || age > 10*time.Minute {
			v1Fail(w, r, 409, "plan_conflict", "副职业预览已过期或内容已变更，请重新预览", false)
			return
		}
	} else {
		command.PlanToken = ""
	}
	result, e := adminops.RunExpertJobOperation(r.Context(), u, command, commit, s.expertJobDefinitions, s.v1QuestOffline)
	if e == nil && !commit {
		command.DefinitionRevision = result.DefinitionRevision
		v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"receipt": result, "definitionRevision": result.DefinitionRevision, "planToken": s.v1CursorToken("expert-job-plan", adminops.ExpertJobIdentity(command), strconv.FormatInt(time.Now().Unix(), 10))}})
		return
	}
	s.v1ExpertJobResult(w, r, result, e)
}
func (s *Server) v1ExpertJobResult(w http.ResponseWriter, r *http.Request, result adminops.ExpertJobReceipt, e error) {
	switch {
	case e == nil:
		v1Reply(w, r, 200, v1Envelope{Data: result})
	case errors.Is(e, expertjob.ErrDefinition):
		v1Fail(w, r, 409, "pvf_conflict", "副职业定义已变更，请刷新后重新预览", false)
	case errors.Is(e, expertjob.ErrInput):
		v1Fail(w, r, 422, "invalid_expert_job", "请选择可用副职业、等级和经验", false)
	case errors.Is(e, adminops.ErrExpertJobState):
		v1Fail(w, r, 422, "invalid_expert_job_state", "副职业存档超出范围，请核对存档", false)
	default:
		s.v1LevelResult(w, r, adminops.LevelReceipt{}, e)
	}
}
func (s *Server) v1ExpertJobLog(w http.ResponseWriter, r *http.Request) {
	q, e := v1ParseQuery(r)
	if e != nil || q.Q != "" {
		v1Fail(w, r, 400, "invalid_query", "副职业记录查询参数无效", false)
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
	u, ok := g.Character.(repository.ExpertJobManagementUnitOfWork)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	filter := v1Filter(q, c.CharacterID+"\x00"+c.AccountID)
	raw, e := s.v1CursorAfter(q.Cursor, "expert-job-log", filter)
	var before int64
	if e == nil && raw != "" {
		before, e = strconv.ParseInt(raw, 10, 64)
	}
	if e != nil || before < 0 {
		v1Fail(w, r, 400, "invalid_cursor", "副职业记录分页条件已变更", false)
		return
	}
	rows, e := u.ExpertJobManagementLog(r.Context(), c.CharacterID, before, q.Limit+1)
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	page := v1Page{Limit: q.Limit}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		page.NextCursor = s.v1CursorToken("expert-job-log", filter, strconv.FormatInt(rows[len(rows)-1].Sequence, 10))
	}
	items := []adminops.ExpertJobReceipt{}
	for _, row := range rows {
		v, e := adminops.DecodeExpertJobReceipt(row.Payload)
		if e != nil {
			s.v1Unavailable(w, r, e)
			return
		}
		items = append(items, v)
	}
	v1Reply(w, r, 200, v1Envelope{Data: items, Page: &page})
}
