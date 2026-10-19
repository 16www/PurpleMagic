package gmtool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
	"dnf90.local/gateway/internal/servicehttp"
)

func externalTestServices(t *testing.T, s *Server) (*Server, *servicehttp.Client, *atomic.Int64) {
	t.Helper()
	source, e := s.source()
	if e != nil {
		t.Fatal(e)
	}
	cat := &Server{cfg: s.cfg, deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}, pvf: newPVFCache(), sessions: map[string]time.Time{}, failures: map[string][]time.Time{}, grants: map[string]grantRecord{}}
	cat.items = newItemIndex(s.items.cachePath, cat, func(string, ...any) {})
	cat.items.setEntriesLocked(append([]itemEntry(nil), s.items.snapshot()...))
	cat.questIndex = s.questIndex
	if cat.questIndex == nil {
		cat.questIndex = &questDiskIndex{Checksum: itemIndexSourceChecksum(source), ManifestHash: strings.Repeat("a", 64)}
	}
	catalog := httptest.NewServer(cat.CatalogHandler(strings.Repeat("c", 48)))
	t.Cleanup(catalog.Close)
	remote, e := NewRemoteCatalog(servicehttp.Config{URL: catalog.URL, Token: strings.Repeat("c", 48), PVFSHA256: itemIndexSourceChecksum(source)})
	if e != nil {
		t.Fatal(e)
	}
	s.deps.Catalog = remote
	s.deps.PVF = func() (dnfpvf.Source, error) { return remote, nil }
	s.pvf = newPVFCache()
	s.questIndex = nil
	s.items = newItemIndex(filepath.Join(t.TempDir(), "absent.json"), s, func(string, ...any) {})
	if e = s.items.loadCache(); e != nil {
		t.Fatal(e)
	}
	calls := new(atomic.Int64)
	handler := s.GatewayHandler(strings.Repeat("g", 48), remote.peer.Hash())
	game := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); handler.ServeHTTP(w, r) }))
	t.Cleanup(game.Close)
	peer, e := servicehttp.New(servicehttp.Config{URL: game.URL, Token: strings.Repeat("g", 48), PVFSHA256: remote.peer.Hash()})
	if e != nil {
		t.Fatal(e)
	}
	if s.v1PVFChecksum() != cat.v1PVFChecksum() {
		t.Fatal("remote source lost PVF identity")
	}
	return cat, peer, calls
}

func TestV1ExternalCatalogParityAndIsolation(t *testing.T) {
	ctx := context.Background()
	source := levelTestSource()
	for k, v := range questFixture("外部任务").readonlyTestSource {
		source.readonlyTestSource[k] = v
	}
	for k, v := range itemCacheTestDocuments("外部物品", false) {
		source.readonlyTestSource[k] = v
	}
	source = newItemCacheTestSource(source.readonlyTestSource)
	summary := filepath.Join(t.TempDir(), "items.json")
	ix := itemCacheTestIndex(summary, source)
	if e := ix.build(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := BuildQuestIndex(ctx, source, summary); e != nil {
		t.Fatal(e)
	}
	s := ix.srv
	s.cfg.Password = strings.Repeat("p", 48)
	s.items = ix
	s.pvf = newPVFCache()
	s.questIndex, _ = s.questReadIndex(ctx)
	c := repository.CharacterRecord{Job: "0", Stats: map[string]int64{"grow_type": 1}}
	before, e := s.levelDefinition(ctx, c, 50)
	if e != nil {
		t.Fatal(e)
	}
	cat, peer, calls := externalTestServices(t, s)
	after, e := s.levelDefinition(ctx, c, 50)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("level changed over transport", before, after, e)
	}
	qi, e := s.questReadIndex(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(qi.Rows) != 0 {
		t.Fatal("gateway retained full quest index")
	}
	d, e := qi.definition(ctx, 1)
	if e != nil || d.Name != "外部任务" {
		t.Fatal(d, e)
	}
	if len(s.items.entries) != 0 || len(s.items.byID) != 0 {
		t.Fatal("gateway retained full item index")
	}
	if _, e = s.deps.Catalog.ReadText("n_quest/quest.lst"); e == nil {
		t.Fatal("raw archive route available")
	}
	token, e := cat.startSession()
	if e != nil {
		t.Fatal(e)
	}
	handler := cat.ExternalHandler("", "", peer)
	for _, path := range []string{"/api/v1/items", "/api/v1/quests", "/api/v1/quests/1"} {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("static search reached game", calls.Load())
	}
	r := httptest.NewRequest("POST", "/api/v1/mail/preview", strings.NewReader(`{}`))
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 || calls.Load() != 0 {
		t.Fatal("missing CSRF crossed gateway")
	}
	// Direct private routes reject browser credentials.
	r = httptest.NewRequest("GET", "/api/v1/accounts", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	w = httptest.NewRecorder()
	s.GatewayHandler(strings.Repeat("g", 48), peer.Hash()).ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}

}

func TestV1ExternalMailProjectionAndFailure(t *testing.T) {
	source := newItemCacheTestSource(readonlyTestSource{itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText, itemcfgSeparateUpgradePVFPath: itemcfgTestSeparateUpgradeText})
	s := &Server{cfg: Config{Password: strings.Repeat("p", 48), Logger: func(string, ...any) {}}, deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}, pvf: newPVFCache()}
	s.items = newItemIndex("", s, s.cfg.Logger)
	s.items.setEntriesLocked(itemcfgTestEntries())
	draft := testbenefits.MailDraft{Title: "transport", ItemID: "1", Count: 1, PVFChecksum: source.checksum}
	draft.ItemID = jsonNumber(itemcfgTestWeaponID)
	at := time.Date(2026, 10, 18, 1, 2, 3, 0, time.UTC)
	expected, e := s.v1ProjectMailDraft(draft, at)
	if e != nil {
		t.Fatal(e)
	}
	_, _, _ = externalTestServices(t, s)
	actual, e := s.v1ProjectMailDraft(draft, at)
	if e != nil || !reflect.DeepEqual(expected, actual) {
		t.Fatal(expected, actual, e)
	}
	draft.PVFChecksum = strings.Repeat("f", 64)
	if _, e = s.v1ProjectMailDraft(draft, at); e == nil {
		t.Fatal("stale mail generation accepted")
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	s.deps.Catalog, _ = NewRemoteCatalog(servicehttp.Config{URL: dead.URL, Token: strings.Repeat("x", 48), PVFSHA256: source.checksum})
	draft.PVFChecksum = source.checksum
	if _, e = s.v1ProjectMailDraft(draft, at); e == nil {
		t.Fatal("network failure produced a grant")
	}
}
func jsonNumber(v uint32) string { raw, _ := json.Marshal(v); return string(raw) }

// Used by the disposable database/browser fixture, never production data.
func externalFeedbackEnabled() bool { return os.Getenv("DNF90GM_EXTERNAL_FEEDBACK") == "1" }
