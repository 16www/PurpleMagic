package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/onlineevent"
	"dnf90.local/gateway/internal/modules/dnf/repository/memory"
)

func activityTestSnapshot(revision uint64, enabled, restartRequired bool) onlineevent.ControlSnapshot {
	snapshot := onlineevent.ControlSnapshot{Revision: revision, RestartRequired: restartRequired}
	snapshot.Activities = []onlineevent.ControlRow{
		{
			ControlDefinition: onlineevent.ControlDefinition{ID: 2347, Name: "累计在线得好礼", Description: "在线奖励", Source: "event2347", DefaultEnabled: true, Group: "常驻功能"},
			Enabled:           enabled,
			EffectiveEnabled:  !enabled,
			PendingRestart:    enabled != !enabled,
		},
		{
			ControlDefinition: onlineevent.ControlDefinition{ID: 2222, Name: "饺子大师", Description: "活动入口", Source: "npc", DefaultEnabled: false, Group: "图片活动NPC", AvailabilityOnly: true},
			Enabled:           true,
			EffectiveEnabled:  true,
		},
	}
	return snapshot
}

func activityV1Call(t *testing.T, handler http.Handler, s *Server, token, method, path, body string, withCSRF bool) (int, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
		if withCSRF {
			request.Header.Set("X-CSRF-Token", s.v1CSRF(request))
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatal(recorder.Code, recorder.Body.String(), err)
	}
	return recorder.Code, out
}

func TestV1ActivitiesRevisionRestartAndPrivateGameFields(t *testing.T) {
	var mu sync.Mutex
	snapshot := activityTestSnapshot(3, false, true)
	var requests []map[string]any
	var saveRequests int
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("g", 32) {
			t.Error("missing private game credential")
		}
		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		requests = append(requests, raw)
		action, _ := raw["action"].(string)
		if action == adminapi.SaveActivityAction {
			saveRequests++
			revision, _ := raw["revision"].(float64)
			if uint64(revision) != snapshot.Revision {
				mu.Unlock()
				http.Error(w, "revision", http.StatusConflict)
				return
			}
			changes, _ := raw["changes"].([]any)
			for _, item := range changes {
				row := item.(map[string]any)
				id := uint16(row["id"].(float64))
				for i := range snapshot.Activities {
					if snapshot.Activities[i].ID == id {
						snapshot.Activities[i].Enabled = row["enabled"].(bool)
					}
				}
			}
			snapshot.Revision++
			snapshot.RestartRequired = true
		}
		out, _ := json.Marshal(adminapi.Response{Version: adminapi.Version, OK: true, Activities: &snapshot})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}))
	defer runtime.Close()
	client, err := adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("g", 32))
	if err != nil {
		t.Fatal(err)
	}
	restartCalls := 0
	restartFailure := errors.New("private helper detail")
	failRestart := true
	s := &Server{
		cfg: Config{Password: "fixture"},
		deps: Deps{Runtime: client, RestartGame: func(context.Context, uint64) error {
			restartCalls++
			if failRestart {
				return restartFailure
			}
			return nil
		}},
		sessions: map[string]time.Time{},
	}
	handler := s.v1Routes()
	if status, out := activityV1Call(t, handler, s, "", http.MethodGet, "/api/v1/activities", "", false); status != 401 || out["error"].(map[string]any)["code"] != "authentication_required" {
		t.Fatal(status, out)
	}
	token, err := s.startSession()
	if err != nil {
		t.Fatal(err)
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", `{"revision":3,"activities":[{"id":2347,"enabled":true}]}`, false); status != 403 {
		t.Fatal("missing CSRF accepted", status, out)
	}
	status, out := activityV1Call(t, handler, s, token, http.MethodGet, "/api/v1/activities", "", false)
	if status != 200 || out["data"].(map[string]any)["restartAvailable"] != true {
		t.Fatal(status, out)
	}
	controls := out["data"].(map[string]any)["activityControls"].(map[string]any)
	if controls["revision"] != float64(3) || controls["restartRequired"] != true {
		t.Fatal(controls)
	}
	rows := controls["activities"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["id"] != float64(2347) {
		t.Fatal(rows)
	}
	if !strings.Contains(controlsToString(rows[1]), `"group":"图片活动NPC"`) || !strings.Contains(controlsToString(rows[1]), `"availabilityOnly":true`) {
		t.Fatal("optional activity metadata was lost", controlsToString(rows[1]))
	}

	for _, body := range []string{
		`{"activities":[{"id":2347,"enabled":true}]}`,
		`{"revision":3,"activities":[]}`,
		`{"revision":3,"activities":[{"id":2347}]}`,
		`{"revision":3,"activities":[{"id":2347,"enabled":true},{"id":2347,"enabled":false}]}`,
		`{"revision":3,"activities":[{"id":2347,"enabled":true}],"command":"restart"}`,
	} {
		if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", body, true); status != 400 && status != 422 {
			t.Fatal("invalid save accepted", body, status, out)
		}
	}
	if saveRequests != 0 {
		t.Fatal("invalid request reached game API", saveRequests)
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", `{"revision":2,"activities":[{"id":2347,"enabled":true}]}`, true); status != 409 || out["error"].(map[string]any)["code"] != "revision_conflict" {
		t.Fatal("stale revision accepted", status, out)
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", `{"revision":3,"activities":[{"id":181,"enabled":true}]}`, true); status != 422 || out["error"].(map[string]any)["code"] != "unsupported_activity" {
		t.Fatal("unsupported activity accepted", status, out)
	}
	if saveRequests != 0 {
		t.Fatal("rejected save reached game API", saveRequests)
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", `{"revision":3,"activities":[{"id":2347,"enabled":false}]}`, true); status != 200 || out["data"].(map[string]any)["activityControls"].(map[string]any)["revision"] != float64(3) || saveRequests != 0 {
		t.Fatal("unchanged settings must not create an override", status, out, saveRequests)
	}

	status, out = activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", `{"revision":3,"activities":[{"id":2347,"enabled":true}]}`, true)
	if status != 200 {
		t.Fatal(status, out)
	}
	saved := out["data"].(map[string]any)["activityControls"].(map[string]any)
	if saved["revision"] != float64(4) || saved["restartRequired"] != true {
		t.Fatal(saved)
	}
	mu.Lock()
	if saveRequests != 1 {
		t.Fatal(saveRequests)
	}
	last := requests[len(requests)-1]
	if len(last) != 4 || last["action"] != adminapi.SaveActivityAction || last["revision"] != float64(3) {
		t.Fatal("private save fields changed", last)
	}
	changes := last["changes"].([]any)[0].(map[string]any)
	if len(changes) != 2 || changes["id"] != float64(2347) || changes["enabled"] != true {
		t.Fatal("private change fields changed", changes)
	}
	mu.Unlock()

	if restartCalls != 0 {
		t.Fatal("save restarted the service")
	}
	for _, body := range []string{`{}`, `{"revision":4,"command":"restart"}`} {
		if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", body, true); status != 400 && status != 422 {
			t.Fatal("invalid restart accepted", body, status, out)
		}
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":3}`, true); status != 409 || out["error"].(map[string]any)["code"] != "revision_conflict" {
		t.Fatal("stale restart accepted", status, out)
	}
	if restartCalls != 0 {
		t.Fatal(restartCalls)
	}
	status, out = activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":4}`, true)
	if status != 503 || strings.Contains(out["error"].(map[string]any)["message"].(string), "private") || restartCalls != 1 {
		t.Fatal("restart failure leaked or retried", status, out, restartCalls)
	}
	failRestart = false
	status, out = activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":4}`, true)
	if status != 200 || out["data"].(map[string]any)["restartRequested"] != true || restartCalls != 2 {
		t.Fatal(status, out, restartCalls)
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":4}`, true); status != 409 || out["error"].(map[string]any)["code"] != "restart_already_requested" || restartCalls != 2 {
		t.Fatal("duplicate restart accepted", status, out, restartCalls)
	}

	mu.Lock()
	snapshot = activityTestSnapshot(5, true, false)
	mu.Unlock()
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":5}`, true); status != 409 || out["error"].(map[string]any)["code"] != "restart_not_required" {
		t.Fatal("unnecessary restart accepted", status, out)
	}
	s.deps.RestartGame = nil
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":5}`, true); status != 503 || out["error"].(map[string]any)["code"] != "restart_unavailable" {
		t.Fatal(status, out)
	}
}

func controlsToString(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TestV1ActivitiesLocalControlsFallback(t *testing.T) {
	controls, err := onlineevent.NewControls(context.Background(), memory.NewMemoryGroup().Settings, []onlineevent.ControlDefinition{{ID: 2347, Name: "online", DefaultEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: Config{Password: "fixture"}, deps: Deps{Activities: controls}, sessions: map[string]time.Time{}}
	handler := s.v1Routes()
	token, err := s.startSession()
	if err != nil {
		t.Fatal(err)
	}
	status, out := activityV1Call(t, handler, s, token, http.MethodGet, "/api/v1/activities", "", false)
	if status != 200 || out["data"].(map[string]any)["restartAvailable"] != false {
		t.Fatal(status, out)
	}
	status, out = activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities", `{"revision":0,"activities":[{"id":2347,"enabled":false}]}`, true)
	if status != 200 {
		t.Fatal(status, out)
	}
	controlsOut := out["data"].(map[string]any)["activityControls"].(map[string]any)
	if controlsOut["revision"] != float64(1) || controlsOut["restartRequired"] != true || !controls.Enabled(2347) {
		t.Fatal(controlsOut, controls.Snapshot())
	}
	if status, out := activityV1Call(t, handler, s, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":1}`, true); status != 503 || out["error"].(map[string]any)["code"] != "restart_unavailable" {
		t.Fatal(status, out)
	}
	status, out = v1TestCall(t, s, token, http.MethodGet, "/api/v1/capabilities")
	features := out["data"].(map[string]any)["features"].(map[string]any)
	if status != 200 || features["activities.read"] != true || features["activities.write"] != true || features["activities.restart"] != false {
		t.Fatal(status, features)
	}
}
