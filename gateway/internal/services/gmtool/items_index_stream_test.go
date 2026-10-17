package gmtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestItemSummaryStreamingRejectsAmbiguity(t *testing.T) {
	source := newItemCacheTestSource(itemCacheTestDocuments("测试物品", false))
	file := filepath.Join(t.TempDir(), "items.json")
	payload := itemIndexFile{Version: itemIndexVersion, PVFChecksum: source.checksum, Entries: []itemEntry{{ID: 1, Name: "测试物品", Kind: "stackable"}}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{string(raw) + "{}", `{"version":6,"version":6,"entries":[]}`, `{"version":6,"entries":null}`, string(raw[:len(raw)-1]), `{"other":true}`} {
		if err = os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		ix := itemCacheTestIndex(file, source)
		if err = ix.loadCache(); err == nil || ix.isReady() {
			t.Fatal("invalid stream accepted", body)
		}
	}
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ix := itemCacheTestIndex(file, source)
	if err = ix.loadCache(); err != nil {
		t.Fatal(err)
	}
	if got, ok := ix.lookup(1); !ok || got.Name != "测试物品" || source.reads != 0 {
		t.Fatal(got, ok, source.reads)
	}
}
