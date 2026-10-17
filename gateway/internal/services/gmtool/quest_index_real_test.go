package gmtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	platformpvf "dnf90.local/gateway/internal/platform/pvf"
)

func TestRealQuestIndexCompiled(t *testing.T) {
	out := os.Getenv("GM_QUEST_INDEX_AUDIT")
	if out == "" {
		t.Skip("set GM_QUEST_INDEX_AUDIT, DNFBRIDGE_REAL_PVF_SMOKE and GM_DISK_SHA256 for an isolated offline audit")
	}
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(out, "pvf-directory.bin")
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		if err = platformpvf.BuildDiskIndex(context.Background(), os.Getenv("DNFBRIDGE_REAL_PVF_SMOKE"), os.Getenv("GM_DISK_SHA256"), directory); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GM_DISK_INDEX", directory)
	source := loadRealPVFSource(t)
	start := time.Now()
	summary := filepath.Join(out, "items.json")
	report, err := BuildQuestIndex(context.Background(), source, summary)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	ix, err := readQuestIndex(context.Background(), report.Path, report.PVFChecksum)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range ix.Rows {
		if _, err = ix.definition(context.Background(), row.ID); err != nil {
			t.Fatal(row.ID, err)
		}
	}
	warm, err := BuildQuestIndex(context.Background(), source, summary)
	if err != nil || !warm.Reused {
		t.Fatal("warm build failed", warm, err)
	}
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	receipt := map[string]any{"build": report, "buildMilliseconds": elapsed.Milliseconds(), "verifiedRecords": len(ix.Rows), "warmReused": warm.Reused, "retainedHeapBytes": mem.HeapAlloc, "databaseConnected": false, "httpStarted": false}
	raw, _ := json.MarshalIndent(receipt, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "quest-receipt.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
}
