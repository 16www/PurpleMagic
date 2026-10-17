package gmtool

import (
	"context"
	"encoding/json"
	dnfpvf "github.com/Cec1c/DNF90GM/internal/modules/dnf/pvf"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func detailFixture() *itemCacheTestSource {
	return newItemCacheTestSource(readonlyTestSource{
		itemListStackable: "", itemListEquipment: "42 `test.equ`\n",
		"equipment/test.equ": "[name]\n`test sword`\n[equipment type]\n`[weapon]` 1\n[usable job]\n`[swordman]` `[knight]`\n[/usable job]\n[basic explain]\n`增加10%%\n第二行`\n[physical attack]\n10\n[if]\n[time]\n100 1 0\n[/if]\n[then]\n[physical attack]\n999\n[stat change]\n`first` 1\n[stat change]\n`second` 2\n[/then]\n",
	})
}

func TestProjectedSetBonusKeepsConditionalStatsSeparate(t *testing.T) {
	doc, err := dnfpvf.Parse("set.equ", "[piece set ability]\n3\n[parameter basic explain]\n`三件效果`\n[physical attack]\n10\n[if]\n[time]\n100\n[/if]\n[then]\n[physical attack]\n999\n[/then]\n[/piece set ability]\n")
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := itemSections(doc)
	bonuses := projectedSetBonuses(blocks)
	if len(bonuses) != 1 || bonuses[0].RequiredPieces != 3 || len(bonuses[0].Attributes) != 1 || bonuses[0].Attributes[0].Value != 10 {
		t.Fatal(bonuses)
	}
}

func TestItemProjectionPreservesScopeAndRepeatedSections(t *testing.T) {
	source := detailFixture()
	ix := itemCacheTestIndex(filepath.Join(t.TempDir(), "items.json"), source)
	ix.srv.pvf = newPVFCache()
	ix.start(context.Background())
	entry, _ := ix.lookup(42)
	if !strings.Contains(entry.UsableJob, "[knight]") {
		t.Fatal("summary lost additional usable jobs", entry)
	}
	result, err := ix.srv.projectItem(context.Background(), source, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Texts) != 1 || result.Texts[0].Text != "增加10%\n第二行" {
		t.Fatal(result.Texts)
	}
	if len(result.Attributes) != 1 || result.Attributes[0].Name != "strength" || result.Attributes[0].Value != 11 {
		t.Fatal("conditional stat leaked into base attributes", result.Attributes)
	}
	found := false
	for _, node := range result.Sections {
		if node.Name == "then" {
			found = true
			if len(node.Children) != 3 || node.Children[1].Values[0] != "first  1" || node.Children[2].Values[0] != "second  2" {
				t.Fatal(node)
			}
		}
	}
	if !found {
		t.Fatal("missing condition block")
	}
}

func TestItemDetailIndexReuseReplacementCorruptionAndCancellation(t *testing.T) {
	source := detailFixture()
	summary := filepath.Join(t.TempDir(), "items.json")
	first, err := BuildItemDetails(context.Background(), source, summary, nil)
	if err != nil || first.Items != 1 || first.Failed != 0 {
		t.Fatal(first, err)
	}
	before := source.reads
	for range 3 {
		value, err := readItemDetailIndex(context.Background(), first.Path, source.checksum, 42)
		if err != nil || value.Item["name"] != "test sword" {
			t.Fatal(value, err)
		}
	}
	repeated, err := BuildItemDetails(context.Background(), source, summary, nil)
	if err != nil || !repeated.Reused || source.reads != before {
		t.Fatal("unchanged archive was reparsed", repeated, err, source.reads, before)
	}
	fresh := detailFixture()
	if _, err = readItemDetailIndex(context.Background(), first.Path, fresh.checksum, 42); err != nil || fresh.reads != 0 {
		t.Fatal("restart re-read PVF", err)
	}
	fresh.readonlyTestSource["equipment/test.equ"] = strings.ReplaceAll(fresh.readonlyTestSource["equipment/test.equ"], "test sword", "new sword")
	fresh = newItemCacheTestSource(fresh.readonlyTestSource)
	if _, err = readItemDetailIndex(context.Background(), first.Path, fresh.checksum, 42); err == nil {
		t.Fatal("stale archive accepted")
	}
	next, err := BuildItemDetails(context.Background(), fresh, summary, nil)
	if err != nil || next.Path == first.Path {
		t.Fatal(next, err)
	}
	f, err := os.OpenFile(first.Path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte("!"), detailHeaderSize+3)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readItemDetailIndex(context.Background(), first.Path, source.checksum, 42); err == nil {
		t.Fatal("corrupted payload accepted")
	}
	rebuilt, err := BuildItemDetails(context.Background(), source, summary, nil)
	if err != nil || rebuilt.Reused {
		t.Fatal("corrupt index not rebuilt", rebuilt, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = BuildItemDetails(ctx, source, filepath.Join(t.TempDir(), "new.json"), nil); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err = readItemDetailIndex(ctx, first.Path, source.checksum, 42); err == nil {
		t.Fatal("read cancel ignored")
	}
	if _, err = readItemDetailIndex(context.Background(), first.Path, source.checksum, 43); !os.IsNotExist(err) {
		t.Fatal("unknown item", err)
	}
}

func TestV1ItemDetailContract(t *testing.T) {
	source := detailFixture()
	ix := itemCacheTestIndex(filepath.Join(t.TempDir(), "items.json"), source)
	s := ix.srv
	s.pvf = newPVFCache()
	s.sessions = map[string]time.Time{}
	s.cfg.Password = "fixture"
	ix.start(context.Background())
	token, err := s.startSession()
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := v1TestCall(t, s, "", "GET", "/api/v1/items/42"); status != 401 {
		t.Fatal(status)
	}
	for _, path := range []string{"/api/v1/items/0", "/api/v1/items/-1", "/api/v1/items/042", "/api/v1/items/42?x=1"} {
		if status, _ := v1TestCall(t, s, token, "GET", path); status != 400 {
			t.Fatal(path, status)
		}
	}
	if status, _ := v1TestCall(t, s, token, "GET", "/api/v1/items/43"); status != 404 {
		t.Fatal(status)
	}
	status, result := v1TestCall(t, s, token, "GET", "/api/v1/items/42")
	if status != 200 {
		t.Fatal(status, result)
	}
	data := result["data"].(map[string]any)
	if data["item"].(map[string]any)["itemId"] != "42" {
		t.Fatal(data)
	}
	first, _ := json.Marshal(data)
	if _, err = BuildItemDetails(context.Background(), source, ix.cachePath, nil); err != nil {
		t.Fatal(err)
	}
	before := source.reads
	status, result = v1TestCall(t, s, token, "GET", "/api/v1/items/42")
	second, _ := json.Marshal(result["data"])
	if status != 200 || string(first) != string(second) || source.reads != before {
		t.Fatal("indexed API changed projection or read PVF", status, result)
	}
}
