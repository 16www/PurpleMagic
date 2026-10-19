package gmtool

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rememberedFixture(t *testing.T, cfg Config) *Server {
	t.Helper()
	s := &Server{cfg: cfg, sessions: map[string]time.Time{}, failures: map[string][]time.Time{}}
	if err := s.loadRememberedSessions(); err != nil {
		t.Fatal(err)
	}
	return s
}
func rememberedLogin(t *testing.T, s *Server, remember bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(loginRequest{Password: s.cfg.Password, Remember: remember})
	r := httptest.NewRequest("POST", "https://gm.test/api/auth/login", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ManagementHandler("").ServeHTTP(w, r)
	return w
}
func rememberedRequest(cookie *http.Cookie) *http.Request {
	r := httptest.NewRequest("GET", "https://gm.test/api/v1/capabilities", nil)
	r.AddCookie(cookie)
	return r
}

func TestRememberedSessionSurvivesRestartAndLogoutRevokes(t *testing.T) {
	cfg := Config{Password: strings.Repeat("p", 32), SessionStorePath: filepath.Join(t.TempDir(), "sessions.json")}
	s := rememberedFixture(t, cfg)
	w := rememberedLogin(t, s, true)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("expected persistent cookie", w.Body.String())
	}
	c := cookies[0]
	if c.MaxAge != 30*24*60*60 || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || time.Until(c.Expires) < rememberedSessionTTL-time.Minute {
		t.Fatal("invalid persistent cookie attributes")
	}
	raw, err := os.ReadFile(cfg.SessionStorePath)
	if err != nil || bytes.Contains(raw, []byte(c.Value)) || bytes.Contains(raw, []byte(cfg.Password)) {
		t.Fatal("credentials must not be stored verbatim")
	}
	restarted := rememberedFixture(t, cfg)
	r := rememberedRequest(c)
	if !restarted.authenticated(r) || restarted.v1CSRF(r) == "" {
		t.Fatal("restart lost remembered authentication")
	}
	w = httptest.NewRecorder()
	restarted.ManagementHandler("").ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	logout := httptest.NewRequest("POST", "https://gm.test/api/auth/logout", strings.NewReader("{}"))
	logout.AddCookie(c)
	w = httptest.NewRecorder()
	restarted.ManagementHandler("").ServeHTTP(w, logout)
	if len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if rememberedFixture(t, cfg).authenticated(r) {
		t.Fatal("logout did not persist revocation")
	}
}

func TestRememberedSessionExpiryRotationAndTemporaryLogin(t *testing.T) {
	cfg := Config{Password: strings.Repeat("q", 32), SessionStorePath: filepath.Join(t.TempDir(), "sessions.json")}
	s := rememberedFixture(t, cfg)
	c := rememberedLogin(t, s, true).Result().Cookies()[0]
	r := rememberedRequest(c)
	s.remembered[sessionDigest(c.Value)] = time.Now().Add(-time.Second)
	if s.authenticated(r) {
		t.Fatal("expired cookie accepted")
	}
	if err := s.saveRememberedSessions(); err != nil {
		t.Fatal(err)
	}
	if rememberedFixture(t, cfg).authenticated(r) {
		t.Fatal("expired cookie survived reload")
	}
	c = rememberedLogin(t, s, true).Result().Cookies()[0]
	r = rememberedRequest(c)
	rotated := cfg
	rotated.Password = strings.Repeat("r", 32)
	if rememberedFixture(t, rotated).authenticated(r) {
		t.Fatal("old token survived credential rotation")
	}
	c = rememberedLogin(t, s, false).Result().Cookies()[0]
	if c.MaxAge != int(sessionTTL/time.Second) {
		t.Fatal("temporary login TTL changed")
	}
	if rememberedFixture(t, cfg).authenticated(rememberedRequest(c)) {
		t.Fatal("temporary login was persisted")
	}
}

func TestRememberedSessionFailureDoesNotGrantAccess(t *testing.T) {
	cfg := Config{Password: strings.Repeat("x", 32)}
	s := rememberedFixture(t, cfg)
	if w := rememberedLogin(t, s, true); len(w.Result().Cookies()) != 0 || len(s.remembered) != 0 {
		t.Fatal("unpersisted session issued")
	}
	s.cfg.SessionStorePath = filepath.Join(t.TempDir(), "sessions.json")
	c := rememberedLogin(t, s, true).Result().Cookies()[0]
	s.cfg.SessionStorePath = filepath.Join(s.cfg.SessionStorePath, "blocked") // parent is a regular file
	if err := s.endSession(c.Value); err == nil {
		t.Fatal("revocation persistence failure ignored")
	}
	if !s.authenticated(rememberedRequest(c)) {
		t.Fatal("failed revocation must leave original state")
	}
	r := httptest.NewRequest("POST", "https://gm.test/api/auth/login", strings.NewReader(`{"password":"wrong","remember":true}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ManagementHandler("").ServeHTTP(w, r)
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid token accepted")
	}
	r = httptest.NewRequest("POST", "https://gm.test/api/auth/logout", strings.NewReader("{}"))
	r.AddCookie(c)
	r.Header.Set("Origin", "https://other.test")
	w = httptest.NewRecorder()
	s.ManagementHandler("").ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin logout accepted", w.Code)
	}
}
