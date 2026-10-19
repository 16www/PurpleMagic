package gmtool

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestV1FeedbackRound2MySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			g, ctx := open(), context.Background()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "实例修正", Job: "0", Level: 2, Stats: map[string]int64{"bonus_sp": 40, "bonus_tp": 3}}))
			source := newItemCacheTestSource(readonlyTestSource{})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			entries := itemcfgTestEntries()
			replacement := entries[0]
			replacement.ID = 8001
			replacement.Name = "替换武器"
			s.items.setEntriesLocked(append(entries, replacement))
			raw := make([]byte, 119)
			binary.LittleEndian.PutUint32(raw[2:6], entries[0].ID)
			raw[0x0a] = 31
			raw[100] = 0xad
			stack := repository.ItemStack{ItemID: int64(entries[0].ID), Count: 1, Bind: true, RawEntry: raw, Extra: map[string]string{"serial": "keep-instance"}}
			check(g.Inventory.Save(ctx, repository.InventoryRecord{CharacterID: "10", Slots: map[string]repository.ItemStack{"0:9": stack}}))
			check(g.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "10", Entries: map[string]repository.EquipmentEntry{"worn": {SlotIndex: 0, ItemID: stack.ItemID, Bind: true, RawEntry: raw, Extra: stack.Extra}}}))
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeAccountEditFixture(t, w, r, false) }))
			defer runtime.Close()
			var err error
			s.deps.Runtime, err = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(err)
			token, err := s.startSession()
			check(err)
			for _, container := range []string{"equipment", "equipped"} {
				slot := 9
				if container == "equipped" {
					slot = 0
				}
				status, out := v1TestCall(t, s, token, "GET", fmt.Sprintf("/api/v1/characters/10/inventory/%s/%d/options", container, slot))
				if status != 200 {
					t.Fatal(status, out)
				}
				d := out["data"].(map[string]any)
				if d["canReplace"] != true {
					t.Fatal(d)
				}
				body := map[string]any{"accountId": "wallet:1", "action": "configure", "items": []any{map[string]any{"container": container, "slot": slot, "revision": d["item"].(map[string]any)["revision"]}}, "options": map[string]any{"itemId": "8001"}, "pvfChecksum": source.checksum, "reason": "替换测试"}
				call := func(path, key string, want int) map[string]any {
					t.Helper()
					b, _ := json.Marshal(body)
					code, res := operationCall(s, token, "characters/10/inventory-operations"+path, string(b), key, nil)
					if code != want {
						t.Fatal(code, res)
					}
					return res["data"].(map[string]any)
				}
				plan := call("/preview", "", 200)
				body["planToken"] = plan["planToken"]
				first := call("", "replace-item-"+container, 200)
				again := call("", "replace-item-"+container, 200)
				if !reflect.DeepEqual(first, again) {
					t.Fatal("replay differs")
				}
				b, _ := json.Marshal(body)
				status, _ = operationCall(s, token, "characters/10/inventory-operations", string(b), "replace-stale-"+container, nil)
				if status != 409 {
					t.Fatal("stale", status)
				}
			}
			inv, _, err := open().Inventory.Load(ctx, "10")
			check(err)
			got := inv.Slots["0:9"]
			if got.ItemID != 8001 || binary.LittleEndian.Uint32(got.RawEntry[2:6]) != 8001 || got.RawEntry[100] != 0xad || got.RawEntry[0x0a] != 31 || !got.Bind || got.Extra["serial"] != "keep-instance" {
				t.Fatal(got)
			}
			eq, _, err := open().Equipment.Load(ctx, "10")
			check(err)
			if eq.Entries["worn"].ItemID != 8001 {
				t.Fatal(eq)
			}
			// A failed receipt rolls back the ID and raw bytes in the same transaction.
			back := fmt.Sprint(entries[0].ID)
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "10", Action: "configure", Items: []adminops.InventorySelection{{Container: "equipment", Slot: 9, Revision: adminops.InventoryRevision("wallet:1", "10", "equipment", 9, got)}}, Options: &adminops.InventoryOptions{ItemID: &back}, PVFChecksum: source.checksum, Reason: "rollback", Actor: "test", PlanToken: "plan", Key: "replace-failed-receipt"}
			_, err = adminops.RunInventoryOperation(ctx, inventoryFailingUOW{g.Character.(repository.InventoryManagementUnitOfWork)}, cmd, true, s.v1InventoryMutator(time.Now(), "nonce"), s.v1InventoryProject, func(context.Context, string) error { return nil })
			if err == nil {
				t.Fatal("receipt failure accepted")
			}
			after, _, err := open().Inventory.Load(ctx, "10")
			check(err)
			if !reflect.DeepEqual(inv, after) {
				t.Fatal("failed replace persisted")
			}
			if _, err = s.v1ReplaceInventoryItem(cmd.Items[0], got, replacement, fmt.Sprint(itemcfgTestAvatarID), time.Now()); err == nil {
				t.Fatal("cross-type replace")
			}
			for _, target := range []adminops.InventorySelection{{Container: "pets", Slot: 0}, {Container: "equipped", Slot: 27}, {Container: "shared", Slot: 354}} {
				if _, err = s.v1ReplaceInventoryItem(target, got, replacement, back, time.Now()); err == nil {
					t.Fatal("linked replace", target)
				}
			}
			avatar := stack
			avatar.ItemID = itemcfgTestAvatarID
			avatar.RawEntry = append([]byte(nil), raw...)
			avatar.RawEntry[0x0a] = 19
			attrs := s.v1InventoryRow("avatars", repository.InventorySlot{Stack: avatar}, true)["attributes"].(map[string]any)
			for _, field := range []string{"reinforce", "amplifyType", "forging"} {
				if attrs[field] != nil {
					t.Fatal("avatar interpreted equipment", field, attrs)
				}
			}
			// Defaults clear bonuses, preserve learned skills and deduct points already spent.
			check(g.Skill.Save(ctx, repository.SkillRecord{CharacterID: "10", Points: repository.SkillPointState{TotalSP: 340, RemainingSP: 120, TotalTP: 8, RemainingTP: 3, SyncedLevel: 2}, Skills: map[int64]repository.SkillState{42: {Level: 3, Enabled: true}}}))
			points := g.Character.(repository.SkillPointManagementUnitOfWork)
			read := func() repository.SkillPointManagementState {
				v, e := open().Character.(repository.SkillPointManagementReader).LoadSkillPointManagement(ctx, "10")
				check(e)
				return v
			}
			def := adminops.SkillPointDefinition{PVFChecksum: source.checksum, Version: 1, Level: 2, SP: 300, TP: 5}
			resolve := func(context.Context, int) (adminops.SkillPointDefinition, error) { return def, nil }
			pc := adminops.SkillPointCommand{AccountID: "wallet:1", CharacterID: "10", Action: "reset_defaults", Target: "both", Amount: "0", Revision: adminops.SkillPointRevision(read()), DefinitionRevision: adminops.SkillPointDefinitionRevision(def), Reason: "defaults", Actor: "test", Key: "reset-defaults-once"}
			before := read()
			_, err = adminops.RunSkillPointOperation(ctx, pointFailUOW{points, false}, pc, true, resolve, func(context.Context, string) error { return nil })
			if err == nil || !reflect.DeepEqual(before, read()) {
				t.Fatal("default reset rollback", err)
			}
			plan, err := adminops.RunSkillPointOperation(ctx, points, pc, false, resolve, nil)
			check(err)
			if !reflect.DeepEqual(before, read()) {
				t.Fatal("reset preview wrote")
			}
			for range 2 {
				receipt, e := adminops.RunSkillPointOperation(ctx, points, pc, true, resolve, func(context.Context, string) error { return nil })
				check(e)
				if receipt.After != plan.After {
					t.Fatal(receipt)
				}
			}
			balance, err := adminops.SkillPointValues(read())
			check(err)
			if balance.TotalSP != 300 || balance.RemainingSP != 80 || balance.TotalTP != 5 || balance.RemainingTP != 0 || balance.BonusSP != 0 || balance.BonusTP != 0 {
				t.Fatal(balance)
			}
			skills, _, err := open().Skill.Load(ctx, "10")
			check(err)
			if skills.Skills[42].Level != 3 {
				t.Fatal("learned skills changed")
			}
			pc.Key = "reset-stale-request"
			if _, err = adminops.RunSkillPointOperation(ctx, points, pc, true, resolve, func(context.Context, string) error { return nil }); !errors.Is(err, adminops.ErrConflict) {
				t.Fatal(err)
			}
		})
	}
}

func TestV1PresenceDirectoryMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			g := open()
			ctx := context.Background()
			for i := 100; i < 135; i++ {
				if e := g.Character.Save(ctx, repository.CharacterRecord{CharacterID: fmt.Sprint(i), AccountID: "wallet:1", Name: fmt.Sprintf("筛选%d", i), Job: "0", Level: 90, Slot: i - 100}); e != nil {
					t.Fatal(e)
				}
			}
			response := adminapi.Response{Version: 1, OK: true, OnlineSnapshot: &adminapi.OnlineSnapshot{ID: strings.Repeat("a", 32), ObservedAt: time.Now().UTC(), Complete: true, Members: []adminapi.OnlineMember{{CharacterID: 130, Generation: 1}, {CharacterID: 134, Generation: 1}}}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(response) }))
			defer server.Close()
			var err error
			s.deps.Runtime, err = adminapi.NewClient(server.URL+adminapi.Path, strings.Repeat("x", 32))
			if err != nil {
				t.Fatal(err)
			}
			token, _ := s.startSession()
			status, out := v1TestCall(t, s, token, "GET", "/api/v1/characters?online=online&limit=1")
			if status != 200 {
				t.Fatal(status, out)
			}
			rows := out["data"].([]any)
			if len(rows) != 1 || rows[0].(map[string]any)["characterId"] != "130" {
				t.Fatal(rows)
			}
			cursor := out["page"].(map[string]any)["nextCursor"].(string)
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/characters?online=online&limit=1&cursor="+url.QueryEscape(cursor))
			if status != 200 || out["data"].([]any)[0].(map[string]any)["characterId"] != "134" {
				t.Fatal(status, out)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters?online=offline&limit=1&cursor="+url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal("cross-filter cursor", status)
			}
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/characters?online=offline&limit=100")
			if status != 200 || len(out["data"].([]any)) != 33 {
				t.Fatal(status, out)
			}
			response.OnlineSnapshot.Complete = false
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters?online=online")
			if status != 503 {
				t.Fatal("incomplete snapshot", status)
			}
		})
	}
}
