package baidudelivery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func fixture(t *testing.T) *Resolver {
	t.Helper()
	dir := t.TempDir()
	c := Config{filepath.Join(dir, "credentials.json"), filepath.Join(dir, "artifacts.json")}
	os.WriteFile(c.CredentialsFile, []byte(`{"AccessToken":"isolated-access","RefreshToken":"isolated-refresh"}`), 0600)
	os.WriteFile(c.ArtifactsFile, []byte(`{"`+strings.Repeat("a", 64)+`":{"fs_id":42,"path":"/approved/blob","size":123}}`), 0600)
	r, e := New(c)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestResolveCacheRefreshAndBoundary(t *testing.T) {
	r := fixture(t)
	calls, heads, refresh := 0, 0, 0
	expired := false
	r.client.Transport = transport(func(q *http.Request) (*http.Response, error) {
		if q.Header.Get("Authorization") != "" || q.Header.Get("Cookie") != "" || q.Header.Get("User-Agent") != "pan.baidu.com" {
			t.Fatal("unsafe headers")
		}
		switch q.URL.Host {
		case "api.oplist.org":
			refresh++
			return response(200, `{"access_token":"new-access","refresh_token":"new-refresh"}`), nil
		case "pan.baidu.com":
			calls++
			if expired && q.URL.Query().Get("access_token") == "isolated-access" {
				return response(200, `{"error_code":110}`), nil
			}
			return response(200, `{"errno":0,"list":[{"fs_id":42,"path":"/approved/blob","size":123,"dlink":"https://d.pcs.baidu.com/file"}]}`), nil
		case "d.pcs.baidu.com":
			heads++
			if q.Method != "HEAD" {
				t.Fatal("not HEAD")
			}
			v := response(302, "")
			v.Header.Set("Location", "https://cdn.baidupcs.com/opaque?signature=short")
			return v, nil
		}
		t.Fatal("unexpected host")
		return nil, nil
	})
	hash := strings.Repeat("a", 64)
	if _, e := r.Resolve(context.Background(), hash, 124, false); e == nil || calls != 0 {
		t.Fatal("unregistered size reached provider")
	}
	for i := 0; i < 2; i++ {
		v, e := r.Resolve(context.Background(), hash, 123, false)
		if e != nil || !strings.Contains(v, "baidupcs.com") {
			t.Fatal(e)
		}
	}
	if calls != 1 || heads != 1 {
		t.Fatal("cache miss")
	}
	expired = true
	if _, e := r.Resolve(context.Background(), hash, 123, true); e != nil {
		t.Fatal(e)
	}
	if refresh != 1 || calls != 3 {
		t.Fatalf("refresh %d calls %d", refresh, calls)
	}
	var saved credentials
	readPrivate(r.cfg.CredentialsFile, &saved)
	if saved.RefreshToken != "new-refresh" {
		t.Fatal("rotation not saved")
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := r.Resolve(context.Background(), hash, 123, false); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls != 3 {
		t.Fatal("cached concurrent calls reached provider")
	}
}
func TestUnsafeProviderAndMetadataFailClosed(t *testing.T) {
	for _, link := range []string{"http://cdn.baidupcs.com/x", "https://evil.test/x", "https://baidupcs.com.evil.test/x", "https://cdn.baidupcs.com:444/x", "https://u:p@cdn.baidupcs.com/x", "https://cdn.baidupcs.com/x#secret", "https://cdn.baidupcs.com/x?access_token=isolated-access"} {
		t.Run(link, func(t *testing.T) {
			r := fixture(t)
			r.client.Transport = transport(func(q *http.Request) (*http.Response, error) {
				if q.Method == "HEAD" {
					v := response(302, "")
					v.Header.Set("Location", link)
					return v, nil
				}
				return response(200, `{"list":[{"fs_id":42,"path":"/approved/blob","size":123,"dlink":"https://d.pcs.baidu.com/file"}]}`), nil
			})
			if _, e := r.Resolve(context.Background(), strings.Repeat("a", 64), 123, false); e == nil {
				t.Fatal("unsafe link accepted")
			}
		})
	}
	for _, body := range []string{`{"list":[]}`, `{"list":[{"fs_id":43,"path":"/approved/blob","size":123,"dlink":"https://d.pcs.baidu.com/x"}]}`, `{"list":[{"fs_id":42,"path":"/other","size":123,"dlink":"https://d.pcs.baidu.com/x"}]}`, `{"list":[{"fs_id":42,"path":"/approved/blob","size":124,"dlink":"https://d.pcs.baidu.com/x"}]}`} {
		r := fixture(t)
		r.client.Transport = transport(func(q *http.Request) (*http.Response, error) {
			if q.Method == "HEAD" {
				t.Fatal("bad metadata resolved")
			}
			return response(200, body), nil
		})
		if _, e := r.Resolve(context.Background(), strings.Repeat("a", 64), 123, false); e == nil {
			t.Fatal("bad identity accepted")
		}
	}
}
func TestCapabilityAndProviderFailureFallback(t *testing.T) {
	r := fixture(t)
	hash := strings.Repeat("a", 64)
	r.cache[hash] = cached{"https://cdn.baidupcs.com/file?opaque=x", time.Now().Add(time.Hour)}
	for _, mode := range []string{"", "unknown", "baidu-v1"} {
		req := httptest.NewRequest("GET", "https://gateway.test/blobs/"+hash, nil)
		req.Header.Set(CapabilityHeader, mode)
		w := httptest.NewRecorder()
		redirected := r.Redirect(w, req, hash, 123)
		if redirected != (mode == Capability) {
			t.Fatal("legacy capability changed")
		}
		if redirected && (w.Code != 307 || w.Header().Get("Cache-Control") != "private, no-store") {
			t.Fatal("redirect cache unsafe")
		}
		if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), CapabilityHeader) {
			t.Fatal("Vary missing")
		}
	}
	r.cache = map[string]cached{}
	calls := 0
	r.client.Transport = transport(func(q *http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("secret=must-not-leak") })
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "https://gateway.test/x", nil)
		req.Header.Set(CapabilityHeader, Capability)
		w := httptest.NewRecorder()
		if r.Redirect(w, req, hash, 123) || w.Body.Len() != 0 {
			t.Fatal("failure did not fall back")
		}
	}
	if calls != 1 {
		t.Fatal("failure cooldown missing")
	}
}
func TestInvalidRegistrationRejected(t *testing.T) {
	r := fixture(t)
	os.WriteFile(r.cfg.ArtifactsFile, []byte(`{"not-a-digest":{"fs_id":42,"path":"/approved/blob","size":123}}`), 0600)
	if _, e := New(r.cfg); e == nil {
		t.Fatal("invalid hash accepted")
	}
	var nilResolver *Resolver
	w := httptest.NewRecorder()
	if nilResolver.Redirect(w, httptest.NewRequest("GET", "https://x/x", nil), "", 1) {
		t.Fatal("disabled resolver changed response")
	}
}
