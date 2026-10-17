package gmtool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func (s *Server) v1CampaignDefinition(entry itemEntry) (*testbenefits.ItemDefinition, error) {
	source, err := s.source()
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errRepositoriesUnavailable
	}
	text, err := source.ReadText(entry.documentPath())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("empty item definition")
	}
	sum := sha256.Sum256([]byte(text))
	return &testbenefits.ItemDefinition{UsablePeriodDays: entry.UsablePeriodDays, AbsoluteExpireUnix: entry.AbsoluteExpireUnix, ScriptSHA256: hex.EncodeToString(sum[:]), Path: entry.documentPath(), Name: entry.Name, Kind: entry.Kind, Tag: entry.Tag, Rarity: entry.Rarity, MinLevel: entry.MinLevel, Grade: entry.Grade, StackLimit: entry.StackLimit}, nil
}

func (s *Server) v1ReprojectCampaign(c testbenefits.Campaign) (testbenefits.PVFProjection, error) {
	j := c.Reward
	if j.RewardPlan == nil || j.Validate() != nil {
		return testbenefits.PVFProjection{}, testbenefits.ErrPVFReview
	}
	a := j.Attachments[0]
	next, name, err := s.v1ProjectCampaignPlan(testbenefits.MailDraft{Title: j.Title, Body: j.Body, Gold: j.Gold, ItemID: strconv.FormatInt(a.ItemID, 10), Count: a.Count, Options: j.RewardPlan.Options, PVFChecksum: s.v1PVFChecksum()})
	return testbenefits.PVFProjection{PVFChecksum: next.PVFChecksum, Attachments: next.Attachments, Definition: next.Definition, ItemName: name}, err
}

func pvfProjectionDTO(p testbenefits.PVFProjection) map[string]any {
	return map[string]any{"pvfChecksum": p.PVFChecksum, "itemName": p.ItemName, "definition": p.Definition, "reward": v1RewardDTO(testbenefits.Job{Attachments: p.Attachments})}
}

func pvfReviewDTO(r testbenefits.PVFReview) map[string]any {
	return map[string]any{"sequence": r.Sequence, "campaignId": r.CampaignID, "createdAt": r.CreatedAt, "actor": r.Actor, "previousRevision": r.PreviousRevision, "revision": r.Revision, "before": pvfProjectionDTO(r.Before), "after": pvfProjectionDTO(r.After)}
}

func (s *Server) v1CampaignPVFPreview(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !v1Decode(w, r, &in) || !s.v1OperationReady(w, r) {
		return
	}
	key, ok := v1Idempotency(w, r)
	if !ok {
		return
	}
	c, err := s.deps.Benefits.Campaign(r.Context(), r.PathValue("id"))
	if err != nil {
		s.v1OperationError(w, r, err)
		return
	}
	p, err := s.v1ReprojectCampaign(c)
	if err != nil {
		s.v1OperationError(w, r, err)
		return
	}
	command := testbenefits.PVFReviewCommand{CampaignID: c.ID, Revision: c.Revision, TargetChecksum: p.PVFChecksum, ProjectionHash: testbenefits.PVFProjectionHash(p)}
	command.PlanToken = s.v1CursorToken("benefit-pvf", testbenefits.PVFReviewIdentity(command, key), strconv.FormatInt(time.Now().Unix(), 10))
	v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"id": c.ID, "title": c.Reward.Title, "scope": c.Reward.Scope.Effective(), "reward": v1RewardDTO(c.Reward), "before": pvfProjectionDTO(testbenefits.CampaignProjection(c)), "after": pvfProjectionDTO(p), "canApply": c.Reward.PVFChecksum != p.PVFChecksum, "command": command}})
}

func (s *Server) v1CampaignPVFApply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		testbenefits.PVFReviewCommand
		Confirm bool `json:"confirm"`
	}
	if !v1Decode(w, r, &in) || !s.v1OperationReady(w, r) {
		return
	}
	key, ok := v1Idempotency(w, r)
	if !ok {
		return
	}
	if !in.Confirm || in.CampaignID != r.PathValue("id") {
		v1Fail(w, r, 422, "confirmation_required", "请核对物品版本并确认", false)
		return
	}
	actor := "管理员"
	if s.cfg.LoginKeySHA256 != "" {
		actor = "管理凭据 " + s.cfg.LoginKeySHA256[:min(12, len(s.cfg.LoginKeySHA256))]
	}
	result, err := s.deps.Benefits.ApplyCampaignPVF(r.Context(), key, in.PVFReviewCommand, actor, func(c testbenefits.Campaign) (testbenefits.PVFProjection, error) {
		raw, err := s.v1CursorAfter(in.PlanToken, "benefit-pvf", testbenefits.PVFReviewIdentity(in.PVFReviewCommand, key))
		at, parseErr := strconv.ParseInt(raw, 10, 64)
		age := time.Since(time.Unix(at, 0))
		if err != nil || parseErr != nil || age < 0 || age > 10*time.Minute {
			return testbenefits.PVFProjection{}, &v1MailInputError{409, "plan_conflict", "核对预览已过期或内容已变更，请重新核对"}
		}
		return s.v1ReprojectCampaign(c)
	})
	if err != nil {
		s.v1OperationError(w, r, err)
		return
	}
	v1Reply(w, r, 200, v1Envelope{Data: pvfReviewDTO(result)})
}

func (s *Server) v1CampaignPVFHistory(w http.ResponseWriter, r *http.Request) {
	if !s.v1OperationReady(w, r) {
		return
	}
	q, err := v1ParseQuery(r)
	if err != nil || q.Q != "" {
		v1Fail(w, r, 400, "invalid_query", "请使用分页数量和游标查询", false)
		return
	}
	filter := v1Filter(q, r.PathValue("id"))
	raw, err := s.v1CursorAfter(q.Cursor, "benefit-pvf-history", filter)
	var before uint64
	if err == nil && raw != "" {
		before, err = strconv.ParseUint(raw, 10, 64)
	}
	if err != nil {
		v1Fail(w, r, 400, "invalid_cursor", "核对历史分页条件已变更", false)
		return
	}
	rows, err := s.deps.Benefits.CampaignPVFHistory(r.Context(), r.PathValue("id"), before, q.Limit+1)
	if err != nil {
		s.v1OperationError(w, r, err)
		return
	}
	page := v1Page{Limit: q.Limit}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		page.NextCursor = s.v1CursorToken("benefit-pvf-history", filter, strconv.FormatUint(rows[len(rows)-1].Sequence, 10))
	}
	items := []map[string]any{}
	for _, row := range rows {
		items = append(items, pvfReviewDTO(row))
	}
	v1Reply(w, r, 200, v1Envelope{Data: items, Page: &page})
}
