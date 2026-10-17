package gmtool

import (
	"context"
	"encoding/json"
	"errors"
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

func TestV1CampaignOptionsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		for _, scope := range []testbenefits.GrantScope{testbenefits.ScopeAccount, testbenefits.ScopeCharacter} {
			t.Run(fmt.Sprintf("%d/%s", shards, scope), func(t *testing.T) {
				s, _, open := currencyFixture(t, shards)
				ctx := context.Background()
				g := open()
				check := func(e error) {
					t.Helper()
					if e != nil {
						t.Fatal(e)
					}
				}
				store, e := testbenefits.NewStore(s.db, "web_accounts")
				check(e)
				check(store.EnsureSchema(ctx))
				s.deps.Benefits = store
				source := newItemCacheTestSource(readonlyTestSource{"equipment/weapon/test_sword.equ": "[durability]\n30\n", itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText, itemcfgSeparateUpgradePVFPath: itemcfgTestSeparateUpgradeText})
				s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
				s.pvf = newPVFCache()
				s.items = newItemIndex("", s, func(string, ...any) {})
				s.items.setEntriesLocked(itemcfgTestEntries())
				for i, id := range []string{"10", "11", "20"} {
					account := "wallet:1"
					if id == "20" {
						account = "wallet:2"
					}
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: account, CharacterID: id, Name: id, Slot: i, Job: "0", Level: 90, CreatedAt: time.Now().UTC().Add(-24 * time.Hour)}))
				}
				token, e := s.startSession()
				check(e)
				payload := map[string]any{"scope": scope, "title": "持续福利", "body": "附件参数", "itemId": fmt.Sprint(itemcfgTestWeaponID), "count": 1, "gold": 5, "autoNewCharacters": true, "pvfChecksum": source.checksum, "options": map[string]any{"reinforce": 12, "amplifyType": 3, "forgingLevel": 7, "equipmentQuality": "random", "expireDays": 7}}
				encode := func() string { b, e := json.Marshal(payload); check(e); return string(b) }
				post := func(path, body, key string, want int) map[string]any {
					t.Helper()
					code, out := operationCall(s, token, path, body, key, nil)
					if code != want {
						t.Fatal(path, code, out)
					}
					return out
				}
				bad := encode()
				post("benefits", strings.Replace(bad, `"expireDays":7`, `"expireDays":3651`, 1), "plan-bad-options", 422)
				post("benefits", strings.Replace(bad, source.checksum, strings.Repeat("f", 64), 1), "plan-bad-pvf-key", 409)
				var wg sync.WaitGroup
				ids := make(chan string, 4)
				for range 4 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						code, out := operationCall(s, token, "benefits", bad, "plan-concurrent-key", nil)
						if code != 200 {
							t.Error(code, out)
							return
						}
						ids <- out["data"].(map[string]any)["id"].(string)
					}()
				}
				wg.Wait()
				close(ids)
				id := ""
				for v := range ids {
					if id != "" && id != v {
						t.Fatal("concurrent duplicate")
					}
					id = v
				}
				if id == "" {
					t.Fatal("no rule")
				}
				c, e := store.Campaign(ctx, id)
				check(e)
				if c.Reward.RewardPlan == nil || c.Reward.PVFChecksum != source.checksum || !c.Reward.Attachments[0].ExpireAt.IsZero() || c.Reward.Attachments[0].Extra["quality_seed"] != "" {
					t.Fatal("rule materialized time or random seed", c)
				}
				for _, table := range []string{"web_accounts_benefit_campaigns", "web_accounts_benefit_acct_campaigns"} {
					var n int
					check(s.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id=?", id).Scan(&n))
					if n != 0 {
						t.Fatal("old reader can consume plan")
					}
				}
				// Simulate a long-lived rule. Its creation date cannot shorten the item lifetime.
				c.CreatedAt = c.CreatedAt.Add(-30 * 24 * time.Hour)
				c.Reward.CreatedAt = c.CreatedAt
				b, e := json.Marshal(c)
				check(e)
				_, e = s.db.Exec("UPDATE web_accounts_benefit_v2_campaigns SET payload=? WHERE id=?", b, id)
				check(e)
				items := s.items
				s.items = nil
				post("benefits", bad, "plan-concurrent-key", 200)
				post("benefits", strings.Replace(bad, `"gold":5`, `"gold":6`, 1), "plan-concurrent-key", 409)
				s.items = items
				c, e = store.EnableCampaign(ctx, id, 0, true)
				check(e)
				j, e := store.PreviewCampaignOnce(ctx, g, id, "plan-preview-key")
				check(e)
				want := 3
				if scope == testbenefits.ScopeAccount {
					want = 2
				}
				if len(j.Recipients) != want {
					t.Fatal(j.Recipients)
				}
				var oldJobs int
				check(s.db.QueryRow("SELECT COUNT(*) FROM web_accounts_benefit_jobs WHERE id=?", j.ID).Scan(&oldJobs))
				if oldJobs != 0 {
					t.Fatal("old job reader can consume plan")
				}
				originalChecksum := source.checksum
				source.checksum = strings.Repeat("e", 64)
				post("tasks/"+j.ID+"/run", `{"revision":0,"confirm":true}`, "", 409)
				source.checksum = originalChecksum
				start := time.Now().UTC().Add(-time.Second)
				post("tasks/"+j.ID+"/run", `{"revision":0,"confirm":true}`, "", 200)
				j, e = store.Job(ctx, j.ID)
				check(e)
				for _, r := range j.Recipients {
					if r.Error != "" || r.Delivery == nil || r.Delivery.GrantedAt.Before(start) {
						t.Fatal(r)
					}
					a := r.Delivery.Attachments[0]
					if !a.ExpireAt.Equal(r.Delivery.GrantedAt.Add(7*24*time.Hour)) || a.Extra["quality_seed"] == "" || a.Extra["reinforce"] != "12" || a.Extra["amplify_type"] != "3" || a.Extra["byte_57"] != "7" {
						t.Fatal(a)
					}
					box, _, e := g.Mailbox.Load(ctx, r.CharacterID)
					check(e)
					mail := box.Mails[r.MailID]
					if !mail.CreatedAt.Equal(r.Delivery.GrantedAt) || !reflect.DeepEqual(mail.Attachments, r.Delivery.Attachments) {
						t.Fatal("receipt differs from delivered instance")
					}
					check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: r.CharacterID}))
					// Once committed, recovery does not need the PVF projector or the mail.
					recovered, e := testbenefits.DeliverProjectedWithReceipt(ctx, g, j, r, func(testbenefits.Job, time.Time) ([]repository.MailAttachment, error) {
						return nil, errors.New("must not reproject")
					})
					check(e)
					if !reflect.DeepEqual(recovered, *r.Delivery) {
						t.Fatal("receipt drift")
					}
				}
				store, e = testbenefits.NewStore(s.db, "web_accounts")
				check(e)
				s.deps.Benefits = store
				again, e := store.PreviewCampaignOnce(ctx, g, id, "plan-second-preview")
				check(e)
				for _, r := range again.Recipients {
					if r.Delivery == nil || r.MailID == "" {
						t.Fatal("restart lost receipt")
					}
				}
				// A newly registered account and its two roles exercise the automatic worker.
				check(g.Account.Save(ctx, repository.AccountRecord{AccountID: "wallet:3"}))
				_, e = s.db.Exec("INSERT INTO web_accounts(id,account_id,account) VALUES(3,'wallet:3','新账号')")
				check(e)
				for i, cid := range []string{"30", "31"} {
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: "wallet:3", CharacterID: cid, Name: cid, Slot: i, Job: "0", Level: 1, CreatedAt: time.Now().UTC().Add(time.Second)}))
				}
				source.checksum = strings.Repeat("f", 64)
				if _, e = s.ScanNewCharacters(ctx, 0, 100); e == nil {
					t.Fatal("automatic changed PVF accepted")
				}
				for _, cid := range []string{"30", "31"} {
					box, _, e := g.Mailbox.Load(ctx, cid)
					check(e)
					if len(box.Mails) != 0 {
						t.Fatal("PVF failure left mail")
					}
				}
				c, e = store.EnableCampaign(ctx, id, c.Revision, false)
				check(e)
				_, e = s.ScanNewCharacters(ctx, 0, 100)
				check(e)
				c, e = store.EnableCampaign(ctx, id, c.Revision, true)
				check(e)
				source.checksum = originalChecksum
				_, e = s.ScanNewCharacters(ctx, 0, 100)
				check(e)
				_, e = s.ScanNewCharacters(ctx, 0, 100)
				check(e)
				count := 0
				for _, cid := range []string{"30", "31"} {
					box, _, e := g.Mailbox.Load(ctx, cid)
					check(e)
					count += len(box.Mails)
					for _, m := range box.Mails {
						if !m.Attachments[0].ExpireAt.Equal(m.CreatedAt.Add(7 * 24 * time.Hour)) {
							t.Fatal("auto expiry")
						}
					}
				}
				autoWant := 2
				if scope == testbenefits.ScopeAccount {
					autoWant = 1
				}
				if count != autoWant {
					t.Fatal("automatic dedupe", count)
				}
				// Mixed legacy/v2 pagination retains both formats.
				_, e = store.CreateCampaign(ctx, testbenefits.Job{Title: "旧规则", Attachments: []repository.MailAttachment{{ItemID: 1, Count: 1}}}, "旧物品", false)
				check(e)
				page, next, e := store.CampaignPage(ctx, "", 1)
				check(e)
				if len(page) != 1 || next == "" {
					t.Fatal("mixed first page")
				}
				page, next, e = store.CampaignPage(ctx, next, 1)
				check(e)
				if len(page) != 1 || next != "" {
					t.Fatal("mixed second page")
				}
			})
		}
	}
}
