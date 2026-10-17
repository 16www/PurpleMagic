package gmtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	platformpvf "dnf90.local/gateway/internal/platform/pvf"
)

// This opt-in audit never opens a database or listener. It retains the actual
// GM item index, so its heap figure includes more than the stage-one read probe.
func TestRealDiskGMIndexMemory(t *testing.T) {
	path, idx, digest := os.Getenv("GM_DISK_PVF"), os.Getenv("GM_DISK_INDEX"), os.Getenv("GM_DISK_SHA256")
	if path == "" || idx == "" || digest == "" {
		t.Skip("set isolated GM_DISK_PVF, GM_DISK_INDEX, GM_DISK_SHA256")
	}
	d, err := platformpvf.OpenDisk(path, idx, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var opened, loaded runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&opened)
	itemPath := filepath.Join(t.TempDir(), "items.json")
	ix := itemCacheTestIndex(itemPath, d)
	started := time.Now()
	if err = ix.build(context.Background()); err != nil {
		t.Fatal(err)
	}
	builtIn := time.Since(started)
	runtime.GC()
	runtime.ReadMemStats(&loaded)
	linuxMemory := map[string]string{}
	if status, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			for _, key := range []string{"VmRSS:", "VmHWM:", "VmSwap:"} {
				if value, ok := strings.CutPrefix(line, key); ok {
					linuxMemory[strings.TrimSuffix(key, ":")] = strings.TrimSpace(value)
				}
			}
		}
		for _, key := range []string{"memory.current", "memory.peak", "memory.swap.current"} {
			if value, err := os.ReadFile("/sys/fs/cgroup/" + key); err == nil {
				linuxMemory[key] = strings.TrimSpace(string(value))
			}
		}
	}
	count := len(ix.snapshot())
	if count == 0 {
		t.Fatal("no items")
	}
	if digest == "b86dd8fa5bcde40a445d9e494a9e1a89d0fc15ddd15d61041e4b97f78973d00f" && count != 149574 {
		t.Fatalf("item count %d differs from fixed asset baseline", count)
	}
	if loaded.HeapAlloc > 250<<20 {
		t.Fatalf("retained GM heap exceeds 250 MiB: %d", loaded.HeapAlloc)
	}
	// Cache reload must preserve a complete, sorted item projection.
	reloaded := itemCacheTestIndex(itemPath, d)
	if err = reloaded.loadCache(); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(ix.snapshot())
	after, _ := json.Marshal(reloaded.snapshot())
	if string(before) != string(after) {
		t.Fatal("disk item cache changes projection")
	}
	receipt := map[string]any{"sourceSHA256": digest, "items": count, "openHeapBytes": opened.HeapAlloc, "indexedHeapBytes": loaded.HeapAlloc, "buildMilliseconds": builtIn.Milliseconds(), "auditMilliseconds": time.Since(started).Milliseconds(), "pvf": d.Snapshot(), "cache": d.CacheStats(), "databaseConnected": false, "httpStarted": false, "linuxMemoryAfterIndex": linuxMemory}
	raw, _ := json.MarshalIndent(receipt, "", "  ")
	t.Log(string(raw))
	if out := os.Getenv("GM_DISK_RECEIPT"); out != "" {
		if err = os.WriteFile(out, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runtime.KeepAlive(ix)
}
