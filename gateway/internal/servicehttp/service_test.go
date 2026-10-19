package servicehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivateIdentityGenerationAndBrowserIsolation(t *testing.T) {
	token, hash := strings.Repeat("t", 48), strings.Repeat("a", 64)
	calls := 0
	h := Protect(token, hash, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Authenticated(r) {
			t.Error("identity missing")
		}
		calls++
		w.Write([]byte(`{"ok":true}`))
	}))
	server := httptest.NewServer(h)
	defer server.Close()
	c, e := New(Config{URL: server.URL, Token: token, PVFSHA256: hash})
	if e != nil {
		t.Fatal(e)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if e = c.JSON(context.Background(), "/resolve", struct{}{}, &out); e != nil || !out.OK {
		t.Fatal(e, out)
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Del("Authorization") },
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 48)) },
		func(r *http.Request) { r.Header.Set("X-PVF-SHA256", strings.Repeat("b", 64)) },
		func(r *http.Request) { r.Header.Set("X-PurpleMagic-Protocol", "old") },
		func(r *http.Request) { r.Header.Set("Origin", "https://example.test") },
		func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "dfo_gm_session", Value: "test"}) },
	} {
		r := httptest.NewRequest("POST", "/resolve", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-PVF-SHA256", hash)
		r.Header.Set("X-PurpleMagic-Protocol", Version)
		mutate(r)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if calls != 1 {
		t.Fatal("unauthenticated call reached handler", calls)
	}
}
func TestPeerRejectsRedirectAndChangedGeneration(t *testing.T) {
	for _, mode := range []string{"redirect", "generation"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, "https://example.test/", 302)
					return
				}
				w.Header().Set("X-PurpleMagic-Protocol", Version)
				w.Header().Set("X-PVF-SHA256", strings.Repeat("b", 64))
				w.Write([]byte(`{}`))
			}))
			defer s.Close()
			c, e := New(Config{URL: s.URL, Token: strings.Repeat("s", 32), PVFSHA256: strings.Repeat("a", 64)})
			if e != nil {
				t.Fatal(e)
			}
			var out any
			if c.JSON(context.Background(), "/resolve", struct{}{}, &out) == nil {
				t.Fatal("invalid peer response accepted")
			}
		})
	}
	for _, url := range []string{"http://example.test", "https://user:password@example.test", "https://example.test/base", "https://example.test/?x=1"} {
		if _, e := New(Config{URL: url, Token: strings.Repeat("s", 32), PVFSHA256: strings.Repeat("a", 64)}); e == nil {
			t.Fatal("invalid peer config", url)
		}
	}
}
