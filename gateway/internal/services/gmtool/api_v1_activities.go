package gmtool

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/onlineevent"
)

var errActivityUnavailable = errors.New("activity controls unavailable")

type v1ActivityChange struct {
	ID      uint16 `json:"id"`
	Enabled *bool  `json:"enabled"`
}

func (s *Server) activityAvailable() bool {
	return s.deps.Runtime != nil || s.deps.Activities != nil
}

func (s *Server) activitySnapshot(ctx context.Context) (onlineevent.ControlSnapshot, error) {
	if s.deps.Runtime != nil {
		result, err := s.deps.Runtime.Call(ctx, adminapi.Request{Action: adminapi.ActivityAction})
		if err != nil {
			return onlineevent.ControlSnapshot{}, err
		}
		if result.Activities == nil {
			return onlineevent.ControlSnapshot{}, errors.New("game activity response omitted controls")
		}
		return *result.Activities, nil
	}
	if s.deps.Activities != nil {
		return s.deps.Activities.Snapshot(), nil
	}
	return onlineevent.ControlSnapshot{}, errActivityUnavailable
}

func (s *Server) saveActivitySnapshot(ctx context.Context, revision uint64, changes []onlineevent.ControlChange) (onlineevent.ControlSnapshot, error) {
	if s.deps.Runtime != nil {
		result, err := s.deps.Runtime.Call(ctx, adminapi.Request{Action: adminapi.SaveActivityAction, Revision: revision, Changes: changes})
		if err != nil {
			return onlineevent.ControlSnapshot{}, err
		}
		if result.Activities == nil {
			return onlineevent.ControlSnapshot{}, errors.New("game activity response omitted controls")
		}
		return *result.Activities, nil
	}
	if s.deps.Activities == nil {
		return onlineevent.ControlSnapshot{}, errActivityUnavailable
	}
	return s.deps.Activities.Save(ctx, revision, changes)
}

func (s *Server) v1ActivityData(snapshot onlineevent.ControlSnapshot) map[string]any {
	return map[string]any{
		"activityControls": snapshot,
		"restartAvailable": s.deps.RestartGame != nil,
	}
}

func (s *Server) v1ActivityUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	if s.cfg.Logger != nil {
		s.cfg.Logger("gm-v1-activity-unavailable", "request_id", r.Context().Value(v1RequestIDKey{}), "cause", err.Error())
	}
	v1Fail(w, r, 503, "activity_unavailable", "活动设置暂时无法读取，请稍后重试", true)
}

func (s *Server) v1Activities(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		v1Fail(w, r, 400, "invalid_query", "此接口不接受查询参数", false)
		return
	}
	snapshot, err := s.activitySnapshot(r.Context())
	if err != nil {
		s.v1ActivityUnavailable(w, r, err)
		return
	}
	v1Reply(w, r, 200, v1Envelope{Data: s.v1ActivityData(snapshot)})
}

func (s *Server) v1SaveActivities(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision   *uint64            `json:"revision"`
		Activities []v1ActivityChange `json:"activities"`
	}
	if !v1Decode(w, r, &input) {
		return
	}
	if input.Revision == nil {
		v1Fail(w, r, 422, "revision_required", "缺少活动设置版本，请刷新后重试", false)
		return
	}
	if len(input.Activities) == 0 {
		v1Fail(w, r, 422, "activity_required", "请选择受支持的活动", false)
		return
	}
	changes := make([]onlineevent.ControlChange, 0, len(input.Activities))
	seen := make(map[uint16]bool, len(input.Activities))
	for _, row := range input.Activities {
		if row.Enabled == nil {
			v1Fail(w, r, 422, "enabled_required", "缺少活动开启状态", false)
			return
		}
		if seen[row.ID] {
			v1Fail(w, r, 422, "duplicate_activity", "活动设置中包含重复活动", false)
			return
		}
		seen[row.ID] = true
		changes = append(changes, onlineevent.ControlChange{ID: row.ID, Enabled: *row.Enabled})
	}

	current, err := s.activitySnapshot(r.Context())
	if err != nil {
		s.v1ActivityUnavailable(w, r, err)
		return
	}
	if current.Revision != *input.Revision {
		v1Fail(w, r, 409, "revision_conflict", "活动设置已更新，请刷新后重试", false)
		return
	}
	supported := make(map[uint16]bool, len(current.Activities))
	values := make(map[uint16]bool, len(current.Activities))
	for _, row := range current.Activities {
		supported[row.ID] = true
		values[row.ID] = row.Enabled
	}
	for _, change := range changes {
		if !supported[change.ID] {
			v1Fail(w, r, 422, "unsupported_activity", "不支持开启活动 "+strconv.Itoa(int(change.ID)), false)
			return
		}
	}
	// Do not turn unchanged defaults into explicit overrides. Older clients may
	// submit the complete table; the game only receives actual value changes.
	delta := changes[:0]
	for _, change := range changes {
		if values[change.ID] != change.Enabled {
			delta = append(delta, change)
		}
	}
	if len(delta) == 0 {
		v1Reply(w, r, 200, v1Envelope{Data: s.v1ActivityData(current)})
		return
	}

	snapshot, err := s.saveActivitySnapshot(r.Context(), *input.Revision, delta)
	if err != nil {
		// A concurrent save or a lost response can make the game-side revision
		// newer while the write failed. Re-read once so the page receives the
		// actionable conflict instead of retrying the same stale revision.
		if latest, readErr := s.activitySnapshot(r.Context()); readErr == nil && latest.Revision != *input.Revision {
			v1Fail(w, r, 409, "revision_conflict", "活动设置已更新，请刷新后重试", false)
			return
		}
		s.logEvent("gm-v1-activity-save-failed", "request_id", r.Context().Value(v1RequestIDKey{}), "cause", err.Error())
		v1Fail(w, r, 503, "activity_save_failed", "活动设置未保存，请刷新后重试", true)
		return
	}
	s.logEvent("gm-activity-settings-saved", "revision", snapshot.Revision, "restart_required", snapshot.RestartRequired)
	v1Reply(w, r, 200, v1Envelope{Data: s.v1ActivityData(snapshot)})
}

// v1RestartActivities is a separate confirmation after a successful save. It
// never forwards a command: only the current revision reaches the fixed local
// helper, and the game-side snapshot must still require a restart.
func (s *Server) v1RestartActivities(w http.ResponseWriter, r *http.Request) {
	if s.deps.RestartGame == nil {
		v1Fail(w, r, 503, "restart_unavailable", "当前部署未提供游戏服务器重启功能，请联系管理员重启", false)
		return
	}
	var input struct {
		Revision *uint64 `json:"revision"`
	}
	if !v1Decode(w, r, &input) {
		return
	}
	if input.Revision == nil {
		v1Fail(w, r, 422, "revision_required", "缺少活动设置版本，请刷新后重试", false)
		return
	}

	s.activityRestartMu.Lock()
	defer s.activityRestartMu.Unlock()
	snapshot, err := s.activitySnapshot(r.Context())
	if err != nil {
		s.v1ActivityUnavailable(w, r, err)
		return
	}
	if snapshot.Revision != *input.Revision {
		v1Fail(w, r, 409, "revision_conflict", "活动设置已更新，请刷新后重新确认重启", false)
		return
	}
	if !snapshot.RestartRequired {
		v1Fail(w, r, 409, "restart_not_required", "活动设置已生效，无需重启游戏服务器", false)
		return
	}
	if s.activityRestartRevision != nil && *s.activityRestartRevision == snapshot.Revision {
		v1Fail(w, r, 409, "restart_already_requested", "本次重启已提交，请稍后刷新活动设置", false)
		return
	}
	if err := s.deps.RestartGame(r.Context(), snapshot.Revision); err != nil {
		s.logEvent("gm-activity-server-restart-failed", "revision", snapshot.Revision, "cause", err.Error())
		v1Fail(w, r, 503, "restart_failed", "暂未取得重启确认，请刷新运行状态后再核对请求", true)
		return
	}
	revision := snapshot.Revision
	s.activityRestartRevision = &revision
	s.logEvent("gm-activity-server-restart-requested", "revision", revision)
	v1Reply(w, r, 200, v1Envelope{Data: map[string]any{"restartRequested": true, "revision": revision}})
}
