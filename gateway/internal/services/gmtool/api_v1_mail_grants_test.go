package gmtool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func TestV1SelectedMailMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			store, err := testbenefits.NewStore(s.db, "web_accounts")
			check(err)
			check(store.EnsureSchema(ctx))
			s.deps.Benefits = store
			source := newItemCacheTestSource(readonlyTestSource{itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText, itemcfgSeparateUpgradePVFPath: itemcfgTestSeparateUpgradeText})
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.pvf = newPVFCache()
			s.items = newItemIndex("", s, func(string, ...any) {})
			s.items.setEntriesLocked(itemcfgTestEntries())
			targets := []testbenefits.MailTarget{}
			for i := 0; i < 100; i++ {
				id := fmt.Sprint(i + 10)
				account := fmt.Sprintf("wallet:%d", i%2+1)
				check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: account, Slot: i / 2, Name: "收件" + id, Level: 90, Job: "0", CreatedAt: time.Now().UTC()}))
				targets = append(targets, testbenefits.MailTarget{AccountID: account, CharacterID: id})
			}
			token, err := s.startSession()
			check(err)
			payload := map[string]any{"recipients": targets[:12], "title": "参数发放", "body": "奖励说明", "itemId": fmt.Sprint(itemcfgTestWeaponID), "count": 1, "gold": 50, "pvfChecksum": source.checksum, "options": map[string]any{"reinforce": 12, "amplifyType": 3, "forgingLevel": 7, "equipmentQuality": "random", "expireDays": 7}}
			encode := func() string { raw, e := json.Marshal(payload); check(e); return string(raw) }
			post := func(path, raw, key string, want int) map[string]any {
				t.Helper()
				status, out := operationCall(s, token, path, raw, key, nil)
				if status != want {
					t.Fatal(path, status, out)
				}
				return out
			}
			status, out := v1TestCall(t, s, token, "GET", fmt.Sprintf("/api/v1/items/%d/grant-options", itemcfgTestWeaponID))
			if status != 200 || out["data"].(map[string]any)["equipment"].(map[string]any)["canForge"] != true {
				t.Fatal(status, out)
			}
			// Malformed intent and a changed PVF never create a job.
			originalOptions := payload["options"]
			payload["options"] = map[string]any{"forgingLevel": 100}
			post("mail/preview", encode(), "selected-mail-invalid", 422)
			payload["options"] = originalOptions
			payload["pvfChecksum"] = strings.Repeat("f", 64)
			post("mail/preview", encode(), "selected-mail-old-pvf", 409)
			payload["pvfChecksum"] = source.checksum
			payload["recipients"] = []testbenefits.MailTarget{targets[0], targets[0]}
			post("mail/preview", encode(), "selected-mail-duplicate", 422)
			payload["recipients"] = []testbenefits.MailTarget{{AccountID: "wallet:2", CharacterID: "10"}}
			post("mail/preview", encode(), "selected-mail-wrong-owner", 422)
			rows, _, err := store.JobPage(ctx, "", 100)
			check(err)
			if len(rows) != 0 {
				t.Fatal("invalid preview persisted")
			}
			payload["recipients"] = targets[:12]
			raw := encode()
			// Cross-request serialization freezes random quality and absolute expiry once.
			var wg sync.WaitGroup
			results := make(chan map[string]any, 4)
			codes := make(chan int, 4)
			for range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					code, body := operationCall(s, token, "mail/preview", raw, "selected-mail-concurrent", nil)
					codes <- code
					results <- body
				}()
			}
			wg.Wait()
			close(results)
			close(codes)
			for code := range codes {
				if code != 200 {
					t.Fatal("concurrent preview", code)
				}
			}
			var data map[string]any
			for result := range results {
				if data == nil {
					data = result["data"].(map[string]any)
				} else if !reflect.DeepEqual(data, result["data"]) {
					t.Fatal("rerandomized preview")
				}
			}
			id := data["id"].(string)
			job, err := store.Job(ctx, id)
			check(err)
			a := job.Attachments[0]
			if a.Extra["reinforce"] != "12" || a.Extra["amplify_type"] != "3" || a.Extra["amplify_value"] == "" || a.Extra["byte_57"] != "7" || a.Extra["quality_seed"] == "" || !a.ExpireAt.Equal(job.CreatedAt.Add(7*24*time.Hour)) {
				t.Fatal("projection", a)
			}
			for _, target := range targets[:12] {
				box, _, e := g.Mailbox.Load(ctx, target.CharacterID)
				check(e)
				if len(box.Mails) != 0 {
					t.Fatal("preview sent mail")
				}
			}
			// Persistent intent identity recovers without a live PVF or a second projection.
			s.deps.PVF = nil
			s.items = nil
			replayed := post("mail/preview", raw, "selected-mail-concurrent", 200)["data"]
			if !reflect.DeepEqual(data, replayed) {
				t.Fatal("recovery changed plan")
			}
			post("tasks/"+id+"/run", `{"revision":0,"confirm":true}`, "", 409)
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			s.items = newItemIndex("", s, func(string, ...any) {})
			s.items.setEntriesLocked(itemcfgTestEntries())
			payload["gold"] = 51
			post("mail/preview", encode(), "selected-mail-concurrent", 409)
			payload["gold"] = 50
			// A deleted target fails individually; ten recipients are processed per run.
			deleted, _, err := g.Character.Load(ctx, targets[0].CharacterID)
			check(err)
			if deleted.Stats == nil {
				deleted.Stats = map[string]int64{}
			}
			deleted.Stats["delete_flag"] = 1
			check(g.Character.Save(ctx, deleted))
			first := post("tasks/"+id+"/run", `{"revision":0,"confirm":true}`, "", 200)["data"].(map[string]any)
			if first["sent"] != float64(9) || first["failed"] != float64(1) || first["pending"] != float64(2) {
				t.Fatal(first)
			}
			post("tasks/"+id+"/run", `{"revision":0,"confirm":true}`, "", 409)
			last := post("tasks/"+id+"/run", `{"revision":1,"confirm":true}`, "", 200)["data"].(map[string]any)
			if last["sent"] != float64(11) || last["failed"] != float64(1) {
				t.Fatal(last)
			}
			for _, target := range targets[1:12] {
				box, _, e := g.Mailbox.Load(ctx, target.CharacterID)
				check(e)
				if len(box.Mails) != 1 {
					t.Fatal("mail count", target)
				}
				for _, mail := range box.Mails {
					if mail.Gold != 50 || !reflect.DeepEqual(mail.Attachments[0].Extra, a.Extra) || !mail.Attachments[0].ExpireAt.Equal(a.ExpireAt) {
						t.Fatal("mail projection changed")
					}
				}
			}
			deleted.Stats["delete_flag"] = 0
			check(g.Character.Save(ctx, deleted))
			post("tasks/"+id+"/run", `{"revision":2,"confirm":true,"retry":true}`, "", 200)
			box, _, err := g.Mailbox.Load(ctx, targets[0].CharacterID)
			check(err)
			if len(box.Mails) != 1 {
				t.Fatal("explicit retry")
			}
			// Largest specified list traverses HTTP and binds all 100 targets.
			payload["recipients"] = targets
			if len(encode()) <= 4096 {
				t.Fatal("maximum-list fixture too small")
			}
			max := post("mail/preview", encode(), "selected-mail-maximum", 200)["data"].(map[string]any)
			if max["total"] != float64(100) {
				t.Fatal(max)
			}
			payload["recipients"] = append(targets, targets[0])
			post("mail/preview", encode(), "selected-mail-over-limit", 422)
		})
	}
}
