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
	"dnf90.local/gateway/internal/modules/dnf/equip"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func artifactManagementFixtureRecords(t *testing.T, character string) (repository.InventoryRecord, repository.EquipmentRecord, repository.PetRecord) {
	t.Helper()
	inv, eq, pets := petManagementFixtureRecords(character)
	pets.Artifacts = map[string]repository.ItemStack{}
	for i, color := range []string{"red", "blue", "green"} {
		id := int64(63500 + i*500)
		slot := 27 + i
		raw := make([]byte, 119)
		binary.LittleEndian.PutUint16(raw[:2], uint16(slot))
		binary.LittleEndian.PutUint32(raw[2:6], uint32(id))
		raw[90] = 0xad
		entry := repository.EquipmentEntry{SlotIndex: int16(slot), ItemID: id, Bind: true, ExpireAt: time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second), RawEntry: raw, Extra: map[string]string{"item_kind": "equipment", "equipment_type": "artifact " + color, "serial": strconv.Itoa(1000 + i), "opaque": "preserved"}}
		eq.Entries[strconv.Itoa(slot)] = entry
		projection, err := equip.AdministrativeArtifactProjection(entry)
		if err != nil {
			t.Fatal(err)
		}
		pets.Artifacts[color] = projection
		bag := entry
		bag.RawEntry = append([]byte(nil), raw...)
		binary.LittleEndian.PutUint16(bag.RawEntry[:2], uint16(140+i))
		inv.Slots[fmt.Sprintf("7:%d", 140+i)] = repository.ItemStack{ItemID: id, Count: 1, Bind: bag.Bind, ExpireAt: bag.ExpireAt, RawEntry: bag.RawEntry, Extra: map[string]string{"serial": strconv.Itoa(2000 + i), "opaque": "bag-preserved"}}
	}
	return inv, eq, pets
}

func TestV1ArtifactOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			g := open()
			ctx := context.Background()
			u := g.Character.(repository.InventoryManagementUnitOfWork)
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "宠物装备测试", Level: 90}))
			inv, eq, pets := artifactManagementFixtureRecords(t, "10")
			check(g.Inventory.Save(ctx, inv))
			check(g.Equipment.Save(ctx, eq))
			check(g.Pet.Save(ctx, pets))
			source := newItemCacheTestSource(readonlyTestSource{})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			entries := []itemEntry{}
			for i, color := range []string{"red", "blue", "green"} {
				entries = append(entries, itemEntry{ID: uint32(63500 + i*500), Name: color + "宠物装备", Kind: "equipment", Tag: "artifact " + color})
			}
			s.items.setEntriesLocked(entries)
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true})
			}))
			defer runtime.Close()
			var err error
			s.deps.Runtime, err = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(err)
			token, _ := s.startSession()
			load := func() (repository.InventoryRecord, repository.EquipmentRecord, repository.PetRecord) {
				a, _, e := g.Inventory.Load(ctx, "10")
				check(e)
				b, _, e := g.Equipment.Load(ctx, "10")
				check(e)
				c, _, e := g.Pet.Load(ctx, "10")
				check(e)
				return a, b, c
			}
			get := func(container string, slot int) map[string]any {
				t.Helper()
				status, out := v1TestCall(t, s, token, "GET", fmt.Sprintf("/api/v1/characters/10/inventory/%s/%d/options", container, slot))
				if status != 200 {
					t.Fatal(status, out)
				}
				return out["data"].(map[string]any)
			}
			selection := func(container string, slot int) adminops.InventorySelection {
				t.Helper()
				data := get(container, slot)
				if data["canDelete"] != true {
					t.Fatal(data)
				}
				item := data["item"].(map[string]any)
				v := adminops.InventorySelection{Container: container, Slot: slot, Revision: item["revision"].(string)}
				if related, ok := item["relatedRevision"].(string); ok {
					v.RelatedRevision = related
				}
				return v
			}
			originalInv, originalEq, originalPets := load()
			body := map[string]any{"accountId": "wallet:1", "action": "configure", "items": []adminops.InventorySelection{selection("equipped", 27), selection("pet-equipment", 140)}, "options": map[string]any{"expirationDays": 30}, "pvfChecksum": source.checksum, "reason": "调整宠物装备期限"}
			encode := func() string { raw, _ := json.Marshal(body); return string(raw) }
			preview := func() {
				t.Helper()
				delete(body, "planToken")
				status, out := operationCall(s, token, "characters/10/inventory-operations/preview", encode(), "", nil)
				if status != 200 {
					t.Fatal(status, out)
				}
				body["planToken"] = out["data"].(map[string]any)["planToken"]
			}
			preview()
			a, b, c := load()
			if !reflect.DeepEqual(a, originalInv) || !reflect.DeepEqual(b, originalEq) || !reflect.DeepEqual(c, originalPets) {
				t.Fatal("preview wrote state")
			}
			// A parent revision change invalidates the plan with identical worn rows.
			c.TownDisplay = false
			c.UpdatedAt = c.UpdatedAt.Add(time.Second)
			check(g.Pet.Save(ctx, c))
			status, out := operationCall(s, token, "characters/10/inventory-operations", encode(), "artifact-stale-request", nil)
			if status != 409 {
				t.Fatal(status, out)
			}
			body["items"] = []adminops.InventorySelection{selection("equipped", 27), selection("pet-equipment", 140)}
			preview()
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "artifact-config-request", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			first := out["data"]
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "artifact-config-request", nil)
			if status != 200 || !reflect.DeepEqual(first, out["data"]) {
				t.Fatal("replay", status, out)
			}
			a, b, c = load()
			if !equip.AdministrativeArtifactMatches(b.Entries["27"], c.Artifacts["red"]) || !b.Entries["27"].ExpireAt.After(originalEq.Entries["27"].ExpireAt) || !a.Slots["7:140"].ExpireAt.Equal(b.Entries["27"].ExpireAt) {
				t.Fatal("projection not updated")
			}
			if !reflect.DeepEqual(c.Entries, originalPets.Entries) || c.EquippedKey != originalPets.EquippedKey || c.TownDisplay || !reflect.DeepEqual(c.Artifacts["blue"], originalPets.Artifacts["blue"]) || !reflect.DeepEqual(c.Artifacts["green"], originalPets.Artifacts["green"]) {
				t.Fatal("unselected state changed")
			}
			if b.Entries["27"].RawEntry[90] != 0xad || b.Entries["27"].Extra["opaque"] != "preserved" {
				t.Fatal("opaque data lost")
			}
			// Isolated projection corruption disables options and rejects writes.
			good := repository.ClonePet(c)
			bad := repository.ClonePet(c)
			v := bad.Artifacts["red"]
			v.Count = 2
			bad.Artifacts["red"] = v
			check(g.Pet.Save(ctx, bad))
			if d := get("equipped", 27); d["canDelete"] != false || d["configuration"] != nil {
				t.Fatal("corruption writable", d)
			}
			check(g.Pet.Save(ctx, good))
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "10", Action: "delete", Items: []adminops.InventorySelection{selection("equipped", 27), selection("equipped", 28), selection("pet-equipment", 140), selection("pets", 0)}, Reason: "清理宠物及装备", Actor: "管理员", PVFChecksum: source.checksum, PlanToken: "test-plan", Key: "artifact-mixed-delete"}
			beforeInv, beforeEq, beforePet := load()
			offline := func(context.Context, string) error { return nil }
			mutate := s.v1InventoryMutator(time.Now().UTC(), "fixed")
			unchanged := func() {
				t.Helper()
				a, b, c := load()
				if !reflect.DeepEqual(a, beforeInv) || !reflect.DeepEqual(b, beforeEq) || !reflect.DeepEqual(c, beforePet) {
					t.Fatal("rollback failed")
				}
			}
			for _, failure := range []repository.InventoryManagementUnitOfWork{petFailingUOW{u}, inventoryFailingUOW{u}} {
				if _, e := adminops.RunInventoryOperation(ctx, failure, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
					t.Fatal("injected failure accepted")
				}
				unchanged()
			}
			calls := 0
			lateOnline := func(context.Context, string) error {
				calls++
				if calls > 1 {
					return adminops.ErrCharacterOnline
				}
				return nil
			}
			if _, e := adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, lateOnline); e == nil {
				t.Fatal("online race accepted")
			}
			unchanged()
			results := make(chan error, 2)
			for i := range 2 {
				copy := cmd
				copy.Key += strconv.Itoa(i)
				go func() {
					_, e := adminops.RunInventoryOperation(ctx, u, copy, true, mutate, s.v1InventoryProject, offline)
					results <- e
				}()
			}
			wins := 0
			for range 2 {
				e := <-results
				if e == nil {
					wins++
				} else if !errors.Is(e, adminops.ErrConflict) && !errors.Is(e, adminops.ErrInventorySlot) {
					t.Fatal(e)
				}
			}
			if wins != 1 {
				t.Fatal(wins)
			}
			a, b, c = load()
			if len(c.Artifacts) != 1 || c.Artifacts["green"].ItemID != 64500 || len(c.Entries) != 2 || c.EquippedKey != "103" || len(a.Slots) != 3 || len(b.Entries) != 2 {
				t.Fatal("unexpected linked deletion", a, b, c)
			}
			rows, e := u.InventoryManagementLog(ctx, "10", 0, 20)
			check(e)
			if len(rows) != 2 {
				t.Fatal(len(rows))
			}
		})
	}
}
