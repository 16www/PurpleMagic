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

func TestV1SharedResourcesMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			u := g.AccountInventory.(repository.InventoryManagementUnitOfWork)
			for _, role := range []repository.CharacterRecord{{CharacterID: "10", AccountID: "wallet:1", Slot: 0, Name: "共享一"}, {CharacterID: "11", AccountID: "wallet:1", Slot: 1, Name: "共享二"}, {CharacterID: "20", AccountID: "wallet:2", Slot: 0, Name: "空库存"}} {
				if e := g.Character.Save(ctx, role); e != nil {
					t.Fatal(e)
				}
			}
			texts := readonlyTestSource{}
			entries := []itemEntry{}
			for slot := int16(354); slot <= 365; slot++ {
				id, _ := repository.AccountWarehouseItemForSlot(slot)
				path := fmt.Sprintf("material/resource_%d.stk", id)
				entries = append(entries, itemEntry{ID: uint32(id), Name: fmt.Sprintf("资源%d", id), Kind: "stackable", PVFPath: path, StackLimit: 99})
				texts["stackable/"+path] = "[name]\n`资源`\n[stackable type]\n`[material]` 0\n[stack limit]\n99\n"
			}
			source := newItemCacheTestSource(texts)
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
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
			get := func(path string) map[string]any {
				t.Helper()
				status, out := v1TestCall(t, s, token, "GET", path)
				if status != 200 {
					t.Fatal(status, out)
				}
				return out
			}
			// Twelve fixed definitions paginate even when the account has no inventory row.
			seen := map[int]bool{}
			path := "/api/v1/characters/10/inventory?container=shared&limit=5"
			for {
				out := get(path)
				for _, v := range out["data"].(map[string]any)["items"].([]any) {
					row := v.(map[string]any)
					slot := int(row["slot"].(float64))
					if seen[slot] || row["count"] != "0" || row["present"] != false || row["definitionKnown"] != true {
						t.Fatal(row)
					}
					seen[slot] = true
				}
				cursor, _ := out["page"].(map[string]any)["nextCursor"].(string)
				if cursor == "" {
					break
				}
				path = "/api/v1/characters/10/inventory?container=shared&limit=5&cursor=" + url.QueryEscape(cursor)
			}
			if len(seen) != 12 {
				t.Fatal(seen)
			}
			if _, ok, e := g.AccountInventory.Load(ctx, "wallet:1"); e != nil || ok {
				t.Fatal("read created account inventory", ok, e)
			}
			data := get("/api/v1/characters/10/inventory/shared/355/options")["data"].(map[string]any)
			item := data["item"].(map[string]any)
			if item["present"] != false || item["count"] != "0" || data["quantity"].(map[string]any)["min"] != "0" {
				t.Fatal(data)
			}
			body := map[string]any{"accountId": "wallet:1", "action": "configure", "items": []any{map[string]any{"container": "shared", "slot": 355, "revision": item["revision"]}}, "options": map[string]any{"count": "125"}, "pvfChecksum": source.checksum, "reason": "从零补入白色晶块"}
			encode := func() string { raw, _ := json.Marshal(body); return string(raw) }
			status, out := operationCall(s, token, "characters/10/inventory-operations/preview", encode(), "", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			plan := out["data"].(map[string]any)
			body["planToken"] = plan["planToken"]
			if _, ok, e := g.AccountInventory.Load(ctx, "wallet:1"); e != nil || ok {
				t.Fatal("preview created row", ok, e)
			}
			change := plan["receipt"].(map[string]any)["changes"].([]any)[0].(map[string]any)
			if change["before"].(map[string]any)["present"] != false || change["after"].(map[string]any)["count"] != "125" {
				t.Fatal(change)
			}
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "shared-create-http-1", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			firstReceipt := out["data"]
			load := func() repository.AccountInventoryRecord {
				v, ok, e := g.AccountInventory.Load(ctx, "wallet:1")
				if e != nil || !ok {
					t.Fatal(ok, e)
				}
				return v
			}
			stack := load().Slots["0:355"]
			if stack.Count != 125 || len(stack.RawEntry) != 119 || binary.LittleEndian.Uint16(stack.RawEntry[:2]) != 355 || binary.LittleEndian.Uint32(stack.RawEntry[2:6]) != 3034 || binary.LittleEndian.Uint32(stack.RawEntry[6:10]) != 125 || binary.LittleEndian.Uint32(stack.RawEntry[14:18]) != 3034 || stack.Extra["stackable_type"] != "[material]" {
				t.Fatal(stack)
			}
			if _, ok, e := g.Inventory.Load(ctx, "10"); e != nil || ok {
				t.Fatal("created personal inventory", ok, e)
			}
			// Clear through the same count editor, then recover the original committed receipt.
			mutate := s.v1InventoryMutator(time.Now().UTC().Truncate(time.Second), "fixed")
			offline := func(context.Context, string) error { return nil }
			zero := "0"
			cmd := adminops.InventoryCommand{AccountID: "wallet:1", CharacterID: "11", Action: "configure", Items: []adminops.InventorySelection{{Container: "shared", Slot: 355, Revision: adminops.InventoryRevision("wallet:1", "11", "shared", 355, stack)}}, Options: &adminops.InventoryOptions{Count: &zero}, PVFChecksum: source.checksum, Reason: "清空", Actor: "管理员", PlanToken: "unit-plan", Key: "shared-clear-request"}
			cleared, e := adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline)
			if e != nil {
				t.Fatal(e)
			}
			if _, ok := load().Slots["0:355"]; ok || cleared.Changes[0].After.Present {
				t.Fatal("zero retained a row", cleared)
			}
			status, out = operationCall(s, token, "characters/10/inventory-operations", encode(), "shared-create-http-1", nil)
			if status != 200 || !reflect.DeepEqual(firstReceipt, out["data"]) {
				t.Fatal("replay after clear", status, out)
			}
			if _, ok := load().Slots["0:355"]; ok {
				t.Fatal("replay granted again")
			}
			// A persisted zero and an absent row have different versions.
			emptyRevision := cleared.Changes[0].Revision
			v := load()
			if v.Slots == nil {
				v.Slots = map[string]repository.ItemStack{}
			}
			v.Slots["0:355"] = repository.ItemStack{ItemID: 3034, Count: 0}
			if e = g.AccountInventory.Save(ctx, v); e != nil {
				t.Fatal(e)
			}
			count := "9"
			cmd.Key = "shared-stale-empty"
			cmd.Items[0].Revision = emptyRevision
			cmd.Options = &adminops.InventoryOptions{Count: &count}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal("zero row confused with absence", e)
			}
			cmd.Items[0].Revision = adminops.InventoryRevision("wallet:1", "11", "shared", 355, load().Slots["0:355"])
			cmd.Key = "shared-zero-row-fill"
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e != nil {
				t.Fatal(e)
			}
			// Concurrent creation from two roles cannot both consume one empty version.
			cmd.Items[0] = adminops.InventorySelection{Container: "shared", Slot: 360, Revision: adminops.InventorySlotRevision("wallet:1", "10", "shared", 360, repository.ItemStack{}, false)}
			results := make(chan error, 2)
			for _, id := range []string{"10", "11"} {
				copy := cmd
				copy.CharacterID = id
				copy.Key = "shared-create-race-" + id
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
			// The very first account inventory insert rolls back if its receipt fails.
			cmd.AccountID = "wallet:2"
			cmd.CharacterID = "20"
			cmd.Key = "shared-first-row-rollback"
			cmd.Items[0].Revision = adminops.InventorySlotRevision("wallet:2", "20", "shared", 360, repository.ItemStack{}, false)
			if _, e = adminops.RunInventoryOperation(ctx, inventoryFailingUOW{u}, cmd, true, mutate, s.v1InventoryProject, offline); e == nil {
				t.Fatal("receipt failure accepted")
			}
			if _, ok, e := g.AccountInventory.Load(ctx, "wallet:2"); e != nil || ok {
				t.Fatal("first row not rolled back", ok, e)
			}
			// Bad quantities and missing PVF metadata cannot create inventory or receipts.
			for _, tc := range []struct {
				name, count, document string
				missing               bool
			}{
				{name: "negative", count: "-1"},
				{name: "overflow", count: "2147483648"},
				{name: "missing-pvf", count: "9", missing: true},
				{name: "incomplete-pvf", count: "9", document: "[name]\n`资源`\n"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					badCmd := cmd
					badCmd.Key = "shared-rejected-" + tc.name
					badCmd.Options = &adminops.InventoryOptions{Count: &tc.count}
					oldSource := source
					defer func() { source = oldSource; s.pvf = newPVFCache() }()
					if tc.missing || tc.document != "" {
						documents := readonlyTestSource{}
						for path, text := range texts {
							documents[path] = text
						}
						id, _ := repository.AccountWarehouseItemForSlot(360)
						path := fmt.Sprintf("stackable/material/resource_%d.stk", id)
						if tc.missing {
							delete(documents, path)
						} else {
							documents[path] = tc.document
						}
						source = newItemCacheTestSource(documents)
						s.pvf = newPVFCache()
						badCmd.PVFChecksum = source.checksum
					}
					if _, e := adminops.RunInventoryOperation(ctx, u, badCmd, true, mutate, s.v1InventoryProject, offline); e == nil {
						t.Fatal("invalid resource accepted")
					}
					if _, ok, e := g.AccountInventory.Load(ctx, "wallet:2"); e != nil || ok {
						t.Fatal("rejection created inventory", ok, e)
					}
					if _, found, e := adminops.LookupInventoryOperation(ctx, u, badCmd); e != nil || found {
						t.Fatal("rejection wrote receipt", found, e)
					}
				})
			}
			// A later invalid selection leaves the earlier valid selection uncommitted.
			badBatch := cmd
			badBatch.Key = "shared-partial-batch-refused"
			badBatch.Items = append([]adminops.InventorySelection(nil), cmd.Items...)
			badBatch.Items = append(badBatch.Items, adminops.InventorySelection{Container: "shared", Slot: 361, Revision: strings.Repeat("0", 64)})
			if _, e = adminops.RunInventoryOperation(ctx, u, badBatch, true, mutate, s.v1InventoryProject, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			if _, ok, e := g.AccountInventory.Load(ctx, "wallet:2"); e != nil || ok {
				t.Fatal("partial batch persisted", ok, e)
			}
			// All twelve resources can be initialized atomically, using canonical IDs.
			cmd.Key = "shared-all-slots-init"
			cmd.Items = nil
			for slot := 354; slot <= 365; slot++ {
				cmd.Items = append(cmd.Items, adminops.InventorySelection{Container: "shared", Slot: slot, Revision: adminops.InventorySlotRevision("wallet:2", "20", "shared", slot, repository.ItemStack{}, false)})
			}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); e != nil {
				t.Fatal(e)
			}
			all, ok, e := g.AccountInventory.Load(ctx, "wallet:2")
			if e != nil || !ok || len(all.Slots) != 12 {
				t.Fatal(ok, e, all)
			}
			// Invalid stored rows remain visible and cannot be overwritten as if empty.
			v = load()
			v.Slots["0:356"] = repository.ItemStack{ItemID: 0, Count: 4}
			if e = g.AccountInventory.Save(ctx, v); e != nil {
				t.Fatal(e)
			}
			bad := get("/api/v1/characters/10/inventory/shared/356/options")["data"].(map[string]any)["item"].(map[string]any)
			if bad["present"] != true || bad["itemId"] != "0" {
				t.Fatal("corrupt slot hidden", bad)
			}
			cmd.AccountID = "wallet:1"
			cmd.CharacterID = "10"
			cmd.Key = "shared-corrupt-refused"
			cmd.Items = []adminops.InventorySelection{{Container: "shared", Slot: 356, Revision: bad["revision"].(string)}}
			if _, e = adminops.RunInventoryOperation(ctx, u, cmd, true, mutate, s.v1InventoryProject, offline); !errors.Is(e, adminops.ErrInventoryInput) {
				t.Fatal(e)
			}
		})
	}
}
