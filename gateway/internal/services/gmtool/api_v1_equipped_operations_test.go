package gmtool

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/itemquality"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestV1EquippedOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			u := g.Character.(repository.InventoryManagementUnitOfWork)
			if e := g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "穿戴测试", Level: 90}); e != nil {
				t.Fatal(e)
			}
			source := newItemCacheTestSource(readonlyTestSource{itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText, itemcfgSeparateUpgradePVFPath: itemcfgTestSeparateUpgradeText})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			s.items.setEntriesLocked(itemcfgTestEntries())
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAccountEditFixture(t, w, r, false)
			}))
			defer runtime.Close()
			var e error
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			raw := func(slot uint16) []byte {
				v := make([]byte, 119)
				binary.LittleEndian.PutUint16(v[:2], slot)
				binary.LittleEndian.PutUint32(v[2:6], itemcfgTestWeaponID)
				v[90] = 0xad
				return v
			}
			original := repository.EquipmentRecord{CharacterID: "10", Entries: map[string]repository.EquipmentEntry{
				"legacy-key": {SlotIndex: 12, ItemID: itemcfgTestWeaponID, Bind: true, RawEntry: raw(12), Extra: map[string]string{"serial": "worn-one", "amplify_type": "3", "amplify_value": "777", "current_exe_runtime_move": "1", "current_exe_equipment_type": "12", repository.CloneAvatarAppearanceExtraKey: "123"}},
				"11":         {SlotIndex: 11, ItemID: itemcfgTestWeaponID, RawEntry: raw(11)},
				"26":         {SlotIndex: 26, ItemID: 99, RawEntry: []byte{1, 2, 3}, Extra: map[string]string{"pet_serial": "987"}},
			}}
			if e = g.Equipment.Save(ctx, original); e != nil {
				t.Fatal(e)
			}
			load := func() repository.EquipmentRecord {
				v, ok, e := g.Equipment.Load(ctx, "10")
				if e != nil || !ok {
					t.Fatal(ok, e)
				}
				return v
			}
			original = load()
			token, _ := s.startSession()
			get := func(slot int) map[string]any {
				t.Helper()
				status, out := v1TestCall(t, s, token, "GET", fmt.Sprintf("/api/v1/characters/10/inventory/equipped/%d/options", slot))
				if status != 200 {
					t.Fatal(status, out)
				}
				return out["data"].(map[string]any)
			}
			for _, slot := range []int{12, 11} {
				data := get(slot)
				item := data["item"].(map[string]any)
				if data["canDelete"] != true || data["quantity"] != nil || data["configuration"] == nil || data["ownerScope"] != "character" {
					t.Fatal(data)
				}
				body := map[string]any{"accountId": "wallet:1", "action": "configure", "items": []any{map[string]any{"container": "equipped", "slot": slot, "revision": item["revision"]}}, "options": map[string]any{"qualityMode": 1, "upgradeLevel": 12, "amplifyType": 3, "forgingLevel": 7}, "pvfChecksum": source.checksum, "reason": "修复穿戴属性"}
				encode := func() string { v, _ := json.Marshal(body); return string(v) }
				before := load()
				status, out := operationCall(s, token, "characters/10/inventory-operations/preview", encode(), "", nil)
				if status != 200 {
					t.Fatal(status, out)
				}
				if !reflect.DeepEqual(before, load()) {
					t.Fatal("preview wrote equipment")
				}
				body["planToken"] = out["data"].(map[string]any)["planToken"]
				key := fmt.Sprintf("equipped-http-request-%d", slot)
				status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), key, nil)
				if status != 200 || out["data"].(map[string]any)["ownerScope"] != "character" {
					t.Fatal(status, out)
				}
				first := out["data"]
				status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), key, nil)
				if status != 200 || !reflect.DeepEqual(first, out["data"]) {
					t.Fatal("replay", status, out)
				}
				if _, found, e := g.Inventory.Load(ctx, "10"); e != nil || found {
					t.Fatal("worn-only edit created bag", found, e)
				}
			}
			entry := load().Entries["legacy-key"]
			if entry.SlotIndex != 12 || !entry.Bind || len(entry.RawEntry) != 119 || entry.RawEntry[90] != 0xad || binary.LittleEndian.Uint32(entry.RawEntry[6:10]) != itemquality.TopSeed || entry.Extra["amplify_value"] != "777" || entry.Extra[repository.CloneAvatarAppearanceExtraKey] != "123" || entry.Extra["current_exe_equipment_type"] != "12" || !reflect.DeepEqual(load().Entries["26"], original.Entries["26"]) {
				t.Fatal(entry)
			}
			if get(26)["canDelete"] != false {
				t.Fatal("pet accepted without its aggregate")
			}
			for _, slot := range []int{33, -1} {
				if _, _, e := adminops.InventoryTarget(adminops.InventorySelection{Container: "equipped", Slot: slot}); e == nil {
					t.Fatal("invalid worn target accepted", slot)
				}
			}
			quality := 0
			selection := func(slot int) adminops.InventorySelection {
				return adminops.InventorySelection{Container: "equipped", Slot: slot, Revision: get(slot)["item"].(map[string]any)["revision"].(string)}
			}
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "10", Action: "configure", Items: []adminops.InventorySelection{selection(12)}, Options: &adminops.InventoryOptions{QualityMode: &quality}, Reason: "并发穿戴修复", Actor: "测试管理员", PVFChecksum: source.checksum, PlanToken: "frozen-plan", Key: "equipped-race-request"}
			mutate := s.v1InventoryMutator(time.Now().UTC().Truncate(time.Second), "fixed")
			offline := func(context.Context, string) error { return nil }
			badCount := "2"
			bad := cmd
			bad.Options = &adminops.InventoryOptions{Count: &badCount}
			beforeQuantity := load()
			if _, e = adminops.RunInventoryOperation(ctx, u, bad, true, mutate, s.v1InventoryProject, offline); e == nil || !reflect.DeepEqual(beforeQuantity, load()) {
				t.Fatal("worn quantity accepted or changed instance", e)
			}
			results := make(chan error, 2)
			for i := range 2 {
				copy := cmd
				copy.Key += fmt.Sprint(i)
				go func() {
					_, e := adminops.RunInventoryOperation(ctx, u, copy, true, mutate, s.v1InventoryProject, offline)
					results <- e
				}()
			}
			wins, conflicts := 0, 0
			for range 2 {
				e := <-results
				if e == nil {
					wins++
				} else if errors.Is(e, adminops.ErrConflict) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatal(wins, conflicts)
			}
			bag := repository.InventoryRecord{CharacterID: "10", Slots: map[string]repository.ItemStack{"0:9": {ItemID: itemcfgTestWeaponID, Count: 1}, "3:12": {ItemID: 123, Count: 1}}, Warehouse: map[string]repository.ItemStack{"2:0": {ItemID: itemcfgTestWeaponID, Count: 1}}}
			if e = g.Inventory.Save(ctx, bag); e != nil {
				t.Fatal(e)
			}
			bag, _, _ = g.Inventory.Load(ctx, "10")
			before := load()
			cmd.Action, cmd.Options, cmd.Key = "delete", nil, "equipped-mixed-rollback"
			cmd.Items = []adminops.InventorySelection{selection(12), {Container: "equipment", Slot: 9, Revision: adminops.InventoryRevision("wallet:1", "10", "equipment", 9, bag.Slots["0:9"])}, {Container: "warehouse", Slot: 0, Revision: adminops.InventoryRevision("wallet:1", "10", "warehouse", 0, bag.Warehouse["2:0"])}}
			if _, e = adminops.RunInventoryOperation(ctx, inventoryFailingUOW{u}, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("receipt failure accepted")
			}
			gotBag, _, e := g.Inventory.Load(ctx, "10")
			if e != nil || !reflect.DeepEqual(bag, gotBag) || !reflect.DeepEqual(before, load()) {
				t.Fatal("mixed rollback failed", e)
			}
			checks := 0
			lateOnline := func(context.Context, string) error {
				checks++
				if checks == 2 {
					return adminops.ErrCharacterOnline
				}
				return nil
			}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, lateOnline); !errors.Is(e, adminops.ErrCharacterOnline) {
				t.Fatal(e)
			}
			gotBag, _, e = g.Inventory.Load(ctx, "10")
			if e != nil || !reflect.DeepEqual(bag, gotBag) || !reflect.DeepEqual(before, load()) {
				t.Fatal("late online failed to roll back both aggregates", e)
			}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e != nil {
				t.Fatal(e)
			}
			gotBag, _, e = g.Inventory.Load(ctx, "10")
			if e != nil || len(gotBag.Slots) != 1 || !reflect.DeepEqual(gotBag.Slots["3:12"], bag.Slots["3:12"]) || len(gotBag.Warehouse) != 0 || len(load().Entries) != 2 {
				t.Fatal("mixed delete or legacy bag copy", gotBag, e)
			}
			// Duplicate persisted slot indexes must not select and overwrite one arbitrarily.
			duplicate := load()
			duplicate.Entries["duplicate"] = duplicate.Entries["11"]
			if e = g.Equipment.Save(ctx, duplicate); e != nil {
				t.Fatal(e)
			}
			cmd.Items = []adminops.InventorySelection{selection(11)}
			cmd.Key = "equipped-duplicate-slot"
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); !errors.Is(e, adminops.ErrInventoryInput) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(load().Entries, duplicate.Entries) {
				t.Fatal("duplicate slot changed")
			}
		})
	}
}
