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
	"sync/atomic"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestV1AccountInventoryMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			u := g.AccountInventory.(repository.InventoryManagementUnitOfWork)
			for _, role := range []repository.CharacterRecord{{CharacterID: "10", AccountID: "wallet:1", Name: "甲", Slot: 0}, {CharacterID: "11", AccountID: "wallet:1", Name: "乙", Slot: 1}, {CharacterID: "20", AccountID: "wallet:2", Name: "丙", Slot: 0}} {
				if e := g.Character.Save(ctx, role); e != nil {
					t.Fatal(e)
				}
			}
			source := newItemCacheTestSource(readonlyTestSource{itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText, itemcfgSeparateUpgradePVFPath: itemcfgTestSeparateUpgradeText})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			entries := itemcfgTestEntries()
			entries[1].StackLimit = 99
			crystal := entries[1]
			crystal.ID = 3033
			crystal.Name = "黑色小晶块"
			entries = append(entries, crystal)
			s.items.setEntriesLocked(entries)
			raw := make([]byte, 119)
			binary.LittleEndian.PutUint32(raw[2:6], 3033)
			binary.LittleEndian.PutUint32(raw[6:10], 1000)
			initial := repository.AccountInventoryRecord{AccountID: "wallet:1", Slots: map[string]repository.ItemStack{"12:0": {ItemID: itemcfgTestWeaponID, Count: 1, Extra: map[string]string{"serial": "account-owned"}}, "12:1": {ItemID: itemcfgTestPotionID, Count: 5}, "0:354": {ItemID: 3033, Count: 1000, RawEntry: raw, Extra: map[string]string{"quantity": "1000"}}, "0:355": {ItemID: 3034, Count: 456}}}
			if e := g.AccountInventory.Save(ctx, initial); e != nil {
				t.Fatal(e)
			}
			personal := repository.InventoryRecord{CharacterID: "10", Slots: map[string]repository.ItemStack{"0:354": {ItemID: 3033, Count: 17}}, Warehouse: map[string]repository.ItemStack{"12:0": {ItemID: itemcfgTestWeaponID, Count: 1, Extra: map[string]string{"serial": "stale-character-copy"}}}}
			if e := g.Inventory.Save(ctx, personal); e != nil {
				t.Fatal(e)
			}
			personal, _, _ = g.Inventory.Load(ctx, "10")
			load := func() repository.AccountInventoryRecord {
				v, ok, e := open().AccountInventory.Load(ctx, "wallet:1")
				if e != nil || !ok {
					t.Fatal(ok, e)
				}
				return v
			}
			initial = load()
			selection := func(container string, slot int, stack repository.ItemStack) adminops.InventorySelection {
				return adminops.InventorySelection{Container: container, Slot: slot, Revision: adminops.InventoryRevision("wallet:1", "10", container, slot, stack)}
			}
			q, upgrade := 0, 12
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "10", Action: "configure", Items: []adminops.InventorySelection{selection("account-cargo", 0, initial.Slots["12:0"])}, Options: &adminops.InventoryOptions{QualityMode: &q, UpgradeLevel: &upgrade}, PVFChecksum: source.checksum, Reason: "金库装备修复", Actor: "管理员", PlanToken: "test-plan", Key: "account-inventory-001"}
			mutate := s.v1InventoryMutator(time.Now().UTC().Truncate(time.Second), "account-fixed-seed")
			offline := func(context.Context, string) error { return nil }
			plan, e := adminops.RunInventoryOperation(ctx, u, cmd, false, mutate, s.v1InventoryProject, nil)
			if e != nil || plan.OwnerScope != "account" || !reflect.DeepEqual(initial, load()) {
				t.Fatal("preview", e)
			}
			results := make(chan error, 6)
			for range 6 {
				go func() {
					r, e := adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline)
					a, _ := json.Marshal(r.Changes)
					b, _ := json.Marshal(plan.Changes)
					if e == nil && string(a) != string(b) {
						e = errors.New("random preview mismatch")
					}
					results <- e
				}()
			}
			for range 6 {
				if e := <-results; e != nil {
					t.Fatal(e)
				}
			}
			if v := load().Slots["12:0"]; v.Extra["serial"] != "account-owned" || itemcfgUpgradeLevelOf(v) != 12 {
				t.Fatal(v)
			}
			if v, _, _ := open().Inventory.Load(ctx, "10"); !reflect.DeepEqual(personal, v) {
				t.Fatal("wrote character residue")
			}
			if log, e := u.InventoryManagementLog(ctx, "wallet:1", 0, 10); e != nil || len(log) != 1 {
				t.Fatal(log, e)
			}
			if log, e := g.Character.(repository.InventoryManagementUnitOfWork).InventoryManagementLog(ctx, "10", 0, 10); e != nil || len(log) != 0 {
				t.Fatal("role ledger polluted", e)
			}
			if log, e := g.Account.(repository.AccountManagementUnitOfWork).AccountManagementLog(ctx, "wallet:1", 0, 10); e != nil || len(log) != 0 {
				t.Fatal("currency ledger polluted", e)
			}
			count := "2000"
			cmd.Key = "account-crystals-001"
			cmd.Items = []adminops.InventorySelection{selection("shared", 354, load().Slots["0:354"])}
			cmd.Options = &adminops.InventoryOptions{Count: &count}
			before := load()
			for _, late := range []bool{false, true} {
				var checks atomic.Int32
				_, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, func(_ context.Context, id string) error {
					n := checks.Add(1)
					if id == "11" && (!late || n > 2) {
						return adminops.ErrCharacterOnline
					}
					return nil
				})
				if !errors.Is(e, adminops.ErrCharacterOnline) || !reflect.DeepEqual(before, load()) {
					t.Fatal("sibling online guard", late, e)
				}
			}
			if _, e = adminops.RunInventoryOperation(ctx, inventoryFailingUOW{u}, cmd, true, mutate, s.v1InventoryProject, offline); e == nil || !reflect.DeepEqual(before, load()) {
				t.Fatal("receipt rollback", e)
			}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e != nil {
				t.Fatal(e)
			}
			v := load().Slots["0:354"]
			if v.Count != 2000 || v.Extra["quantity"] != "2000" || binary.LittleEndian.Uint32(v.RawEntry[6:10]) != 2000 || load().Slots["0:355"].Count != 456 {
				t.Fatal(v)
			}
			if adminops.InventoryRevision("wallet:1", "10", "shared", 354, v) != adminops.InventoryRevision("wallet:1", "11", "shared", 354, v) {
				t.Fatal("shared revision depends on role")
			}
			bad := cmd
			bad.CharacterID = "20"
			bad.Key = "account-wrong-owner"
			if _, e = adminops.RunInventoryOperation(ctx, u, bad, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("foreign role accepted")
			}
			bad = cmd
			bad.Items = append(append([]adminops.InventorySelection{}, cmd.Items...), selection("equipment", 9, repository.ItemStack{ItemID: itemcfgTestWeaponID, Count: 1}))
			if _, e = adminops.RunInventoryOperation(ctx, u, bad, false, mutate, s.v1InventoryProject, nil); !errors.Is(e, adminops.ErrInventoryInput) {
				t.Fatal("mixed owners", e)
			}
			// Both characters address one account slot and compete on the same revision.
			cmd.Action = "delete"
			cmd.Options = nil
			cmd.Items = []adminops.InventorySelection{selection("account-cargo", 1, load().Slots["12:1"])}
			for _, id := range []string{"10", "11"} {
				copy := cmd
				copy.CharacterID = id
				copy.Key = "account-race-" + id + "-request"
				go func() {
					_, e := adminops.RunInventoryOperation(ctx, u, copy, true, nil, s.v1InventoryProject, offline)
					results <- e
				}()
			}
			wins, conflicts := 0, 0
			for range 2 {
				e := <-results
				if e == nil {
					wins++
				} else if errors.Is(e, adminops.ErrInventorySlot) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatal(wins, conflicts)
			}
			// Exercise the account receipt size limit and preservation of shared rows.
			many := load()
			for i := 10; i < 210; i++ {
				many.Slots[fmt.Sprintf("12:%d", i)] = repository.ItemStack{ItemID: itemcfgTestPotionID, Count: 5}
			}
			if e = g.AccountInventory.Save(ctx, many); e != nil {
				t.Fatal(e)
			}
			many = load()
			batch := cmd
			batch.Action = "configure"
			batch.Key = "account-inventory-200"
			batch.Items = nil
			count = "9"
			batch.Options = &adminops.InventoryOptions{Count: &count}
			for i := 10; i < 210; i++ {
				batch.Items = append(batch.Items, selection("account-cargo", i, many.Slots[fmt.Sprintf("12:%d", i)]))
			}
			manyResult, e := adminops.RunInventoryOperation(ctx, u, batch, true, mutate, s.v1InventoryProject, offline)
			if e != nil || len(manyResult.Changes) != 200 || load().Slots["0:354"].Count != 2000 {
				t.Fatal("account maximum batch", e)
			}
			// The complete HTTP path checks the sibling's runtime state and routes its ledger by account.
			var siblingOnline atomic.Bool
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var v adminapi.Request
				json.NewDecoder(r.Body).Decode(&v)
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true, AccountEdit: &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: v.AccountID, Busy: siblingOnline.Load()}})
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			token, _ := s.startSession()
			status, out := v1TestCall(t, s, token, "GET", "/api/v1/characters/11/inventory/shared/354/options")
			if status != 200 {
				t.Fatal(status, out)
			}
			data := out["data"].(map[string]any)
			item := data["item"].(map[string]any)
			body := map[string]any{"accountId": "wallet:1", "action": "configure", "items": []any{map[string]any{"container": "shared", "slot": 354, "revision": item["revision"]}}, "options": map[string]any{"count": "3000"}, "pvfChecksum": source.checksum, "reason": "共享资产调整"}
			encode := func() string { v, _ := json.Marshal(body); return string(v) }
			status, out = operationCall(s, token, "characters/11/inventory-operations/preview", encode(), "", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			body["planToken"] = out["data"].(map[string]any)["planToken"]
			siblingOnline.Store(true)
			status, out = operationCall(s, token, "characters/11/inventory-operations", encode(), "account-inventory-http", nil)
			if status != 409 {
				t.Fatal(status, out)
			}
			siblingOnline.Store(false)
			status, out = operationCall(s, token, "characters/11/inventory-operations", encode(), "account-inventory-http", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			s.deps.PVF = nil
			s.deps.Runtime = nil
			status, out = operationCall(s, token, "characters/11/inventory-operations", encode(), "account-inventory-http", nil)
			if status != 200 {
				t.Fatal("offline replay", status, out)
			}
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:1/inventory-operations?limit=1")
			if status != 200 || len(out["data"].([]any)) != 1 {
				t.Fatal(status, out)
			}
			cursor := out["page"].(map[string]any)["nextCursor"].(string)
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:2/inventory-operations?limit=1&cursor="+cursor)
			if status != 400 {
				t.Fatal("cross-account cursor", status, out)
			}
		})
	}
}
