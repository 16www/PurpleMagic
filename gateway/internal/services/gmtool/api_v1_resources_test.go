package gmtool

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestV1InventoryRawInstanceShape(t *testing.T) {
	s := &Server{items: &itemIndex{ready: true, entries: []itemEntry{{ID: 1, Kind: "equipment"}}, byID: map[uint32]int{1: 0}}}
	for _, size := range []int{0, 12, 118, 119, 120} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			raw := make([]byte, size)
			if size >= 119 {
				binary.LittleEndian.PutUint32(raw[6:10], 123456)
				raw[0x0a] = 12
				binary.LittleEndian.PutUint16(raw[0x0b:0x0d], 321)
				raw[0x0d], raw[0x13], raw[0x57] = 1, 2, 8
				binary.LittleEndian.PutUint16(raw[0x14:0x16], 456)
			}
			row := repository.InventorySlot{Slot: 9, Stack: repository.ItemStack{ItemID: 1, Count: 1, RawEntry: raw}}
			out := s.v1InventoryRow("equipment", row, true)
			attrs := out["attributes"].(map[string]any)
			for key, want := range map[string]any{"qualitySeed": "123456", "reinforce": 12, "amplifyType": 2, "amplifyValue": 456, "forging": 8, "durability": "321", "seal": "1"} {
				if size != 119 {
					want = nil
				}
				if attrs[key] != want {
					t.Fatalf("%s = %v, want %v", key, attrs[key], want)
				}
			}
			if len(row.Stack.RawEntry) != size || attrs["enchant_id"] != nil || attrs["emblem_0"] != nil {
				t.Fatal("projection changed source or invented fields", out)
			}
			row.Stack.Extra = map[string]string{"durability": "0", "seal_flag": "0"}
			updated := s.v1InventoryRow("equipment", row, true)
			attrs = updated["attributes"].(map[string]any)
			if attrs["durability"] != "0" || attrs["seal"] != "0" || updated["revision"] == out["revision"] {
				t.Fatal("persisted zero/revision lost", updated)
			}
			if _, present := s.v1InventoryRow("equipment", row, false)["attributes"]; present {
				t.Fatal("list includes instance attributes")
			}
		})
	}
}

func TestV1InventoryUnavailableIndex(t *testing.T) {
	for _, ix := range []*itemIndex{nil, {}, {ready: true, buildEr: fmt.Errorf("broken index")}} {
		s := &Server{items: ix}
		w := httptest.NewRecorder()
		if s.v1RequireItemIndex(w, httptest.NewRequest("GET", "/", nil)) || w.Code != 503 || !strings.Contains(w.Body.String(), "item_index_unavailable") {
			t.Fatal("unavailable index treated as missing item", w.Code, w.Body.String())
		}
	}
}

func testV1ResourcesWithGroup(t *testing.T, s *Server, g repository.Group, counted *v1CountingDB) {
	t.Helper()
	ctx := context.Background()
	token, err := s.startSession()
	if err != nil {
		t.Fatal(err)
	}
	source := questFixture("测试主线")
	s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
	s.items.cachePath = filepath.Join(t.TempDir(), "items.json")
	if _, err = BuildQuestIndex(ctx, source, s.items.cachePath); err != nil {
		t.Fatal(err)
	}
	inv := repository.InventoryRecord{CharacterID: "0010", Slots: map[string]repository.ItemStack{}, Warehouse: map[string]repository.ItemStack{"2:0": {ItemID: 1, Count: 1}}}
	for i := 9; i <= 35; i++ {
		inv.Slots[fmt.Sprintf("0:%d", i)] = repository.ItemStack{ItemID: 1, Count: 1, Extra: map[string]string{"reinforce": "0", "quality_seed": "0", "durability": "52"}}
	}
	inv.Slots["0:354"] = repository.ItemStack{ItemID: 3033, Count: 999}
	inv.Slots["12:0"] = repository.ItemStack{ItemID: 1, Count: 999}
	inv.Slots["0:65"] = repository.ItemStack{ItemID: 999999, Count: 9007199254740993}
	if err = g.Inventory.Save(ctx, inv); err != nil {
		t.Fatal(err)
	}
	if err = g.AccountInventory.Save(ctx, repository.AccountInventoryRecord{AccountID: "dnf:1", Slots: map[string]repository.ItemStack{"0:354": {ItemID: 3033, Count: 17}, "12:0": {ItemID: 1, Count: 2}}}); err != nil {
		t.Fatal(err)
	}
	if err = g.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "0010", Entries: map[string]repository.EquipmentEntry{"10": {SlotIndex: 10, ItemID: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err = g.Quest.Save(ctx, repository.QuestRecord{CharacterID: "0010", States: map[int64]repository.QuestState{1: {Status: "active", ProgressValue: 2}, 2: {Status: "completed"}, 999: {Status: "accepted"}}, Progress: map[int64]repository.QuestState{1: {Status: "completed", ProgressValue: 99}}}); err != nil {
		t.Fatal(err)
	}
	mail, _, err := g.Mailbox.Load(ctx, "0010")
	if err != nil {
		t.Fatal(err)
	}
	mail.CharacterID = "0010"
	if mail.Mails == nil {
		mail.Mails = map[string]repository.MailRecord{}
	}
	mail.Mails["resource-mail"] = repository.MailRecord{MailID: "resource-mail", Title: "资源查询邮件", Body: "正文 <script>", Gold: 9007199254740993, Attachments: []repository.MailAttachment{{ItemID: 1, Count: 1}}}
	if err = g.Mailbox.Save(ctx, mail); err != nil {
		t.Fatal(err)
	}
	get := func(path string, want int) map[string]any {
		t.Helper()
		status, out := v1TestCall(t, s, token, "GET", path)
		if status != want {
			t.Fatalf("%s got %d want %d: %v", path, status, want, out)
		}
		return out
	}
	base := "/api/v1/characters/0010/"
	counted.queries = nil
	out := get(base+"inventory?container=equipment&limit=7", 200)
	data := out["data"].(map[string]any)
	rows := data["items"].([]any)
	if len(rows) != 7 || rows[0].(map[string]any)["slot"] != float64(9) {
		t.Fatal(rows)
	}
	for _, query := range counted.queries {
		if strings.Contains(query, "_extra") || strings.Contains(query, "raw_entry") {
			t.Fatal("list loaded instance details", query)
		}
	}
	cursor := out["page"].(map[string]any)["nextCursor"].(string)
	get(base+"inventory?container=consumables&limit=7&cursor="+url.QueryEscape(cursor), 400)
	get("/api/v1/characters/0011/inventory?container=equipment&limit=7&cursor="+url.QueryEscape(cursor), 400)
	seen := map[float64]bool{}
	path := base + "inventory?container=equipment&limit=7"
	for {
		out = get(path, 200)
		for _, v := range out["data"].(map[string]any)["items"].([]any) {
			slot := v.(map[string]any)["slot"].(float64)
			if seen[slot] {
				t.Fatal("duplicate slot", slot)
			}
			seen[slot] = true
		}
		next, _ := out["page"].(map[string]any)["nextCursor"].(string)
		if next == "" {
			break
		}
		path = base + "inventory?container=equipment&limit=7&cursor=" + url.QueryEscape(next)
	}
	if len(seen) != 27 {
		t.Fatal(seen)
	}
	for _, check := range []struct{ container, count string }{{"shared", "17"}, {"account-cargo", "2"}, {"warehouse", "1"}, {"equipped", "1"}, {"consumables", "9007199254740993"}} {
		out = get(base+"inventory?container="+check.container, 200)
		r := out["data"].(map[string]any)["items"].([]any)
		want := 1
		if check.container == "shared" {
			want = 12
		}
		if len(r) != want || r[0].(map[string]any)["count"] != check.count {
			t.Fatal(check, r)
		}
	}
	out = get(base+"inventory/equipment/9", 200)
	attrs := out["data"].(map[string]any)["attributes"].(map[string]any)
	if attrs["reinforce"] != float64(0) || attrs["qualitySeed"] != "0" || attrs["forging"] != nil {
		t.Fatal("unknown/zero conflated", attrs)
	}
	get(base+"inventory/equipment/8", 400)
	get(base+"inventory/equipment/64", 404)
	get(base+"inventory?container=unknown", 400)
	get("/api/v1/characters/9999/inventory?container=equipment", 404)
	out = get(base+"mailbox/resource-mail", 200)
	if out["data"].(map[string]any)["gold"] != "9007199254740993" {
		t.Fatal(out)
	}
	get("/api/v1/characters/0011/mailbox/resource-mail", 404)
	out = get(base+"quests?state=active&limit=1", 200)
	quests := out["data"].(map[string]any)["quests"].([]any)
	if len(quests) != 1 || quests[0].(map[string]any)["progress"] != "2" {
		t.Fatal("quest precedence lost", quests)
	}
	qcursor := out["page"].(map[string]any)["nextCursor"].(string)
	out = get(base+"quests?state=active&limit=1&cursor="+url.QueryEscape(qcursor), 200)
	if out["data"].(map[string]any)["quests"].([]any)[0].(map[string]any)["definitionKnown"] != false {
		t.Fatal("unknown quest replaced", out)
	}
	get("/api/v1/quests/1", 200)
	get("/api/v1/quests?q=测试", 200)
	get(base+"quests?state=bad", 400)
	changed := questFixture("更新后的主线")
	s.deps.PVF = func() (dnfpvf.Source, error) { return changed, nil }
	get(base+"inventory?container=equipment&limit=7&cursor="+url.QueryEscape(cursor), 400)
	get(base+"quests?state=active", 503)
	if _, err = BuildQuestIndex(ctx, changed, s.items.cachePath); err != nil {
		t.Fatal(err)
	}
	get(base+"quests?state=active&limit=1&cursor="+url.QueryEscape(qcursor), 400)
	out = get("/api/v1/quests/1", 200)
	if out["data"].(map[string]any)["name"] != "更新后的主线" {
		t.Fatal("stale definition", out)
	}
	s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	reader := g.Character.(repository.CharacterResourceReader)
	if _, err = reader.InventoryPage(canceled, repository.CharacterSummary{CharacterID: "0010", AccountID: "dnf:1"}, "equipment", -1, 20, false); err == nil {
		t.Fatal("SQL cancellation ignored")
	}
}
