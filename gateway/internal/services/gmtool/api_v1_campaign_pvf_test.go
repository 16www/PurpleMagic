package gmtool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

// Production sources support concurrent reads. Keep the fixture's read counter
// serialized as well, so the concurrent-delivery checks can run under -race.
type campaignPVFSource struct {
	*itemCacheTestSource
	mu sync.Mutex
}

func (s *campaignPVFSource) ReadText(path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.itemCacheTestSource.ReadText(path)
}

func TestV1CampaignPVFMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		for _, scope := range []testbenefits.GrantScope{testbenefits.ScopeCharacter, testbenefits.ScopeAccount} {
			t.Run(fmt.Sprintf("%d/%s", shards, scope), func(t *testing.T) {
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
				setPVF := func(hash string, durability int, name string, forge bool) {
					docs := readonlyTestSource{"equipment/weapon/test_sword.equ": fmt.Sprintf("[durability]\n%d\n", durability), itemcfgAmplifyItemPVFPath: itemcfgTestAmplifyItemText}
					if forge {
						docs[itemcfgSeparateUpgradePVFPath] = itemcfgTestSeparateUpgradeText
					}
					source := &campaignPVFSource{itemCacheTestSource: newItemCacheTestSource(docs)}
					source.checksum = strings.Repeat(hash, 64)
					s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
					s.pvf = newPVFCache()
					s.items = newItemIndex("", s, func(string, ...any) {})
					entries := itemcfgTestEntries()
					entries[0].Name = name
					s.items.setEntriesLocked(entries)
				}
				setPVF("a", 30, "旧短剑", true)
				for i, id := range []string{"10", "11", "20"} {
					acct := "wallet:1"
					if id == "20" {
						acct = "wallet:2"
					}
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: acct, CharacterID: id, Name: id, Slot: i, Job: "0", Level: 90, CreatedAt: time.Now().UTC().Add(-24 * time.Hour)}))
				}
				token, err := s.startSession()
				check(err)
				encode := func(v any) string { b, e := json.Marshal(v); check(e); return string(b) }
				call := func(path string, body any, key string, want int) map[string]any {
					t.Helper()
					code, out := operationCall(s, token, path, encode(body), key, nil)
					if code != want {
						t.Fatal(path, code, out)
					}
					return out
				}
				c, err := store.CreateCampaignDraftOnce(ctx, "pvf-rule-create-key", scope, true, testbenefits.MailDraft{Title: "版本福利", Body: "保留原规则", Gold: 5, ItemID: "7001", Count: 1, PVFChecksum: strings.Repeat("a", 64), Options: testbenefits.MailOptions{EquipmentQuality: "random", ExpireDays: 7, ForgingLevel: 7}}, s.v1ProjectCampaignPlan)
				check(err)
				// Existing v2 records have no single-script digest or immutable basis.
				c.Reward.Definition = nil
				_, err = s.db.Exec("UPDATE web_accounts_benefit_v2_campaigns SET payload=? WHERE id=?", encode(c), c.ID)
				check(err)
				c, err = store.EnableCampaign(ctx, c.ID, c.Revision, true)
				check(err)
				original := c
				job, err := store.PreviewCampaignOnce(ctx, g, c.ID, "pvf-job-preview-key")
				check(err)
				first := job.Recipients[0]
				prior, err := testbenefits.DeliverProjectedWithReceipt(ctx, g, job, first, s.v1ProjectCampaignDelivery)
				check(err)
				check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: first.CharacterID}))
				setPVF("b", 45, "新短剑", true)
				path := "benefits/" + c.ID
				call("tasks/"+job.ID+"/run", map[string]any{"revision": 0, "confirm": true}, "", 409)
				preview := func(key string) map[string]any {
					return call(path+"/pvf-preview", struct{}{}, key, 200)["data"].(map[string]any)
				}
				command := func(p map[string]any) map[string]any {
					v := p["command"].(map[string]any)
					v["confirm"] = true
					return v
				}
				key := "pvf-review-request-key"
				p := preview(key)
				if p["before"].(map[string]any)["definition"] != nil || !p["canApply"].(bool) {
					t.Fatal(p)
				}
				in := command(p)
				oldToken := in["planToken"]
				in["planToken"] = "tampered"
				call(path+"/pvf-apply", in, key, 409)
				in["planToken"] = oldToken
				var typed testbenefits.PVFReviewCommand
				check(json.Unmarshal([]byte(encode(inWithoutConfirm(in))), &typed))
				in["planToken"] = s.v1CursorToken("benefit-pvf", testbenefits.PVFReviewIdentity(typed, key), strconv.FormatInt(time.Now().Add(-11*time.Minute).Unix(), 10))
				call(path+"/pvf-apply", in, key, 409)
				in["planToken"] = oldToken
				// A changed rule revision and changed PVF both invalidate a signed review.
				c, err = store.EnableCampaign(ctx, c.ID, c.Revision, false)
				check(err)
				call(path+"/pvf-apply", in, key, 409)
				c, err = store.EnableCampaign(ctx, c.ID, c.Revision, true)
				check(err)
				p = preview(key)
				in = command(p)
				setPVF("c", 50, "其他短剑", true)
				call(path+"/pvf-apply", in, key, 409)
				setPVF("b", 46, "新短剑", true)
				call(path+"/pvf-apply", in, key, 409) // same claimed version, different projection
				setPVF("b", 45, "新短剑", false)
				call(path+"/pvf-preview", struct{}{}, key, 422)
				setPVF("b", 45, "新短剑", true)
				entries := itemcfgTestEntries()
				entries[0].Invalid = true
				s.items.setEntriesLocked(entries)
				call(path+"/pvf-preview", struct{}{}, key, 422)
				setPVF("b", 45, "新短剑", true)
				// A history persistence failure must roll back the rule and receipt basis.
				_, err = s.db.Exec("CREATE TRIGGER fail_pvf_review BEFORE INSERT ON web_accounts_benefit_pvf_ops FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected review failure'")
				check(err)
				call(path+"/pvf-apply", in, key, 503)
				after, err := store.Campaign(ctx, c.ID)
				check(err)
				if after.Reward.ReceiptBasis != nil || after.Reward.PVFChecksum != original.Reward.PVFChecksum || after.Revision != c.Revision {
					t.Fatal("failed review changed rule", after)
				}
				_, err = s.db.Exec("DROP TRIGGER fail_pvf_review")
				check(err)
				body := encode(in)
				var wg sync.WaitGroup
				for range 4 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						code, out := operationCall(s, token, path+"/pvf-apply", body, key, nil)
						if code != 200 {
							t.Error(code, out)
						}
					}()
				}
				wg.Wait()
				history, err := store.CampaignPVFHistory(ctx, c.ID, 0, 10)
				check(err)
				if len(history) != 1 {
					t.Fatal("duplicate history", history)
				}
				c, err = store.Campaign(ctx, c.ID)
				check(err)
				if c.Revision != after.Revision+1 || !c.FirstEnabledAt.Equal(original.FirstEnabledAt) || !c.CreatedAt.Equal(original.CreatedAt) || c.RequestFingerprint != original.RequestFingerprint || !c.AutoNewCharacters || !c.Enabled || c.Reward.Attachments[0].Extra["durability"] != "45" {
					t.Fatal("review drift", c)
				}
				// Simulate restart, removed PVF and a lost successful response.
				store, err = testbenefits.NewStore(s.db, "web_accounts")
				check(err)
				s.deps.Benefits = store
				items := s.items
				s.items = nil
				call(path+"/pvf-apply", in, key, 200)
				in["targetChecksum"] = strings.Repeat("c", 64)
				call(path+"/pvf-apply", in, key, 409)
				in["targetChecksum"] = strings.Repeat("b", 64)
				s.items = items
				// The old pending list adopts the new rule; already committed delivery is recovered.
				call("tasks/"+job.ID+"/run", map[string]any{"revision": 0, "confirm": true}, "", 200)
				job, err = store.Job(ctx, job.ID)
				check(err)
				if job.PVFChecksum != c.Reward.PVFChecksum || job.ReceiptBasis == nil {
					t.Fatal(job)
				}
				for _, r := range job.Recipients {
					if r.Error != "" || r.Delivery == nil {
						t.Fatal(r)
					}
					if r.CharacterID == first.CharacterID {
						if !reflect.DeepEqual(*r.Delivery, prior) {
							t.Fatal("old instance changed")
						}
					} else if r.Delivery.Attachments[0].Extra["durability"] != "45" || r.Delivery.PVFChecksum != strings.Repeat("b", 64) {
						t.Fatal("new instance used old projection", r)
					}
				}
				box, _, err := g.Mailbox.Load(ctx, first.CharacterID)
				check(err)
				if len(box.Mails) != 0 {
					t.Fatal("deleted mail redelivered")
				}
				again, err := store.PreviewCampaignOnce(ctx, g, c.ID, "pvf-job-second-key")
				check(err)
				if testbenefits.Summarize(again).Pending != 0 {
					t.Fatal("receipt identity changed")
				}
				// Automatic and manual paths converge on the same permanent receipt.
				check(g.Account.Save(ctx, repository.AccountRecord{AccountID: "wallet:3"}))
				_, err = s.db.Exec("INSERT INTO web_accounts(id,account_id,account) VALUES(3,'wallet:3','新账号')")
				check(err)
				for i, id := range []string{"30", "31"} {
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: "wallet:3", CharacterID: id, Name: id, Slot: i, Job: "0", Level: 1, CreatedAt: time.Now().UTC().Add(time.Second)}))
				}
				batch, err := store.PreviewCampaignOnce(ctx, g, c.ID, "pvf-new-characters-key")
				check(err)
				for _, id := range []string{"30", "31"} {
					character, _, e := g.Character.Load(ctx, id)
					check(e)
					wg.Add(1)
					go func() {
						defer wg.Done()
						if e := store.DeliverCampaignToNewCharacter(ctx, g, c.ID, character, s.v1ProjectCampaignDelivery); e != nil {
							t.Error(e)
						}
					}()
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, e := store.RunVersionProjected(ctx, g, batch.ID, false, 0, s.v1ProjectCampaignDelivery); e != nil {
						t.Error(e)
					}
				}()
				wg.Wait()
				n := 0
				for _, id := range []string{"30", "31"} {
					box, _, e := g.Mailbox.Load(ctx, id)
					check(e)
					n += len(box.Mails)
				}
				want := 2
				if scope == testbenefits.ScopeAccount {
					want = 1
				}
				if n != want {
					t.Fatal("automatic/manual duplicate", n)
				}
				// A second migration retains the very first receipt basis; history uses bounded cursors.
				setPVF("c", 60, "第三版短剑", true)
				secondKey := "pvf-second-review-key"
				in = command(preview(secondKey))
				call(path+"/pvf-apply", in, secondKey, 200)
				c, err = store.Campaign(ctx, c.ID)
				check(err)
				if c.Reward.ReceiptBasis.PVFChecksum != strings.Repeat("a", 64) {
					t.Fatal("basis overwritten")
				}
				again, err = store.PreviewCampaignOnce(ctx, g, c.ID, "pvf-third-job-key")
				check(err)
				if testbenefits.Summarize(again).Pending != 0 {
					t.Fatal("second migration redelivery")
				}
				status, out := v1TestCall(t, s, token, "GET", "/api/v1/"+path+"/pvf-history?limit=1")
				if status != 200 || len(out["data"].([]any)) != 1 {
					t.Fatal(status, out)
				}
				cursor := out["page"].(map[string]any)["nextCursor"].(string)
				if cursor == "" {
					t.Fatal("missing cursor")
				}
				status, out = v1TestCall(t, s, token, "GET", "/api/v1/"+path+"/pvf-history?limit=1&cursor="+cursor)
				if status != 200 || len(out["data"].([]any)) != 1 {
					t.Fatal(status, out)
				}
				if preview("same-pvf-preview-key")["canApply"] != false {
					t.Fatal("same PVF offered mutation")
				}
				// An approved change waits for an already-started automatic delivery.
				check(g.Account.Save(ctx, repository.AccountRecord{AccountID: "wallet:4"}))
				late := repository.CharacterRecord{AccountID: "wallet:4", CharacterID: "40", Name: "40", Job: "0", Level: 1, CreatedAt: time.Now().UTC().Add(time.Second)}
				check(g.Character.Save(ctx, late))
				late, _, err = g.Character.Load(ctx, late.CharacterID)
				check(err)
				setPVF("d", 75, "第四版短剑", true)
				lockKey := "pvf-lock-review-key"
				lockInput := command(preview(lockKey))
				entered, release := make(chan struct{}), make(chan struct{})
				delivered := make(chan error, 1)
				go func() {
					delivered <- store.DeliverCampaignToNewCharacter(ctx, g, c.ID, late, func(j testbenefits.Job, at time.Time) ([]repository.MailAttachment, error) {
						close(entered)
						<-release
						return j.Attachments, nil
					})
				}()
				select {
				case <-entered:
				case e := <-delivered:
					t.Fatalf("delivery never entered projector: %v", e)
				case <-time.After(5 * time.Second):
					close(release)
					t.Fatal("delivery lock timeout")
				}
				applied := make(chan int, 1)
				lockBody := encode(lockInput)
				go func() { code, _ := operationCall(s, token, path+"/pvf-apply", lockBody, lockKey, nil); applied <- code }()
				select {
				case code := <-applied:
					close(release)
					t.Fatalf("review bypassed active delivery: %d", code)
				case <-time.After(150 * time.Millisecond):
				}
				close(release)
				check(<-delivered)
				if code := <-applied; code != 200 {
					t.Fatal("waiting review failed", code)
				}
				check(store.DeliverCampaignToNewCharacter(ctx, g, c.ID, late, s.v1ProjectCampaignDelivery))
				box, _, err = g.Mailbox.Load(ctx, late.CharacterID)
				check(err)
				if len(box.Mails) != 1 {
					t.Fatal("concurrent review redelivery")
				}
				if script := os.Getenv("DNF90GM_BENEFIT_PVF_BROWSER_SCRIPT"); shards == 1 && scope == testbenefits.ScopeCharacter && script != "" {
					setPVF("a", 30, "旧短剑", true)
					webRule, e := store.CreateCampaignDraftOnce(ctx, "pvf-browser-rule-key", scope, false, testbenefits.MailDraft{Title: "网页版本福利", ItemID: "7001", Count: 1, PVFChecksum: strings.Repeat("a", 64), Options: testbenefits.MailOptions{}}, s.v1ProjectCampaignPlan)
					check(e)
					webRule.Reward.Definition = nil
					_, e = s.db.Exec("UPDATE web_accounts_benefit_v2_campaigns SET payload=? WHERE id=?", encode(webRule), webRule.ID)
					check(e)
					setPVF("b", 45, "新短剑", true)
					root, e := filepath.Abs("../../../../gm-web")
					check(e)
					server := httptest.NewServer(s.ManagementHandler(root))
					defer server.Close()
					command := exec.Command("python", script, "--url", server.URL)
					if output, e := command.CombinedOutput(); e != nil {
						t.Fatalf("PVF browser: %v\n%s", e, output)
					} else {
						t.Log(string(output))
					}
				}
			})
		}
	}
}

func inWithoutConfirm(v map[string]any) map[string]any {
	out := map[string]any{}
	for k, x := range v {
		if k != "confirm" {
			out[k] = x
		}
	}
	return out
}
