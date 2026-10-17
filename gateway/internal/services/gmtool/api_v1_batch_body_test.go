package gmtool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestV1BatchHTTPBodyGuards(t *testing.T) {
	s := &Server{cfg: Config{Password: "fixture"}, www: http.NotFoundHandler(), sessions: map[string]time.Time{}}
	token, err := s.startSession()
	if err != nil {
		t.Fatal(err)
	}
	for route, limit := range map[string]int{"mailbox": 32 << 10, "inventory": 64 << 10} {
		for _, suffix := range []string{"", "/preview"} {
			for _, body := range []string{`null`, `{} {}`, `{"unknown":1}`, `{"reason":"` + strings.Repeat("x", limit) + `"}`, `{}` + strings.Repeat(" ", limit)} {
				status, out := operationCall(s, token, "characters/10/"+route+"-operations"+suffix, body, "body-boundary-request", nil)
				if status != 400 || out["error"].(map[string]any)["code"] != "invalid_body" {
					t.Fatal(route, status, out)
				}
			}
		}
	}
}

// Exercise the public adapters, including serialized revisions and signed plans.
// Domain-only maximum-batch tests cannot detect HTTP body truncation.
func TestV1MaximumBatchHTTPMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "批次验证", Job: "0", Level: 90}))
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true})
			}))
			defer runtime.Close()
			var err error
			s.deps.Runtime, err = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(err)
			source := newItemCacheTestSource(readonlyTestSource{})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			s.items.setEntriesLocked(itemcfgTestEntries())
			token, err := s.startSession()
			check(err)
			mailbox := repository.MailboxRecord{CharacterID: "10", Mails: map[string]repository.MailRecord{}}
			mailItems := []adminops.MailSelection{}
			bag := repository.InventoryRecord{CharacterID: "10", Warehouse: map[string]repository.ItemStack{}}
			for i := range 200 {
				bag.Warehouse[fmt.Sprintf("2:%d", i)] = repository.ItemStack{ItemID: itemcfgTestPotionID, Count: 5}
				if i < 100 {
					id := fmt.Sprint(i + 1)
					mailbox.Mails[id] = repository.MailRecord{MailID: id, Title: "已领取", Claimed: true, CreatedAt: time.Now().UTC()}
					mailItems = append(mailItems, adminops.MailSelection{MailID: id})
				}
			}
			check(g.Mailbox.Save(ctx, mailbox))
			check(g.Inventory.Save(ctx, bag))
			bag, _, err = g.Inventory.Load(ctx, "10")
			check(err)
			bagItems := []adminops.InventorySelection{}
			for i := range 200 {
				bagItems = append(bagItems, adminops.InventorySelection{Container: "warehouse", Slot: i, Revision: adminops.InventoryRevision("wallet:1", "10", "warehouse", i, bag.Warehouse[fmt.Sprintf("2:%d", i)])})
			}
			for _, tc := range []struct {
				kind  string
				count int
				body  map[string]any
			}{
				{"mailbox", 100, map[string]any{"accountId": "wallet:1", "reason": "清理已领取邮件", "items": mailItems}},
				{"inventory", 200, map[string]any{"accountId": "wallet:1", "reason": "清理仓库", "action": "delete", "items": bagItems, "pvfChecksum": source.checksum}},
			} {
				t.Run(tc.kind, func(t *testing.T) {
					path := "characters/10/" + tc.kind + "-operations"
					encode := func() string { raw, e := json.Marshal(tc.body); check(e); return string(raw) }
					status, out := operationCall(s, token, path+"/preview", encode(), "", nil)
					if status != 200 {
						t.Fatal("maximum preview", status, out)
					}
					data := out["data"].(map[string]any)
					tc.body["planToken"] = data["planToken"]
					if tc.kind == "mailbox" {
						tc.body["items"] = data["items"]
					}
					raw := encode()
					if len(raw) <= 4096 {
						t.Fatal("fixture does not exercise former body limit")
					}
					status, out = operationCall(s, token, path, raw, "maximum-http-"+tc.kind, nil)
					if status != 200 {
						t.Fatal("maximum commit", len(raw), status, out)
					}
					receipt := out["data"].(map[string]any)
					if len(receipt["changes"].([]any)) != tc.count {
						t.Fatal("partial receipt")
					}
					status, out = operationCall(s, token, path, raw, "maximum-http-"+tc.kind, nil)
					if status != 200 || !reflect.DeepEqual(receipt, out["data"]) {
						t.Fatal("replay", status, out)
					}
				})
			}
			mailbox, _, err = g.Mailbox.Load(ctx, "10")
			check(err)
			for _, m := range mailbox.Mails {
				if !m.Deleted {
					t.Fatal("mail retained", m.MailID)
				}
			}
			bag, _, err = g.Inventory.Load(ctx, "10")
			check(err)
			if len(bag.Warehouse) != 0 {
				t.Fatal("warehouse retained")
			}
		})
	}
}
