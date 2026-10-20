package gmtool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/onlineevent"
	"dnf90.local/gateway/internal/modules/dnf/repository/memory"
	"dnf90.local/gateway/internal/servicehttp"
)

// Exercise the actual browser -> external GM -> private gateway path. Neither
// browser cookies nor CSRF tokens can authenticate the service listener itself.
func TestV1ActivitiesExternalServiceBoundary(t *testing.T) {
	controls, err := onlineevent.NewControls(context.Background(), memory.NewMemoryGroup().Settings, []onlineevent.ControlDefinition{{ID: 2347, Name: "online", DefaultEnabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	restarts := 0
	game := &Server{cfg: Config{Password: "fixture"}, deps: Deps{Activities: controls, RestartGame: func(context.Context, uint64) error { restarts++; return nil }}, sessions: map[string]time.Time{}}
	key, hash := strings.Repeat("g", 48), strings.Repeat("a", 64)
	server := httptest.NewServer(game.GatewayHandler(key, hash))
	defer server.Close()
	peer, err := servicehttp.New(servicehttp.Config{URL: server.URL, Token: key, PVFSHA256: hash})
	if err != nil {
		t.Fatal(err)
	}
	external := &Server{cfg: Config{Password: "fixture"}, sessions: map[string]time.Time{}}
	token, err := external.startSession()
	if err != nil {
		t.Fatal(err)
	}
	handler := external.ExternalHandler("", "", peer)
	if status, _ := activityV1Call(t, handler, external, "", http.MethodGet, "/api/v1/activities", "", false); status != 401 {
		t.Fatal(status)
	}
	if status, _ := activityV1Call(t, handler, external, token, http.MethodPost, "/api/v1/activities", `{"revision":0,"activities":[{"id":2347,"enabled":false}]}`, false); status != 403 {
		t.Fatal(status)
	}
	status, out := activityV1Call(t, handler, external, token, http.MethodGet, "/api/v1/activities", "", false)
	if status != 200 || out["data"].(map[string]any)["restartAvailable"] != true {
		t.Fatal(status, out)
	}
	status, out = activityV1Call(t, handler, external, token, http.MethodPost, "/api/v1/activities", `{"revision":0,"activities":[{"id":2347,"enabled":false}]}`, true)
	if status != 200 || restarts != 0 {
		t.Fatal(status, out, restarts)
	}
	if status, out = activityV1Call(t, handler, external, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":1}`, true); status != 200 || restarts != 1 {
		t.Fatal(status, out, restarts)
	}
	if status, _ = activityV1Call(t, handler, external, token, http.MethodPost, "/api/v1/activities/restart", `{"revision":1}`, true); status != 409 || restarts != 1 {
		t.Fatal(status, restarts)
	}
	r := httptest.NewRequest("GET", "/api/v1/activities", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	w := httptest.NewRecorder()
	game.GatewayHandler(key, hash).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
