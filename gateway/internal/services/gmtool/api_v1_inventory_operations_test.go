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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/itemquality"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type inventoryFailingUOW struct {
	repository.InventoryManagementUnitOfWork
}

func (f inventoryFailingUOW) WithinInventoryManagement(ctx context.Context, a, c string, fn func(repository.Group, repository.ManagementReceiptStore) error) error {
	return f.InventoryManagementUnitOfWork.WithinInventoryManagement(ctx, a, c, func(g repository.Group, r repository.ManagementReceiptStore) error { return fn(g, failingReceipts{r}) })
}
func TestV1InventoryOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			u := g.Character.(repository.InventoryManagementUnitOfWork)
			if e := g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "背包测试", Job: "0", Level: 90}); e != nil {
				t.Fatal(e)
			}
			source := newItemCacheTestSource(readonlyTestSource{itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText, itemcfgSeparateUpgradePVFPath: itemcfgTestSeparateUpgradeText})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			entries := itemcfgTestEntries()
			entries[1].StackLimit = 99
			s.items.setEntriesLocked(entries)
			raw := make([]byte, 119)
			raw[90] = 0xad
			binary.LittleEndian.PutUint32(raw[2:6], itemcfgTestWeaponID)
			potionRaw := make([]byte, 119)
			binary.LittleEndian.PutUint32(potionRaw[2:6], itemcfgTestPotionID)
			binary.LittleEndian.PutUint32(potionRaw[6:10], 5)
			original := repository.InventoryRecord{CharacterID: "10", Slots: map[string]repository.ItemStack{"0:9": {ItemID: itemcfgTestWeaponID, Count: 1, RawEntry: raw, Extra: map[string]string{"serial": "unique-instance", "amplify_type": "3", "amplify_value": "777"}}, "0:65": {ItemID: itemcfgTestPotionID, Count: 5, RawEntry: append([]byte(nil), potionRaw...), Extra: map[string]string{"amount_or_count": "5"}}}, Warehouse: map[string]repository.ItemStack{"2:0": {ItemID: itemcfgTestWeaponID, Count: 1}}}
			if e := g.Inventory.Save(ctx, original); e != nil {
				t.Fatal(e)
			}
			load := func() repository.InventoryRecord {
				r, _, e := open().Inventory.Load(ctx, "10")
				if e != nil {
					t.Fatal(e)
				}
				return r
			}
			original = load()
			selection := func(container string, slot int, stack repository.ItemStack) adminops.InventorySelection {
				return adminops.InventorySelection{Container: container, Slot: slot, Revision: adminops.InventoryRevision("wallet:1", "10", container, slot, stack)}
			}
			quality, upgrade, amp, forge := 0, 12, 3, 7
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "10", Action: "configure", Items: []adminops.InventorySelection{selection("equipment", 9, original.Slots["0:9"])}, Options: &adminops.InventoryOptions{QualityMode: &quality, UpgradeLevel: &upgrade, AmplifyType: &amp, ForgingLevel: &forge}, PVFChecksum: source.checksum, Reason: "修复装备", Actor: "测试管理员", PlanToken: "frozen-test-plan", Key: "inventory-config-request"}
			at := time.Now().UTC().Truncate(time.Second)
			mutate := s.v1InventoryMutator(at, "fixed-nonce")
			offline := func(context.Context, string) error { return nil }
			plan, e := adminops.RunInventoryOperation(ctx, u, cmd, false, mutate, s.v1InventoryProject, nil)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(original, load()) {
				t.Fatal("preview mutated inventory")
			}
			if rows, e := u.InventoryManagementLog(ctx, "10", 0, 10); e != nil || len(rows) != 0 {
				t.Fatal("preview persisted receipt", e)
			}
			var wg sync.WaitGroup
			failures := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r, e := adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline)
					expectedJSON, _ := json.Marshal(plan.Changes)
					actualJSON, _ := json.Marshal(r.Changes)
					if e == nil && string(expectedJSON) != string(actualJSON) {
						e = fmt.Errorf("preview differs from commit: %s / %s", expectedJSON, actualJSON)
					}
					failures <- e
				}()
			}
			wg.Wait()
			close(failures)
			for e := range failures {
				if e != nil {
					t.Fatal(e)
				}
			}
			stack := load().Slots["0:9"]
			if stack.Extra["serial"] != "unique-instance" || stack.RawEntry[90] != 0xad || stack.RawEntry[0x57] != 7 || stack.Extra["amplify_value"] != "777" || !itemquality.Valid(itemcfgQualitySeed(stack, entries[0])) {
				t.Fatal(stack)
			}
			if rows, e := u.InventoryManagementLog(ctx, "10", 0, 10); e != nil || len(rows) != 1 {
				t.Fatal(len(rows), e)
			}
			if rows, e := g.Character.(repository.CharacterManagementUnitOfWork).CharacterManagementLog(ctx, "10", 0, 10); e != nil || len(rows) != 0 {
				t.Fatal("inventory polluted quest log", e)
			}
			if _, found, e := adminops.LookupInventoryOperation(ctx, open().Character.(repository.InventoryManagementUnitOfWork), cmd); e != nil || !found {
				t.Fatal("durable replay", found, e)
			}
			cmd.Key = "inventory-stale-request"
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			count := "22"
			cmd.Items = []adminops.InventorySelection{selection("consumables", 65, load().Slots["0:65"])}
			cmd.Options = &adminops.InventoryOptions{Count: &count}
			cmd.Key = "inventory-count-request"
			before := load()
			if _, e = adminops.RunInventoryOperation(ctx, inventoryFailingUOW{u}, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("receipt failure accepted")
			}
			if !reflect.DeepEqual(before, load()) {
				t.Fatal("receipt failure did not rollback")
			}
			var checks atomic.Int32
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, func(context.Context, string) error {
				if checks.Add(1) == 2 {
					return adminops.ErrCharacterOnline
				}
				return nil
			}); !errors.Is(e, adminops.ErrCharacterOnline) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, load()) {
				t.Fatal("late online check did not rollback")
			}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e != nil {
				t.Fatal(e)
			}
			potion := load().Slots["0:65"]
			if potion.Count != 22 || potion.Extra["amount_or_count"] != "22" || binary.LittleEndian.Uint32(potion.RawEntry[6:10]) != 22 {
				t.Fatal(potion)
			}
			cmd.Items[0] = selection("consumables", 65, potion)
			count = "100"
			cmd.Key = "inventory-over-limit"
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("PVF stack limit ignored")
			}
			count = "7"
			cmd.Key = "inventory-count-second"
			cmd.Items = append(cmd.Items, selection("warehouse", 0, load().Warehouse["2:0"]))
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("equipment count accepted")
			}
			if load().Slots["0:65"].Count != 22 {
				t.Fatal("batch partially persisted")
			}
			cmd.Action = "delete"
			cmd.Options = nil
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, nil, s.v1InventoryProject, offline); e != nil {
				t.Fatal(e)
			}
			if len(load().Warehouse) != 0 || len(load().Slots) != 1 {
				t.Fatal("delete did not preserve other slots")
			}
			// Maximum-sized batches retain atomic receipts, and concurrent removals
			// from one observed version cannot both succeed.
			if e = g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "20", AccountID: "wallet:1", Slot: 1, Name: "批量验证", Level: 90, Job: "0"}); e != nil {
				t.Fatal(e)
			}
			many := repository.InventoryRecord{CharacterID: "20", Warehouse: map[string]repository.ItemStack{}}
			for i := 0; i < 200; i++ {
				many.Warehouse[fmt.Sprintf("2:%d", i)] = repository.ItemStack{ItemID: itemcfgTestPotionID, Count: 5, RawEntry: append([]byte(nil), potionRaw...), Extra: map[string]string{"amount_or_count": "5"}}
			}
			if e = g.Inventory.Save(ctx, many); e != nil {
				t.Fatal(e)
			}
			many, _, e = g.Inventory.Load(ctx, "20")
			if e != nil {
				t.Fatal(e)
			}
			batch := cmd
			batch.CharacterID = "20"
			batch.Action = "configure"
			batch.Key = "inventory-max-batch"
			batch.Items = nil
			count = "9"
			batch.Options = &adminops.InventoryOptions{Count: &count}
			for i := 0; i < 200; i++ {
				batch.Items = append(batch.Items, adminops.InventorySelection{Container: "warehouse", Slot: i, Revision: adminops.InventoryRevision("wallet:1", "20", "warehouse", i, many.Warehouse[fmt.Sprintf("2:%d", i)])})
			}
			manyResult, e := adminops.RunInventoryOperation(ctx, u, batch, true, mutate, s.v1InventoryProject, offline)
			if e != nil || len(manyResult.Changes) != 200 {
				t.Fatal("maximum batch", e)
			}
			batch.Action = "delete"
			batch.Options = nil
			batch.Items = batch.Items[:1]
			batch.Items[0].Revision = manyResult.Changes[0].Revision
			outcomes := make(chan error, 2)
			for _, key := range []string{"inventory-race-request1", "inventory-race-request2"} {
				copy := batch
				copy.Key = key
				go func() {
					_, e := adminops.RunInventoryOperation(ctx, u, copy, true, nil, s.v1InventoryProject, offline)
					outcomes <- e
				}()
			}
			wins, conflicts := 0, 0
			for range 2 {
				e := <-outcomes
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
			// Real HTTP adapters: signed preview, tamper/stale-PVF rejection, replay.
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAccountEditFixture(t, w, r, false)
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			token, _ := s.startSession()
			status, out := v1TestCall(t, s, token, "GET", "/api/v1/characters/10/inventory/equipment/9/options")
			if status != 200 {
				t.Fatal(status, out)
			}
			data := out["data"].(map[string]any)
			item := data["item"].(map[string]any)
			body := map[string]any{"accountId": "wallet:1", "action": "configure", "items": []any{map[string]any{"container": "equipment", "slot": 9, "revision": item["revision"]}}, "options": map[string]any{"qualityMode": 0}, "pvfChecksum": source.checksum, "reason": "HTTP修改"}
			encode := func() string { raw, _ := json.Marshal(body); return string(raw) }
			status, out = operationCall(s, token, "characters/10/inventory-operations/preview", encode(), "", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			data = out["data"].(map[string]any)
			body["planToken"] = data["planToken"]
			predicted := data["receipt"].(map[string]any)["changes"]
			originalToken := body["planToken"]
			var identity adminops.InventoryCommand
			if e = json.Unmarshal([]byte(encode()), &identity); e != nil {
				t.Fatal(e)
			}
			identity.CharacterID = "10"
			identity.Actor = "管理员"
			identity.PlanToken = ""
			body["planToken"] = s.v1CursorToken("inventory-plan", adminops.InventoryIdentity(identity), fmt.Sprintf("%d:%s", time.Now().Add(-11*time.Minute).Unix(), strings.Repeat("a", 32)))
			status, _ = operationCall(s, token, "characters/10/inventory-operations", encode(), "inventory-expired-request", nil)
			if status != 409 {
				t.Fatal("expired plan", status)
			}
			body["planToken"] = originalToken
			body["pvfChecksum"] = strings.Repeat("f", 64)
			status, _ = operationCall(s, token, "characters/10/inventory-operations", encode(), "inventory-old-pvf-request", nil)
			if status != 409 {
				t.Fatal("stale PVF", status)
			}
			body["pvfChecksum"] = source.checksum
			body["reason"] = "篡改原因"
			status, _ = operationCall(s, token, "characters/10/inventory-operations", encode(), "inventory-http-request", nil)
			if status != 409 {
				t.Fatal("tampered plan", status)
			}
			body["reason"] = "HTTP修改"
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "inventory-http-request", nil)
			if status != 200 || !reflect.DeepEqual(predicted, out["data"].(map[string]any)["changes"]) {
				t.Fatal(status, out)
			}
			s.deps.PVF = nil
			s.deps.Runtime = nil
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "inventory-http-request", nil)
			if status != 200 {
				t.Fatal("receipt needs active PVF/runtime", status, out)
			}
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/inventory-operations?limit=1")
			if status != 200 || len(out["data"].([]any)) != 1 {
				t.Fatal(status, out)
			}
		})
	}
}

func TestV1CursorKeyAuthenticationIsolation(t *testing.T) {
	a := &Server{cfg: Config{LoginKeySHA256: strings.Repeat("a", 64)}}
	b := &Server{cfg: Config{LoginKeySHA256: strings.Repeat("b", 64)}}
	token := a.v1CursorToken("inventory-plan", "selected-instance", "signed-data")
	if _, e := b.v1CursorAfter(token, "inventory-plan", "selected-instance"); e == nil {
		t.Fatal("key-only instances share a signing key")
	}
	if _, e := (&Server{}).v1CursorAfter(token, "inventory-plan", "selected-instance"); e == nil {
		t.Fatal("empty credential can verify private plan")
	}
}

func TestV1InventoryRejectsMismatchedRawItem(t *testing.T) {
	s := &Server{}
	raw := make([]byte, itemcfgItemListEntrySize)
	binary.LittleEndian.PutUint32(raw[2:6], itemcfgTestPotionID)
	_, err := s.v1InventoryMutator(time.Now(), "test")(context.Background(), adminops.InventorySelection{Container: "equipment", Slot: 9}, repository.ItemStack{ItemID: itemcfgTestWeaponID, Count: 1, RawEntry: raw}, &adminops.InventoryOptions{})
	if err == nil || !strings.Contains(err.Error(), "编号不一致") {
		t.Fatal("mismatched raw item accepted", err)
	}
}
