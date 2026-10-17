package gmtool

import (
	"context"
	"net/http"
	"strconv"

	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func (s *Server) v1OnlineMailPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		v1RewardInput
		Options     testbenefits.MailOptions `json:"options"`
		PVFChecksum string                   `json:"pvfChecksum"`
	}
	if !v1Decode(w, r, &in) || !s.v1OperationReady(w, r) {
		return
	}
	key, ok := v1Idempotency(w, r)
	if !ok {
		return
	}
	g, err := s.v1OperationGroup()
	var j testbenefits.Job
	if err == nil {
		draft := testbenefits.MailDraft{Title: in.Title, Body: in.Body, Gold: in.Gold, ItemID: in.ItemID, Count: in.Count, Options: in.Options, PVFChecksum: in.PVFChecksum}
		j, err = s.deps.Benefits.PreviewOnlineMailOnce(r.Context(), g, key, draft, func(ctx context.Context) (testbenefits.OnlineSelection, error) {
			snapshot, e := s.deps.Runtime.OnlineSnapshot(ctx)
			if e != nil {
				return testbenefits.OnlineSelection{}, &v1MailInputError{503, "online_snapshot_unavailable", "无法获取完整在线名单，请检查游戏管理接口"}
			}
			selection := testbenefits.OnlineSelection{Source: testbenefits.RecipientSource{Kind: "online", SnapshotID: snapshot.ID, ObservedAt: snapshot.ObservedAt, Count: len(snapshot.Members)}}
			for _, m := range snapshot.Members {
				selection.Members = append(selection.Members, testbenefits.OnlineMember{CharacterID: strconv.FormatUint(uint64(m.CharacterID), 10), Generation: m.Generation})
			}
			return selection, nil
		}, s.v1ProjectMailDraft)
	}
	if err != nil {
		s.v1OperationError(w, r, err)
		return
	}
	s.v1OperationReply(w, r, v1JobDTO(testbenefits.Summarize(j)))
}
