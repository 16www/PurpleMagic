package gmtool

import (
	"context"
	"dnf90.local/gateway/internal/servicehttp"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func TestV1FeedbackMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			g := open()
			ctx := context.Background()
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
			var external *Server
			var peer *servicehttp.Client
			if externalFeedbackEnabled() {
				external, peer, _ = externalTestServices(t, s)
			}
			for i := 0; i < 4; i++ {
				check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: fmt.Sprintf("wallet:%d", i/2+1), CharacterID: fmt.Sprint(i + 10), Name: fmt.Sprintf("角色%d", i+10), Slot: i % 2, CreatedAt: time.Now().UTC().Add(-time.Hour)}))
			}
			token, err := s.startSession()
			check(err)
			post := func(path string, input any, key string, want int) map[string]any {
				t.Helper()
				raw, err := json.Marshal(input)
				check(err)
				status, out := operationCall(s, token, path, string(raw), key, nil)
				if status != want {
					t.Fatal(path, status, out)
				}
				data, _ := out["data"].(map[string]any)
				return data
			}
			payload := map[string]any{"accountIds": []string{"wallet:2", "wallet:1"}, "recipients": []testbenefits.MailTarget{{AccountID: "wallet:1", CharacterID: "10"}}, "title": "批量账号与角色", "itemId": fmt.Sprint(itemcfgTestWeaponID), "count": 1, "pvfChecksum": source.checksum, "options": map[string]any{}}
			job := post("mail/preview", payload, "batch-account-mail-1", 200)
			if job["total"] != float64(4) {
				t.Fatal("overlap duplicated", job)
			}
			check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: "wallet:1", CharacterID: "14", Name: "后建角色", Slot: 3, CreatedAt: time.Now().UTC()}))
			payload["accountIds"] = []string{"wallet:1", "wallet:2"}
			again := post("mail/preview", payload, "batch-account-mail-1", 200)
			if again["total"] != float64(4) || again["id"] != job["id"] {
				t.Fatal("retry changed frozen roster", again)
			}
			job = post("tasks/"+job["id"].(string)+"/run", map[string]any{"revision": job["revision"], "confirm": true}, "", 200)
			if job["sent"] != float64(4) || job["failed"] != float64(0) {
				t.Fatal(job)
			}
			for _, id := range []string{"10", "11", "12", "13"} {
				box, _, err := g.Mailbox.Load(ctx, id)
				check(err)
				if len(box.Mails) != 1 {
					t.Fatal("wrong mail count", id, box)
				}
			}
			payload["title"] = "different"
			post("mail/preview", payload, "batch-account-mail-1", 409)
			// New v1 activation automatically covers existing roles without a preview
			// job or a browser keeping tasks/{id}/run alive.
			for _, future := range []bool{false, true} {
				rule := post("benefits", map[string]any{"title": fmt.Sprint("自动福利", future), "gold": 7, "scope": "character", "autoNewCharacters": future}, fmt.Sprint("auto-existing-rule-", future), 200)
				rule = post("benefits/"+rule["id"].(string)+"/state", map[string]any{"revision": rule["revision"], "enabled": true, "confirm": true}, "", 200)
				if rule["autoExistingCharacters"] != true {
					t.Fatal("activation not automatic", rule)
				}
				for range 2 {
					_, err = s.ScanNewCharacters(ctx, 0, 100)
					check(err)
				}
				for _, id := range []string{"10", "11", "12", "13", "14"} {
					box, _, err := g.Mailbox.Load(ctx, id)
					check(err)
					count := 0
					for _, mail := range box.Mails {
						if mail.Title == fmt.Sprint("自动福利", future) {
							count++
						}
					}
					if count != 1 {
						t.Fatalf("existing character %s received rule %v %d times", id, future, count)
					}
				}
				stored, err := store.Campaign(ctx, rule["id"].(string))
				check(err)
				newRole := repository.CharacterRecord{AccountID: "wallet:2", CharacterID: fmt.Sprint(20 + map[bool]int{false: 0, true: 1}[future]), Name: "未来角色", Slot: 5 + map[bool]int{false: 0, true: 1}[future], CreatedAt: stored.FirstEnabledAt.Add(time.Second)}
				check(g.Character.Save(ctx, newRole))
				_, err = s.ScanNewCharacters(ctx, 0, 100)
				check(err)
				box, _, err := g.Mailbox.Load(ctx, newRole.CharacterID)
				check(err)
				if len(box.Mails) != map[bool]int{false: 0, true: 1}[future] {
					t.Fatal("future policy", future, box)
				}
				// Disable/re-enable retains the receipt identity, even after mail deletion.
				check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: "10"}))
				rule = post("benefits/"+rule["id"].(string)+"/state", map[string]any{"revision": rule["revision"], "enabled": false, "confirm": true}, "", 200)
				post("benefits/"+rule["id"].(string)+"/state", map[string]any{"revision": rule["revision"], "enabled": true, "confirm": true}, "", 200)
				_, err = s.ScanNewCharacters(ctx, 0, 100)
				check(err)
				box, _, err = g.Mailbox.Load(ctx, "10")
				check(err)
				if len(box.Mails) != 0 {
					t.Fatal("receipt lost after re-enable")
				}
			}
			// A legacy rule first opts into existing characters at the new
			// activation time, while keeping its original identity and future cutoff.
			legacy, err := store.CreateCampaign(ctx, testbenefits.Job{Scope: testbenefits.ScopeCharacter, Gold: 1, Title: "legacy"}, "", false)
			check(err)
			legacy, err = store.EnableCampaign(ctx, legacy.ID, legacy.Revision, true)
			check(err)
			between := repository.CharacterRecord{CreatedAt: time.Now().UTC()}
			if testbenefits.EligibleNewCharacter(legacy, between) {
				t.Fatal("upgrade silently enrolled legacy characters")
			}
			migrated, err := store.EnableAutomaticCampaign(ctx, legacy.ID, legacy.Revision, true)
			check(err)
			if !migrated.FirstEnabledAt.Equal(legacy.FirstEnabledAt) || !testbenefits.EligibleNewCharacter(migrated, between) {
				t.Fatal("legacy enrollment cutoff or identity lost", migrated)
			}
			paused, err := store.EnableAutomaticCampaign(ctx, migrated.ID, migrated.Revision, false)
			check(err)
			resumed, err := store.EnableAutomaticCampaign(ctx, paused.ID, paused.Revision, true)
			check(err)
			if !resumed.ExistingThrough.Equal(migrated.ExistingThrough) || testbenefits.EligibleNewCharacter(resumed, repository.CharacterRecord{CreatedAt: migrated.ExistingThrough.Add(time.Second)}) {
				t.Fatal("resume expanded existing-only audience", resumed)
			}
			if script := os.Getenv("DNF90GM_FEEDBACK_BROWSER_SCRIPT"); script != "" && shards == 1 {
				for i := 0; i < 12; i++ {
					check(g.Character.Save(ctx, repository.CharacterRecord{AccountID: "wallet:1", CharacterID: fmt.Sprint(30 + i), Name: fmt.Sprintf("批量角色%d", i), Slot: 4 + i, CreatedAt: time.Now().UTC().Add(-time.Hour)}))
				}
				root, err := filepath.Abs("../../../../gm-web")
				check(err)
				var queue *ExternalQueue
				var handler http.Handler = s.ManagementHandler(root)
				if external != nil {
					queue, err = OpenExternalQueue(filepath.Join(t.TempDir(), "queue.json"), peer)
					check(err)
					handler = external.ExternalHandler(root, "", peer, queue)
				}
				server := httptest.NewServer(handler)
				defer server.Close()
				worker, cancel := context.WithCancel(ctx)
				queueDone := make(chan struct{})
				go func() {
					defer close(queueDone)
					if queue != nil {
						queue.Run(worker, func(string, ...any) {})
					}
				}()
				done := make(chan struct{})
				defer func() { cancel(); <-done; <-queueDone }()
				go func() {
					defer close(done)
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-worker.Done():
							return
						case <-ticker.C:
							if peer != nil {
								var out struct{ Cursor int64 }
								peer.JSON(worker, "/api/internal/gateway/v1/scan", map[string]any{"cursor": 0, "limit": 5}, &out)
							} else {
								s.ScanNewCharacters(worker, 0, 100)
							}
						}
					}
				}()
				cmd := exec.Command("python", script, "--url", server.URL, "--item", fmt.Sprint(itemcfgTestWeaponID))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("feedback browser: %v\n%s", err, out)
				} else {
					t.Log(string(out))
				}
			}
		})
	}
}
