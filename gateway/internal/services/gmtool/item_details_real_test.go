package gmtool

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRealItemDetailSurvey(t *testing.T) {
	out := os.Getenv("GM_ITEM_DETAIL_AUDIT")
	if out == "" {
		t.Skip("set GM_ITEM_DETAIL_AUDIT for local PVF detail evidence")
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	source := loadRealPVFSource(t)
	ix := itemCacheTestIndex(filepath.Join(out, "items.json"), source)
	ix.start(context.Background())
	if !ix.isReady() {
		t.Fatal(ix.failure())
	}
	picked := make([]itemEntry, 0, 12)
	names := map[string]bool{}
	for _, e := range ix.snapshot() {
		wanted := e.ID == 490002780 || e.ID == 490007493 || e.ID == 490009735
		for _, q := range []string{"荒古遗尘巨剑", "幽魂魅影长袍", "魔剑-阿波菲斯", "圣耀救赎太刀", "无尽贪食之力", "清泉流响"} {
			if e.Name == q && !names[q] {
				wanted = true
				names[q] = true
			}
		}
		if !wanted {
			continue
		}
		picked = append(picked, e)
		text, err := source.ReadText(e.documentPath())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(out, strings.ReplaceAll(e.Name, "/", "_")+".txt"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.MarshalIndent(picked, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "sample-items.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("indexed %d items, sampled %d", len(ix.snapshot()), len(picked))
}

func TestRealItemDetailBrowser(t *testing.T) {
	script, out := os.Getenv("GM_ITEM_BROWSER_SCRIPT"), os.Getenv("GM_ITEM_DETAIL_AUDIT")
	if script == "" || out == "" {
		t.Skip("set real-PVF browser script and output directory")
	}
	source := loadRealPVFSource(t)
	ix := itemCacheTestIndex(filepath.Join(out, "items.json"), source)
	s := ix.srv
	s.pvf = newPVFCache()
	s.cfg.Password = "isolated-item-browser-password"
	s.sessions = map[string]time.Time{}
	s.failures = map[string][]time.Time{}
	ix.start(context.Background())
	if !ix.isReady() {
		t.Fatal(ix.failure())
	}
	var err error
	s.www, err = newStaticHandler("wwwroot")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s.routes())
	defer server.Close()
	command := exec.Command("python", script, "--url", server.URL, "--output", filepath.Join(out, "browser"), "--preview-fixture", filepath.Join(out, "preview-items.json"))
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("item browser failed: %v\n%s", err, result)
	} else {
		t.Log(string(result))
	}
}

func TestRealItemDetailsCompiled(t *testing.T) {
	out := os.Getenv("GM_ITEM_DETAIL_AUDIT")
	if out == "" {
		t.Skip("set GM_ITEM_DETAIL_AUDIT for the compiled real-PVF index audit")
	}
	source := loadRealPVFSource(t)
	summary := filepath.Join(out, "items.json")
	started := time.Now()
	report, err := BuildItemDetails(context.Background(), source, summary, func(message string, args ...any) { t.Log(message, args) })
	if err != nil {
		t.Fatal(err)
	}
	buildTime := time.Since(started)
	ix := itemCacheTestIndex(summary, source)
	ix.srv.pvf = newPVFCache()
	ix.start(context.Background())
	if !ix.isReady() {
		t.Fatal(ix.failure())
	}
	samples := []uint32{100050050, 100312103, 100322294, 101010864, 101030517, 490002780, 490007493, 490009735}
	previews := []*itemProjection{}
	for _, id := range samples {
		entry, ok := ix.lookup(id)
		if !ok {
			t.Fatal("missing sample", id)
		}
		direct, err := ix.srv.projectItem(context.Background(), source, entry)
		if err != nil {
			t.Fatal(id, err)
		}
		indexed, err := readItemDetailIndex(context.Background(), report.Path, report.PVFChecksum, id)
		if err != nil {
			t.Fatal(id, err)
		}
		a, _ := json.Marshal(direct)
		b, _ := json.Marshal(indexed)
		var decodedA, decodedB any
		_ = json.Unmarshal(a, &decodedA)
		_ = json.Unmarshal(b, &decodedB)
		if !reflect.DeepEqual(decodedA, decodedB) {
			t.Fatal("compiled detail mismatch", id)
		}
		previews = append(previews, indexed)
	}
	for i := 0; i < len(ix.entries); i += 1000 {
		id := ix.entries[i].ID
		if _, err := readItemDetailIndex(context.Background(), report.Path, report.PVFChecksum, id); err != nil {
			t.Fatal("distributed index lookup", id, err)
		}
	}
	raw, _ := json.MarshalIndent(previews, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "preview-items.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	if memory.HeapAlloc > 270<<20 {
		t.Fatal("retained heap exceeds detail-index budget", memory.HeapAlloc)
	}
	startRead := time.Now()
	for range 100 {
		for _, id := range samples {
			if _, err = readItemDetailIndex(context.Background(), report.Path, report.PVFChecksum, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	readTime := time.Since(startRead)
	reuseStart := time.Now()
	repeat, err := BuildItemDetails(context.Background(), source, summary, nil)
	if err != nil || !repeat.Reused {
		t.Fatal("unchanged PVF rebuild", repeat, err)
	}
	evidence := map[string]any{"build": report, "buildMilliseconds": buildTime.Milliseconds(), "retainedHeapBytes": memory.HeapAlloc, "read800Milliseconds": readTime.Milliseconds(), "reuseMilliseconds": time.Since(reuseStart).Milliseconds(), "sampleItems": samples, "databaseConnected": false, "httpStarted": false}
	raw, _ = json.MarshalIndent(evidence, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "real-index-result.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
	runtime.KeepAlive(ix)
}
