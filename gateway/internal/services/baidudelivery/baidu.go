// Package baidudelivery resolves only operator-registered immutable artifacts.
// OAuth stays on the gateway; clients receive a short-lived provider URL.
package baidudelivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const CapabilityHeader = "X-DNF90-Bundle-Transport"
const Capability = "baidu-v1"

type Config struct {
	CredentialsFile string `json:"credentials_file"`
	ArtifactsFile   string `json:"artifacts_file"`
}
type Artifact struct {
	FSID int64  `json:"fs_id"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}
type credentials struct{ AccessToken, RefreshToken string }
type cached struct {
	URL   string
	Until time.Time
}
type Resolver struct {
	mu          sync.Mutex
	cfg         Config
	credentials credentials
	artifacts   map[string]Artifact
	cache       map[string]cached
	client      *http.Client
	now         func() time.Time
	retryAfter  time.Time
}

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var unavailable = errors.New("Baidu delivery unavailable")

func readPrivate(name string, value any) error {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return unavailable
	}
	b, err := os.ReadFile(name)
	if err != nil || json.Unmarshal(b, value) != nil {
		return unavailable
	}
	return nil
}

func New(cfg Config) (*Resolver, error) {
	r := &Resolver{cfg: cfg, artifacts: map[string]Artifact{}, cache: map[string]cached{}, now: time.Now,
		client: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil, MaxIdleConnsPerHost: 4}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if readPrivate(cfg.CredentialsFile, &r.credentials) != nil || r.credentials.AccessToken == "" || r.credentials.RefreshToken == "" || readPrivate(cfg.ArtifactsFile, &r.artifacts) != nil || len(r.artifacts) == 0 || len(r.artifacts) > 2000 {
		return nil, unavailable
	}
	for hash, a := range r.artifacts {
		if !hashPattern.MatchString(hash) || a.FSID <= 0 || a.Size <= 0 || a.Size > 8<<30 || !strings.HasPrefix(a.Path, "/") || strings.ContainsAny(a.Path, "\x00\r\n") {
			return nil, unavailable
		}
	}
	return r, nil
}

func provider(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "pan.baidu.com" || h == "d.pcs.baidu.com" || h == "baidupcs.com" || strings.HasSuffix(h, ".baidupcs.com")
}

func (r *Resolver) request(ctx context.Context, method, raw string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, unavailable
	}
	req.Header.Set("User-Agent", "pan.baidu.com")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, unavailable
	}
	return resp, nil
}
func decode(resp *http.Response, value any) error {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return unavailable
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 || json.Unmarshal(b, value) != nil {
		return unavailable
	}
	return nil
}

// Resolve is called only after the caller has checked the published artifact.
// Serializing OAuth renewal also prevents a burst of requests from rotating a
// refresh token multiple times. Provider failures briefly fall back to bytes.
func (r *Resolver) Resolve(ctx context.Context, hash string, size int64, fresh bool) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.artifacts[hash]
	if !ok || a.Size != size {
		return "", unavailable
	}
	if c, ok := r.cache[hash]; ok && !fresh && r.now().Before(c.Until) {
		return c.URL, nil
	}
	if r.now().Before(r.retryAfter) {
		return "", unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	link, err := r.resolve(ctx, a)
	if err != nil {
		r.retryAfter = r.now().Add(30 * time.Second)
		return "", unavailable
	}
	r.cache[hash] = cached{link, r.now().Add(10 * time.Minute)}
	return link, nil
}

func (r *Resolver) resolve(ctx context.Context, a Artifact) (string, error) {
	var data struct {
		Errno     int `json:"errno"`
		ErrorCode int `json:"error_code"`
		List      []struct {
			FSID  int64  `json:"fs_id"`
			Size  int64  `json:"size"`
			Path  string `json:"path"`
			Dlink string `json:"dlink"`
		} `json:"list"`
	}
	for attempt := 0; attempt < 2; attempt++ {
		data.Errno, data.ErrorCode, data.List = 0, 0, nil
		q := url.Values{"method": {"filemetas"}, "fsids": {"[" + strconv.FormatInt(a.FSID, 10) + "]"}, "dlink": {"1"}, "access_token": {r.credentials.AccessToken}}
		resp, err := r.request(ctx, "GET", "https://pan.baidu.com/rest/2.0/xpan/multimedia?"+q.Encode())
		if err != nil || decode(resp, &data) != nil {
			return "", unavailable
		}
		code := data.Errno
		if code == 0 {
			code = data.ErrorCode
		}
		if code == 0 {
			break
		}
		if attempt != 0 || (code != 110 && code != 111) || r.refresh(ctx) != nil {
			return "", unavailable
		}
	}
	if len(data.List) != 1 {
		return "", unavailable
	}
	f := data.List[0]
	if f.FSID != a.FSID || f.Size != a.Size || f.Path != a.Path || !provider(f.Dlink) {
		return "", unavailable
	}
	u, _ := url.Parse(f.Dlink)
	q := u.Query()
	q.Set("access_token", r.credentials.AccessToken)
	u.RawQuery = q.Encode()
	resp, err := r.request(ctx, "HEAD", u.String())
	if err != nil {
		return "", unavailable
	}
	defer resp.Body.Close()
	link := resp.Header.Get("Location")
	if (resp.StatusCode != 301 && resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 307 && resp.StatusCode != 308) || !provider(link) || strings.Contains(link, r.credentials.AccessToken) || strings.Contains(link, r.credentials.RefreshToken) {
		return "", unavailable
	}
	u, _ = url.Parse(link)
	for key := range u.Query() {
		if strings.EqualFold(key, "access_token") || strings.EqualFold(key, "refresh_token") {
			return "", unavailable
		}
	}
	return link, nil
}

func (r *Resolver) refresh(ctx context.Context) error {
	q := url.Values{"refresh_ui": {r.credentials.RefreshToken}, "server_use": {"true"}, "driver_txt": {"baiduyun_go"}}
	resp, err := r.request(ctx, "GET", "https://api.oplist.org/baiduyun/renewapi?"+q.Encode())
	if err != nil {
		return unavailable
	}
	var data struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if decode(resp, &data) != nil || data.Access == "" || data.Refresh == "" {
		return unavailable
	}
	next := credentials{data.Access, data.Refresh}
	raw, _ := json.Marshal(next)
	f, err := os.CreateTemp(filepath.Dir(r.cfg.CredentialsFile), ".baidu-credentials-")
	if err != nil {
		return unavailable
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return unavailable
	}
	if err = os.Rename(name, r.cfg.CredentialsFile); err != nil {
		return unavailable
	}
	r.credentials = next
	return nil
}

// A missing/unknown capability ALWAYS retains the legacy byte response.
func (r *Resolver) Redirect(w http.ResponseWriter, req *http.Request, hash string, size int64) bool {
	w.Header().Add("Vary", CapabilityHeader)
	w.Header().Add("Vary", "X-DNF90-Refresh-Link")
	if r == nil || req.Method != "GET" || req.Header.Get(CapabilityHeader) != Capability {
		return false
	}
	link, err := r.Resolve(req.Context(), hash, size, req.Header.Get("X-DNF90-Refresh-Link") == "1")
	if err != nil {
		return false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Location", link)
	w.WriteHeader(http.StatusTemporaryRedirect)
	return true
}
