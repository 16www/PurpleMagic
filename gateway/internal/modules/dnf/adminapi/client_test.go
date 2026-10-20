package adminapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/onlineevent"
)

func TestClientRejectsPublicPlaintextAndRedirects(t *testing.T) {
	for _, endpoint := range []string{"http://8.8.8.8" + Path, "http://localhost" + Path, "https://user:pass@example.org" + Path, "https://example.org/wrong"} {
		if _, err := NewClient(endpoint, strings.Repeat("x", 32)); err == nil {
			t.Fatal("accepted unsafe endpoint", endpoint)
		}
	}
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c, err := NewClient(server.URL+Path, strings.Repeat("x", 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Call(context.Background(), Request{Action: "online", CharacterID: 1}); err == nil || called {
		t.Fatal("redirect followed or accepted")
	}
}

func TestClientVersionAuthenticationAndFailure(t *testing.T) {
	good := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Error("missing credential")
		}
		var req Request
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Version != Version || req.CharacterID != 7 {
			t.Error("invalid request")
		}
		json.NewEncoder(w).Encode(Response{Version: Version, OK: good, Online: true})
	}))
	defer server.Close()
	c, _ := NewClient(server.URL+Path, strings.Repeat("x", 32))
	r, err := c.Call(context.Background(), Request{Action: "online", CharacterID: 7})
	if err != nil || !r.Online {
		t.Fatal(r, err)
	}
	good = false
	if _, err = c.Call(context.Background(), Request{Action: "online", CharacterID: 7}); err == nil {
		t.Fatal("failed response accepted")
	}
}

func TestActivityWireContractRetainsPrivateGameFields(t *testing.T) {
	raw, err := json.Marshal(Request{
		Version:  Version,
		Action:   SaveActivityAction,
		Revision: 9,
		Changes:  []onlineevent.ControlChange{{ID: 2347, Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err = json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	if len(request) != 4 || request["version"] != float64(Version) || request["action"] != SaveActivityAction || request["revision"] != float64(9) {
		t.Fatalf("private request fields changed: %s", raw)
	}
	changes := request["changes"].([]any)
	if len(changes) != 1 || len(changes[0].(map[string]any)) != 2 {
		t.Fatalf("private change fields changed: %s", raw)
	}

	var response Response
	if err = json.Unmarshal([]byte(`{"version":1,"ok":true,"activities":{"revision":9,"restartRequired":true,"activities":[{"id":2347,"name":"online","defaultEnabled":true,"enabled":true,"effectiveEnabled":false,"pendingRestart":true}]}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Activities == nil || response.Activities.Revision != 9 || !response.Activities.RestartRequired || len(response.Activities.Activities) != 1 || !response.Activities.Activities[0].Enabled || response.Activities.Activities[0].EffectiveEnabled {
		t.Fatalf("private response fields changed: %+v", response.Activities)
	}
}
