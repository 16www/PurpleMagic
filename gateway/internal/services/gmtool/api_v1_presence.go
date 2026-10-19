package gmtool

import (
	"net/http"
	"strconv"
)

// The verified complete snapshot distinguishes offline roles from unavailable runtime state.
func (s *Server) v1Presence(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		v1Fail(w, r, 400, "invalid_query", "此接口不接受查询参数", false)
		return
	}
	if s.deps.Runtime == nil {
		v1Fail(w, r, 503, "presence_unavailable", "在线状态暂时无法读取", true)
		return
	}
	snapshot, err := s.deps.Runtime.OnlineSnapshot(r.Context())
	if err != nil {
		v1Fail(w, r, 503, "presence_unavailable", "在线状态暂时无法读取", true)
		return
	}
	ids := make([]string, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		ids = append(ids, strconv.FormatUint(uint64(member.CharacterID), 10))
	}
	v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"characterIds": ids, "observedAt": snapshot.ObservedAt, "complete": true}})
}
