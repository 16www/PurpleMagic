package launcherhub

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedContentBlobBoundary(t *testing.T) {
	h, _ := fixture(t)
	data := []byte("verified immutable compressed resource")
	hash := digest(data)
	dir := filepath.Join(h.cfg.DataDir, "content-blobs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hash), data, 0600); err != nil {
		t.Fatal(err)
	}
	path := "/blobs/" + hash
	if w := call(h.PublicHandler(), "GET", path, "", nil, "", ""); w.Code != 404 {
		t.Fatal("unpublished blob exposed")
	}
	h.state.Published.Content = &Content{Endpoint: h.profile.ApiURL, Files: []File{{Path: "DNF.exe", Sha256: strings.Repeat("c", 64), Length: 100, Downloadable: true, Compression: "gzip", BlobSha256: hash, BlobLength: int64(len(data))}}}
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Range", "bytes=3-9")
	w := httptest.NewRecorder()
	h.PublicHandler().ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != string(data[3:10]) || w.Header().Get("Content-Range") == "" {
		t.Fatal("range", w.Code, w.Body.String())
	}
	for _, target := range []string{path + "?token=x", path + ".key", "/blobs/../state.json", "/blobs/" + strings.Repeat("d", 64), "/blobs/%2e%2e/state.json"} {
		if w := call(h.PublicHandler(), "GET", target, "", nil, "", ""); w.Code != 404 {
			t.Fatalf("exposed %s", target)
		}
	}
	if w := call(h.PublicHandler(), "POST", path, "replace", nil, "", ""); w.Code != 404 {
		t.Fatal("public mutation accepted")
	}
	if w := call(h.PublicHandler(), "HEAD", path, "", nil, "", ""); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("head", w.Code)
	}
	h.state.Published.Content.Endpoint = "https://author.example.test"
	if w := call(h.PublicHandler(), "GET", path, "", nil, "", ""); w.Code != 404 {
		t.Fatal("foreign source exposed")
	}
	h.state.Published.Content.Endpoint = h.profile.ApiURL
	if err := os.WriteFile(filepath.Join(dir, hash), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := call(h.PublicHandler(), "GET", path, "", nil, "", ""); w.Code != 404 {
		t.Fatal("wrong size exposed")
	}
}
