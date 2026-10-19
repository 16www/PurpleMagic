package gmtool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"dnf90.local/gateway/internal/servicehttp"
)

func TestV1ExternalQueueRestartAfterCommittedResponseLoss(t *testing.T) {
	id, hash, token := strings.Repeat("1", 32), strings.Repeat("a", 64), strings.Repeat("t", 48)
	revision, pending, grants := uint64(1), 15, 0
	lost := true
	server := httptest.NewServer(servicehttp.Protect(token, hash, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/"+id && r.URL.Path != "/api/v1/tasks/"+id+"/run" {
			t.Error("unexpected route", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == "POST" {
			var in struct {
				Revision uint64
				Confirm  bool
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.Revision != revision || !in.Confirm {
				http.Error(w, "revision conflict", 409)
				return
			}
			n := min(pending, 10)
			pending -= n
			grants += n
			revision++
			if lost {
				lost = false
				http.Error(w, "committed response lost", 503)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": id, "revision": revision, "pending": pending, "sent": grants, "total": 15, "pvfChecksum": hash}})
	})))
	defer server.Close()
	peer, e := servicehttp.New(servicehttp.Config{URL: server.URL, Token: token, PVFSHA256: hash})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "queue.json")
	q, e := OpenExternalQueue(path, peer)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/api/v1/tasks/"+id+"/schedule", strings.NewReader(`{"revision":1,"confirm":true}`))
	w := httptest.NewRecorder()
	if !q.Handle(w, r) || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if grants != 0 {
		t.Fatal("confirmation handler executed before persistence")
	}
	if e = q.step(context.Background()); e == nil || grants != 10 {
		t.Fatal("lost response not exercised", e, grants)
	}
	restored, e := OpenExternalQueue(path, peer)
	if e != nil {
		t.Fatal(e)
	}
	if e = restored.step(context.Background()); e != nil {
		t.Fatal(e)
	}
	if grants != 15 || pending != 0 || restored.Status(id) != "completed" {
		t.Fatal("restart lost receipt progress", grants, restored.Status(id))
	}
	if e = restored.step(context.Background()); e != nil || grants != 15 {
		t.Fatal("duplicate grant", e, grants)
	}
	// An unreviewed revision cannot start another run.
	r = httptest.NewRequest("POST", "/api/v1/tasks/"+id+"/schedule", strings.NewReader(`{"revision":2,"confirm":true}`))
	w = httptest.NewRecorder()
	restored.Handle(w, r)
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestV1ExternalQueuePauseAndGenerationChange(t *testing.T) {
	hash, token, id := strings.Repeat("a", 64), strings.Repeat("t", 48), strings.Repeat("1", 32)
	server := httptest.NewServer(servicehttp.Protect(token, hash, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": id, "revision": 1, "pending": 10, "pvfChecksum": hash}})
	})))
	defer server.Close()
	peer, _ := servicehttp.New(servicehttp.Config{URL: server.URL, Token: token, PVFSHA256: hash})
	path := filepath.Join(t.TempDir(), "queue.json")
	q, e := OpenExternalQueue(path, peer)
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"schedule", "pause"} {
		r := httptest.NewRequest("POST", "/api/v1/tasks/"+id+"/"+action, strings.NewReader(`{"revision":1,"confirm":true}`))
		w := httptest.NewRecorder()
		q.Handle(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if q.Status(id) != "paused" {
		t.Fatal(q.Status(id))
	}
	restored, e := OpenExternalQueue(path, peer)
	if e != nil || restored.Status(id) != "paused" {
		t.Fatal(e)
	}
	q.state.Jobs[id] = queuedMail{ID: id, Revision: 1, Status: "active"}
	if e = q.save(); e != nil {
		t.Fatal(e)
	}
	nextPeer, _ := servicehttp.New(servicehttp.Config{URL: server.URL, Token: token, PVFSHA256: strings.Repeat("b", 64)})
	restored, e = OpenExternalQueue(path, nextPeer)
	if e != nil || restored.Status(id) != "conflict" {
		t.Fatal("old generation resumed", e)
	}
}
