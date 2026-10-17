package gmtool

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func (s *Server) v1CleanupContext(w http.ResponseWriter, r *http.Request) (repository.CharacterSummary, repository.MailboxManagementUnitOfWork, bool) {
	c, _, ok := s.v1ResourceCharacter(w, r)
	if !ok {
		return c, nil, false
	}
	g, e := s.repositories()
	if e != nil {
		s.v1Unavailable(w, r, e)
		return c, nil, false
	}
	u, ok := g.Character.(repository.MailboxManagementUnitOfWork)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return c, nil, false
	}
	return c, u, true
}
func (s *Server) v1CleanupActor() string {
	if s.cfg.LoginKeySHA256 != "" {
		return "管理凭据 " + s.cfg.LoginKeySHA256[:12]
	}
	return "管理员"
}
func (s *Server) v1CleanupResult(w http.ResponseWriter, r *http.Request, j adminops.MailboxCleanup, e error) {
	if errors.Is(e, adminops.ErrMailboxInput) {
		v1Fail(w, r, 422, "invalid_mailbox_cleanup", "请核对清理范围、原因和任务版本", false)
		return
	}
	if errors.Is(e, adminops.ErrConflict) {
		v1Fail(w, r, 409, "cleanup_conflict", "清理预览或任务版本已变更，请刷新后重试", false)
		return
	}
	if errors.Is(e, adminops.ErrCleanupMissing) {
		v1Fail(w, r, 404, "cleanup_not_found", "邮箱清理任务不存在", false)
		return
	}
	if e != nil {
		s.v1MailboxOperationResult(w, r, adminops.MailboxReceipt{}, e)
		return
	}
	data := map[string]any{"job": j}
	if j.Status == "ready" {
		data["planToken"] = s.v1CursorToken("mailbox-cleanup-plan", adminops.CleanupPlanIdentity(j), strconv.FormatInt(time.Now().Unix(), 10))
	}
	v1Reply(w, r, 200, v1Envelope{Data: data})
}
func (s *Server) v1CreateMailboxCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID        string                   `json:"accountId"`
		Filter           repository.MailboxFilter `json:"filter"`
		DiscardUnclaimed bool                     `json:"discardUnclaimed"`
		Reason           string                   `json:"reason"`
	}
	if !v1Decode(w, r, &body) {
		return
	}
	key, ok := v1Idempotency(w, r)
	if !ok {
		return
	}
	c, u, ok := s.v1CleanupContext(w, r)
	if !ok {
		return
	}
	if body.AccountID != c.AccountID {
		v1Fail(w, r, 409, "owner_conflict", "角色归属已变更，请重新打开角色", false)
		return
	}
	j, e := adminops.CreateMailboxCleanup(r.Context(), u, adminops.MailboxCleanupCommand{AccountID: c.AccountID, CharacterID: c.CharacterID, Filter: body.Filter, DiscardUnclaimed: body.DiscardUnclaimed, Reason: body.Reason, Actor: s.v1CleanupActor(), Key: key})
	s.v1CleanupResult(w, r, j, e)
}
func (s *Server) v1AdvanceMailboxCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Revision  uint64 `json:"revision"`
		PlanToken string `json:"planToken"`
		Confirm   bool   `json:"confirm"`
	}
	if !v1Decode(w, r, &body) {
		return
	}
	c, u, ok := s.v1CleanupContext(w, r)
	if !ok {
		return
	}
	confirm := func(j adminops.MailboxCleanup) error {
		if !body.Confirm {
			return adminops.ErrConflict
		}
		raw, e := s.v1CursorAfter(body.PlanToken, "mailbox-cleanup-plan", adminops.CleanupPlanIdentity(j))
		at, pe := strconv.ParseInt(raw, 10, 64)
		age := time.Since(time.Unix(at, 0))
		if e != nil || pe != nil || age < 0 || age > 10*time.Minute {
			return adminops.ErrConflict
		}
		return nil
	}
	j, e := adminops.AdvanceMailboxCleanup(r.Context(), u, c.AccountID, c.CharacterID, r.PathValue("cleanupId"), r.PathValue("action"), s.v1CleanupActor(), body.Revision, confirm, s.v1QuestOffline)
	s.v1CleanupResult(w, r, j, e)
}
func (s *Server) v1MailboxCleanup(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		v1Fail(w, r, 400, "invalid_query", "任务详情不接受查询参数", false)
		return
	}
	c, u, ok := s.v1CleanupContext(w, r)
	if !ok {
		return
	}
	j, _, e := adminops.ReadMailboxCleanup(r.Context(), u, c.AccountID, c.CharacterID, r.PathValue("cleanupId"), 0, 0)
	s.v1CleanupResult(w, r, j, e)
}
func (s *Server) v1MailboxCleanups(w http.ResponseWriter, r *http.Request) {
	q, e := v1ParseQuery(r)
	if e != nil || q.Q != "" {
		v1Fail(w, r, 400, "invalid_query", "清理记录查询参数无效", false)
		return
	}
	c, u, ok := s.v1CleanupContext(w, r)
	if !ok {
		return
	}
	reader, ok := u.(repository.MailboxCleanupReader)
	if !ok {
		s.v1Unavailable(w, r, errRepositoriesUnavailable)
		return
	}
	filter := v1Filter(q, c.AccountID+"\x00"+c.CharacterID)
	raw, e := s.v1CursorAfter(q.Cursor, "mailbox-cleanups", filter)
	var before int64
	if e == nil && raw != "" {
		before, e = strconv.ParseInt(raw, 10, 64)
	}
	if e != nil || before < 0 {
		v1Fail(w, r, 400, "invalid_cursor", "清理记录分页条件已变更", false)
		return
	}
	rows, e := reader.MailboxCleanupLog(r.Context(), c.CharacterID, before, q.Limit+1)
	if e != nil {
		s.v1Unavailable(w, r, e)
		return
	}
	page := v1Page{Limit: q.Limit}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		page.NextCursor = s.v1CursorToken("mailbox-cleanups", filter, strconv.FormatInt(rows[len(rows)-1].Sequence, 10))
	}
	items := []adminops.MailboxCleanup{}
	for _, row := range rows {
		j, e := adminops.DecodeMailboxCleanup(row.Payload)
		if e != nil {
			s.v1Unavailable(w, r, e)
			return
		}
		items = append(items, j)
	}
	v1Reply(w, r, 200, v1Envelope{Data: items, Page: &page})
}
func (s *Server) v1MailboxCleanupItems(w http.ResponseWriter, r *http.Request) {
	q, e := v1ParseQuery(r)
	if e != nil || q.Q != "" {
		v1Fail(w, r, 400, "invalid_query", "清理名单查询参数无效", false)
		return
	}
	c, u, ok := s.v1CleanupContext(w, r)
	if !ok {
		return
	}
	id := r.PathValue("cleanupId")
	filter := v1Filter(q, c.AccountID+"\x00"+c.CharacterID+"\x00"+id)
	raw, e := s.v1CursorAfter(q.Cursor, "mailbox-cleanup-items", filter)
	var after int64
	if e == nil && raw != "" {
		after, e = strconv.ParseInt(raw, 10, 64)
	}
	if e != nil || after < 0 {
		v1Fail(w, r, 400, "invalid_cursor", "清理名单分页条件已变更", false)
		return
	}
	j, rows, e := adminops.ReadMailboxCleanup(r.Context(), u, c.AccountID, c.CharacterID, id, after, q.Limit+1)
	if e != nil {
		s.v1CleanupResult(w, r, j, e)
		return
	}
	page := v1Page{Limit: q.Limit}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		page.NextCursor = s.v1CursorToken("mailbox-cleanup-items", filter, strconv.FormatInt(rows[len(rows)-1].Ordinal, 10))
	}
	items := []map[string]any{}
	for _, row := range rows {
		var change adminops.MailboxChange
		if e = json.Unmarshal(row.Payload, &change); e != nil {
			s.v1Unavailable(w, r, e)
			return
		}
		items = append(items, map[string]any{"ordinal": row.Ordinal, "change": change, "processed": row.Ordinal <= j.Processed})
	}
	v1Reply(w, r, 200, v1Envelope{Data: items, Page: &page})
}
