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
	"strconv"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func petManagementFixtureRecords(character string) (repository.InventoryRecord, repository.EquipmentRecord, repository.PetRecord) {
	inv := repository.InventoryRecord{CharacterID: character, Slots: map[string]repository.ItemStack{}}
	eq := repository.EquipmentRecord{CharacterID: character, Entries: map[string]repository.EquipmentEntry{}}
	pets := repository.PetRecord{CharacterID: character, Entries: map[string]repository.PetEntry{}, EquippedKey: "103", TownDisplay: true}
	for i := 0; i < 3; i++ {
		serial := uint32(101 + i)
		key := strconv.Itoa(int(serial))
		id := int64(63000 + i)
		extra := map[string]string{"creature_serial_or_handle": key, "item_kind": "equipment", "equipment_type": "creature"}
		list, slot := byte(7), int16(i)
		if i < 2 {
			raw := make([]byte, 119)
			binary.LittleEndian.PutUint32(raw[2:6], uint32(id))
			raw[90] = 0xad
			inv.Slots[fmt.Sprintf("7:%d", i)] = repository.ItemStack{ItemID: id, Count: 1, RawEntry: raw, Extra: extra}
		} else {
			list, slot = 3, 26
			raw := make([]byte, 47)
			raw[0] = 26
			binary.LittleEndian.PutUint32(raw[1:5], uint32(id))
			binary.LittleEndian.PutUint32(raw[5:9], serial)
			binary.LittleEndian.PutUint32(raw[24:28], serial)
			eq.Entries["26"] = repository.EquipmentEntry{SlotIndex: 26, ItemID: id, RawEntry: raw, Extra: extra}
		}
		pets.Entries[key] = repository.PetEntry{PetKey: key, CreatureKey: serial, ItemID: id, SourceListType: list, SourceSlotIndex: slot, Name: []string{"云朵", "流星", "小羽"}[i], Level: 5 + int64(i), Exp: 1200, Satiety: 85, SatietyMicros: 85_000_000, AppliedClearTokens: map[string]bool{"clear-proof": true}, AppliedClearTokenOrder: []string{"clear-proof"}, Extra: map[string]string{"enchant_card_id": "1234", "opaque": "preserved"}}
	}
	return inv, eq, pets
}

type petSaveFailure struct{ repository.PetRepository }

func (p petSaveFailure) SaveFields(context.Context, repository.PetRecord, ...repository.PetField) error {
	return errors.New("injected pet save failure")
}

type petFailingUOW struct {
	repository.InventoryManagementUnitOfWork
}

func (u petFailingUOW) WithinInventoryManagement(ctx context.Context, a, c string, fn func(repository.Group, repository.ManagementReceiptStore) error) error {
	return u.InventoryManagementUnitOfWork.WithinInventoryManagement(ctx, a, c, func(g repository.Group, r repository.ManagementReceiptStore) error {
		g.Pet = petSaveFailure{g.Pet}
		return fn(g, r)
	})
}

func TestV1PetOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			u := g.Character.(repository.InventoryManagementUnitOfWork)
			if e := g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "宠物测试", Level: 90}); e != nil {
				t.Fatal(e)
			}
			inv, eq, pets := petManagementFixtureRecords("10")
			pets.Artifacts = map[string]repository.ItemStack{"red": {ItemID: 100, Count: 1, Extra: map[string]string{"proof": "artifact-preserved"}}}
			if e := g.Inventory.Save(ctx, inv); e != nil {
				t.Fatal(e)
			}
			if e := g.Equipment.Save(ctx, eq); e != nil {
				t.Fatal(e)
			}
			if e := g.Pet.Save(ctx, pets); e != nil {
				t.Fatal(e)
			}
			source := newItemCacheTestSource(readonlyTestSource{})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			entries := []itemEntry{}
			for i := 0; i < 3; i++ {
				entries = append(entries, itemEntry{ID: uint32(63000 + i), Name: "宠物模板", Kind: "equipment", Tag: "creature"})
			}
			s.items.setEntriesLocked(entries)
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAccountEditFixture(t, w, r, false)
			}))
			defer runtime.Close()
			var e error
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			token, _ := s.startSession()
			get := func(container string, slot int) map[string]any {
				t.Helper()
				status, out := v1TestCall(t, s, token, "GET", fmt.Sprintf("/api/v1/characters/10/inventory/%s/%d/options", container, slot))
				if status != 200 {
					t.Fatal(status, out)
				}
				return out["data"].(map[string]any)
			}
			selection := func(container string, slot int) adminops.InventorySelection {
				data := get(container, slot)
				if data["canDelete"] != true || data["configuration"] != nil || data["quantity"] != nil {
					t.Fatal(data)
				}
				item := data["item"].(map[string]any)
				return adminops.InventorySelection{Container: container, Slot: slot, Revision: item["revision"].(string), RelatedRevision: item["relatedRevision"].(string)}
			}
			loadPets := func() repository.PetRecord {
				v, found, e := g.Pet.Load(ctx, "10")
				if e != nil || !found {
					t.Fatal(found, e)
				}
				return v
			}
			before := loadPets()
			body := map[string]any{"accountId": "wallet:1", "action": "delete", "items": []adminops.InventorySelection{selection("pets", 0)}, "pvfChecksum": source.checksum, "reason": "移除所选宠物"}
			encode := func() string { v, _ := json.Marshal(body); return string(v) }
			status, out := operationCall(s, token, "characters/10/inventory-operations/preview", encode(), "", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			plan := out["data"].(map[string]any)
			body["planToken"] = plan["planToken"]
			change := plan["receipt"].(map[string]any)["changes"].([]any)[0].(map[string]any)
			if change["before"].(map[string]any)["pet"].(map[string]any)["level"] != "5" || !reflect.DeepEqual(before, loadPets()) {
				t.Fatal("preview omitted growth or wrote state", change)
			}
			// A growth update alone invalidates the plan even if the inventory row stays unchanged.
			p := loadPets()
			entry := p.Entries["101"]
			entry.Level = 9
			p.Entries["101"] = entry
			if e = g.Pet.Save(ctx, p); e != nil {
				t.Fatal(e)
			}
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "pet-growth-conflict", nil)
			if status != 409 {
				t.Fatal(status, out)
			}
			v, _, _ := g.Inventory.Load(ctx, "10")
			if _, ok := v.Slots["7:0"]; !ok {
				t.Fatal("stale preview deleted item")
			}
			body["items"] = []adminops.InventorySelection{selection("pets", 0)}
			delete(body, "planToken")
			status, out = operationCall(s, token, "characters/10/inventory-operations/preview", encode(), "", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			body["planToken"] = out["data"].(map[string]any)["planToken"]
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "pet-delete-http-request", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			receipt := out["data"]
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "pet-delete-http-request", nil)
			if status != 200 || !reflect.DeepEqual(receipt, out["data"]) {
				t.Fatal("replay", status, out)
			}
			p = loadPets()
			if len(p.Entries) != 2 || p.EquippedKey != "103" || !reflect.DeepEqual(p.Artifacts, before.Artifacts) || !reflect.DeepEqual(p.Entries["102"], before.Entries["102"]) {
				t.Fatal(p)
			}
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "10", Action: "delete", Items: []adminops.InventorySelection{selection("pets", 1), selection("equipped", 26)}, Reason: "清理宠物与出战状态", Actor: "管理员", PVFChecksum: source.checksum, PlanToken: "frozen-plan", Key: "pet-mixed-removal"}
			offline := func(context.Context, string) error { return nil }
			mutate := s.v1InventoryMutator(time.Now().UTC(), "fixed")
			before = loadPets()
			beforeInv, _, _ := g.Inventory.Load(ctx, "10")
			beforeEq, _, _ := g.Equipment.Load(ctx, "10")
			for _, failure := range []repository.InventoryManagementUnitOfWork{petFailingUOW{u}, inventoryFailingUOW{u}} {
				if _, e = adminops.RunInventoryOperation(ctx, failure, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
					t.Fatal("failure accepted")
				}
				a, _, _ := g.Inventory.Load(ctx, "10")
				b, _, _ := g.Equipment.Load(ctx, "10")
				if !reflect.DeepEqual(a, beforeInv) || !reflect.DeepEqual(b, beforeEq) || !reflect.DeepEqual(loadPets(), before) {
					t.Fatal("linked rollback failed")
				}
			}
			// Duplicate physical aliases cannot share a single creature record for deletion.
			dup := repository.CloneInventory(beforeInv)
			dup.Slots["7:2"] = dup.Slots["7:1"]
			if e = g.Inventory.Save(ctx, dup); e != nil {
				t.Fatal(e)
			}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("duplicate physical creature accepted")
			}
			if e = g.Inventory.Save(ctx, beforeInv); e != nil {
				t.Fatal(e)
			}
			// Concurrent requests serialize across bag, worn slot, growth and the receipt.
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
				} else if errors.Is(e, adminops.ErrInventorySlot) || errors.Is(e, adminops.ErrConflict) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatal(wins, conflicts)
			}
			p = loadPets()
			v, _, _ = g.Inventory.Load(ctx, "10")
			w, _, _ := g.Equipment.Load(ctx, "10")
			if len(p.Entries) != 0 || p.EquippedKey != "" || !p.TownDisplay || len(v.Slots) != 0 || len(w.Entries) != 0 || !reflect.DeepEqual(p.Artifacts, before.Artifacts) {
				t.Fatal("final linked state", p, v, w)
			}
			rows, e := u.InventoryManagementLog(ctx, "10", 0, 20)
			if e != nil || len(rows) != 2 {
				t.Fatal(len(rows), e)
			}
		})
	}
}
