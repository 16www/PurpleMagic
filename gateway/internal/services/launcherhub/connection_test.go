package launcherhub

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestConnectionPublicationIsolationAndRecovery(t *testing.T) {
	h, _ := fixture(t)
	cookie, csrf := auth(t, h, false)
	before := h.state.Published
	rev := connectionRevision(before.Profile)
	write := func(body, revision, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "https://admin.example.test/admin/api/connection", strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://admin.example.test")
		r.Header.Set("X-CSRF-Token", token)
		if revision != "" {
			r.Header.Set("If-Match", `"`+revision+`"`)
		}
		w := httptest.NewRecorder()
		h.AdminHandler().ServeHTTP(w, r)
		return w
	}
	body := `{"gameHost":"new.game.example.test","bootstrapPort":10011}`
	if w := write(body, rev, ""); w.Code != 403 {
		t.Fatal("csrf", w.Code)
	}
	if w := write(body, "", csrf); w.Code != 428 {
		t.Fatal("revision", w.Code)
	}
	if w := write(body, rev, csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	expected := before
	expected.Profile.GameHost = "new.game.example.test"
	expected.Profile.BootstrapPort = 10011
	expected.Sequence++
	if !reflect.DeepEqual(h.state.Published, expected) {
		t.Fatal("unrelated publication fields changed")
	}
	if w := write(body, rev, csrf); w.Code != 200 || h.state.Published.Sequence != expected.Sequence {
		t.Fatal("replay advanced sequence")
	}
	if w := write(`{"gameHost":"another.test","bootstrapPort":10012}`, rev, csrf); w.Code != 409 {
		t.Fatal("stale write accepted")
	}
	for _, invalid := range []string{`{"gameHost":"https://host","bootstrapPort":1}`, `{"gameHost":"a..b","bootstrapPort":1}`, `{"gameHost":"host","bootstrapPort":0}`, `{"gameHost":"host","bootstrapPort":65536}`, `{"gameHost":"host","bootstrapPort":1,"ExeSha256":"bad"}`} {
		if w := write(invalid, rev, csrf); w.Code != 400 {
			t.Fatal("invalid connection accepted", invalid, w.Code)
		}
	}
	restarted, err := New(h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.state.Published, expected) {
		t.Fatal("publication lost on restart")
	}
	w := httptest.NewRecorder()
	restarted.PublicHandler().ServeHTTP(w, httptest.NewRequest("GET", "/launcher/v2/bootstrap.json", nil))
	var env Envelope
	json.Unmarshal(w.Body.Bytes(), &env)
	raw, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var got Bootstrap
	json.Unmarshal(raw, &got)
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("signed bootstrap mismatch")
	}
	for _, method := range []string{"GET", "PUT"} {
		w := httptest.NewRecorder()
		h.PublicHandler().ServeHTTP(w, httptest.NewRequest(method, "/admin/api/connection", strings.NewReader(body)))
		if w.Code != 404 {
			t.Fatal("public admin route exposed")
		}
	}
}
