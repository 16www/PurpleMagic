package gmtool

import (
	"runtime"
	"testing"
)

func TestItemSearchRetainsOnlyRequestedPage(t *testing.T) {
	entries := make([]itemEntry, 150000)
	for i := range entries {
		entries[i] = itemEntry{ID: uint32(i + 1), Name: "fixture", Rarity: 1}
	}
	ix := &itemIndex{ready: true, entries: entries}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	page, total := ix.search(itemSearchQuery{Offset: 1000, Limit: 20, Rarity: -1})
	runtime.ReadMemStats(&after)
	if total != len(entries) || len(page) != 20 || page[0].ID != 1001 || page[19].ID != 1020 {
		t.Fatal("pagination changed")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("one small page allocated %d bytes", allocated)
	}
	if empty, total := ix.search(itemSearchQuery{Offset: 200000, Limit: 20, Rarity: -1}); len(empty) != 0 || total != len(entries) {
		t.Fatal("out-of-range count changed")
	}
	if page, _ := ix.search(itemSearchQuery{Offset: -2, Limit: 1, Rarity: -1}); len(page) != 1 || page[0].ID != 1 {
		t.Fatal("negative offset changed")
	}
}
