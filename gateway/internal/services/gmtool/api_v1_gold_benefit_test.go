package gmtool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func TestV1GoldBenefitMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		for _, scope := range []testbenefits.GrantScope{testbenefits.ScopeCharacter, testbenefits.ScopeAccount} {
			t.Run(fmt.Sprintf("%d/%s", shards, scope), func(t *testing.T) {
				s, _, open := currencyFixture(t, shards)
				ctx, g := context.Background(), open()
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
				// No PVF or item catalog is available: gold has no item definition.
				old := time.Now().UTC().Add(-24 * time.Hour)
				for i := 0; i < 36; i++ {
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: fmt.Sprintf("wallet:%d", i/18+1), CharacterID: fmt.Sprint(i + 10), Name: fmt.Sprint(i + 10), Slot: i % 18, CreatedAt: old}))
				}
				check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: "wallet:1", CharacterID: "99", Name: "deleted", Slot: 31, CreatedAt: old, Stats: map[string]int64{"delete_flag": 1}}))
				token, err := s.startSession()
				check(err)
				post := func(path string, body any, key string, want int) map[string]any {
					t.Helper()
					raw, err := json.Marshal(body)
					check(err)
					status, out := operationCall(s, token, path, string(raw), key, nil)
					if status != want {
						t.Fatal(path, status, out)
					}
					data, _ := out["data"].(map[string]any)
					return data
				}
				payload := map[string]any{"scope": scope, "title": "金币福利", "gold": 20000000, "autoNewCharacters": true}
				created := post("benefits", payload, "gold-benefit-create", 200)
				id := created["id"].(string)
				if created["enabled"] != false {
					t.Fatal("created enabled", created)
				}
				if post("benefits", payload, "gold-benefit-create", 200)["id"] != id {
					t.Fatal("create retry changed identity")
				}
				payload["gold"] = 1
				post("benefits", payload, "gold-benefit-create", 409)
				payload["gold"] = 0
				post("benefits", payload, "gold-benefit-empty-1", 422)
				payload["gold"] = 20000000
				payload["count"] = 1
				post("benefits", payload, "gold-benefit-count-1", 422)
				delete(payload, "count")
				payload["options"] = map[string]any{"reinforce": 12}
				post("benefits", payload, "gold-benefit-options", 422)
				delete(payload, "options")
				c, err := store.EnableCampaign(ctx, id, 0, true)
				check(err)
				job, err := store.PreviewCampaignOnce(ctx, g, id, "gold-benefit-preview")
				check(err)
				want := 36
				if scope == testbenefits.ScopeAccount {
					want = 2
				}
				if len(job.Recipients) != want {
					t.Fatal("all existing roles", len(job.Recipients))
				}
				run := func(j testbenefits.Job) testbenefits.Job {
					t.Helper()
					for testbenefits.Summarize(j).Pending > 0 {
						post("tasks/"+j.ID+"/run", map[string]any{"revision": j.Revision, "confirm": true}, "", 200)
						j, err = store.Job(ctx, j.ID)
						check(err)
					}
					if testbenefits.Summarize(j).Failed != 0 {
						t.Fatal(j)
					}
					return j
				}
				job = run(job)
				for _, r := range job.Recipients {
					box, _, err := g.Mailbox.Load(ctx, r.CharacterID)
					check(err)
					mail := box.Mails[r.MailID]
					if mail.Gold != 20000000 || len(mail.Attachments) != 0 {
						t.Fatal(mail)
					}
					check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: r.CharacterID}))
				}
				// Reload the store after mails have been claimed/deleted. The receipts survive.
				store, err = testbenefits.NewStore(s.db, "web_accounts")
				check(err)
				s.deps.Benefits = store
				again, err := store.PreviewCampaignOnce(ctx, g, id, "gold-benefit-preview-2")
				check(err)
				if summary := testbenefits.Summarize(again); summary.Sent != want || summary.Pending != 0 {
					t.Fatal(summary)
				}
				check(g.Account.Save(ctx, repository.AccountRecord{AccountID: "wallet:3"}))
				_, err = s.db.Exec("INSERT INTO web_accounts(id,account_id,account) VALUES(3,'wallet:3','new')")
				check(err)
				for i, cid := range []string{"60", "61", "62"} {
					account := "wallet:3"
					if i == 2 {
						account = "wallet:1"
					}
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: account, CharacterID: cid, Name: cid, Slot: i + 24, CreatedAt: c.FirstEnabledAt.Add(time.Second)}))
				}
				frozen, err := store.PreviewCampaignOnce(ctx, g, id, "gold-benefit-preview")
				check(err)
				if len(frozen.Recipients) != want {
					t.Fatal("preview grew after confirmation")
				}
				c, err = store.EnableCampaign(ctx, id, c.Revision, false)
				check(err)
				_, err = s.ScanNewCharacters(ctx, 0, 100)
				check(err)
				box, _, err := g.Mailbox.Load(ctx, "60")
				check(err)
				if len(box.Mails) != 0 {
					t.Fatal("disabled rule delivered")
				}
				_, err = store.EnableCampaign(ctx, id, c.Revision, true)
				check(err)
				for range 2 {
					_, err = s.ScanNewCharacters(ctx, 0, 100)
					check(err)
				}
				count := 0
				for _, cid := range []string{"60", "61", "62"} {
					box, _, err := g.Mailbox.Load(ctx, cid)
					check(err)
					count += len(box.Mails)
					for _, m := range box.Mails {
						if m.Gold != 20000000 || len(m.Attachments) != 0 {
							t.Fatal(m)
						}
					}
				}
				autoWant := 3
				if scope == testbenefits.ScopeAccount {
					autoWant = 1
				}
				if count != autoWant {
					t.Fatal("automatic delivery count", count)
				}
				// A different rule is a new grant even when its reward is identical.
				other := post("benefits", payload, "gold-benefit-new-campaign", 200)
				otherID := other["id"].(string)
				if otherID == id {
					t.Fatal("independent rule reused")
				}
				_, err = store.EnableCampaign(ctx, otherID, 0, true)
				check(err)
				newJob, err := store.PreviewCampaignOnce(ctx, g, otherID, "gold-benefit-new-preview")
				check(err)
				want += autoWant
				if testbenefits.Summarize(newJob).Pending != want {
					t.Fatal("new campaign not independent")
				}
				run(newJob)
				if script := os.Getenv("DNF90GM_GOLD_BENEFIT_BROWSER_SCRIPT"); script != "" && shards == 1 && scope == testbenefits.ScopeCharacter {
					root, err := filepath.Abs("../../../../gm-web")
					check(err)
					server := httptest.NewServer(s.ManagementHandler(root))
					defer server.Close()
					cmd := exec.Command("python", script, "--url", server.URL, "--expected", fmt.Sprint(want))
					if out, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("gold browser: %v\n%s", err, out)
					} else {
						t.Log(strings.TrimSpace(string(out)))
					}
				}
			})
		}
	}
}
