package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type cleanupFailStore struct {
	repository.MailboxManagementRepository
	repository.MailboxCleanupStore
	mode string
}

func (s cleanupFailStore) SaveCleanup(ctx context.Context, id string, raw []byte) error {
	if e := s.MailboxCleanupStore.SaveCleanup(ctx, id, raw); e != nil {
		return e
	}
	if s.mode == "job" {
		return errors.New("injected checkpoint failure")
	}
	return nil
}
func (s cleanupFailStore) AppendCleanupItems(ctx context.Context, id string, v []repository.MailboxCleanupItem) error {
	if e := s.MailboxCleanupStore.AppendCleanupItems(ctx, id, v); e != nil {
		return e
	}
	if s.mode == "snapshot" {
		return errors.New("injected snapshot failure")
	}
	return nil
}

type cleanupFailUOW struct {
	repository.MailboxManagementUnitOfWork
	mode string
}

func (s cleanupFailUOW) WithinMailboxManagement(ctx context.Context, a, c string, fn func(repository.MailboxManagementRepository, repository.ManagementReceiptStore) error) error {
	return s.MailboxManagementUnitOfWork.WithinMailboxManagement(ctx, a, c, func(m repository.MailboxManagementRepository, r repository.ManagementReceiptStore) error {
		if s.mode == "receipt" {
			r = failingReceipts{r}
		}
		return fn(cleanupFailStore{m, m.(repository.MailboxCleanupStore), s.mode}, r)
	})
}

func TestV1MailboxCleanupMySQL(t *testing.T) {
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
			for i, id := range []string{"10", "20"} {
				account := "wallet:1"
				if id == "20" {
					account = "wallet:2"
				}
				check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: account, Name: "清理" + id, Slot: i}))
			}
			at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			mails := repository.MailboxRecord{CharacterID: "10", Mails: map[string]repository.MailRecord{}}
			for i := 1; i <= 210; i++ {
				id := fmt.Sprintf("mail-%04d", i)
				title := "目标邮件"
				if i > 205 {
					title = "其他邮件"
				}
				m := repository.MailRecord{MailID: id, Title: title, SenderName: "运营%团队", Body: "原正文", Claimed: i%2 == 0, CreatedAt: at.Add(time.Duration(i) * time.Minute), Metadata: map[string]string{"delivery_receipt": "preserve"}}
				if i == 1 {
					m.Gold = 50
					m.Attachments = []repository.MailAttachment{{ItemID: 123, Count: 2, Bind: true, Extra: map[string]string{"keep": "original"}}}
				}
				mails.Mails[id] = m
			}
			check(g.Mailbox.Save(ctx, mails))
			before, _, e := g.Mailbox.Load(ctx, "10")
			check(e)
			u := g.Character.(repository.MailboxManagementUnitOfWork)
			read := func(id string) adminops.MailboxCleanup {
				t.Helper()
				j, _, e := adminops.ReadMailboxCleanup(ctx, u, "wallet:1", "10", id, 0, 0)
				check(e)
				return j
			}
			unchanged := func() {
				t.Helper()
				now, _, e := open().Mailbox.Load(ctx, "10")
				check(e)
				if !reflect.DeepEqual(before, now) {
					t.Fatal("preview or failed step mutated mailbox")
				}
				rows, e := u.MailboxManagementLog(ctx, "10", 0, 100)
				check(e)
				if len(rows) != 0 {
					t.Fatal("unexpected delete receipt")
				}
			}
			offline := func(context.Context, string) error { return nil }
			confirm := func(adminops.MailboxCleanup) error { return nil }
			advance := func(j adminops.MailboxCleanup, mode string) adminops.MailboxCleanup {
				t.Helper()
				v, e := adminops.AdvanceMailboxCleanup(ctx, u, "wallet:1", "10", j.ID, mode, "tester", j.Revision, confirm, offline)
				check(e)
				return v
			}
			cmd := adminops.MailboxCleanupCommand{AccountID: "wallet:1", CharacterID: "10", Filter: repository.MailboxFilter{Query: "目标", Sender: "%"}, Reason: "跨页清理", Actor: "tester", Key: "cleanup-main-request"}
			for _, mode := range []string{"snapshot", "job"} {
				_, e := adminops.CreateMailboxCleanup(ctx, cleanupFailUOW{u, mode}, cmd)
				if e == nil {
					t.Fatal(mode)
				}
				_, _, e = adminops.ReadMailboxCleanup(ctx, u, "wallet:1", "10", cmd.Key, 0, 1)
				if !errors.Is(e, adminops.ErrCleanupMissing) {
					t.Fatal("failed preview leaked", e)
				}
				unchanged()
			}
			job, e := adminops.CreateMailboxCleanup(ctx, u, cmd)
			check(e)
			if job.Status != "building" || job.Total != 100 {
				t.Fatal(job)
			}
			replayed, e := adminops.CreateMailboxCleanup(ctx, u, cmd)
			check(e)
			if !reflect.DeepEqual(job, replayed) {
				t.Fatal("creation replay")
			}
			bad := cmd
			bad.Reason = "different"
			_, e = adminops.CreateMailboxCleanup(ctx, u, bad)
			if !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			old := job
			job = advance(job, "prepare")
			if job.Total != 200 {
				t.Fatal(job)
			}
			again := advance(old, "prepare")
			if !reflect.DeepEqual(job, again) {
				t.Fatal("prepare repeated")
			}
			job = advance(job, "prepare")
			if job.Status != "ready" || job.Total != 205 || job.Unclaimed != 1 || job.Gold != "50" || job.Attachments != 1 {
				t.Fatal(job)
			}
			_, e = adminops.AdvanceMailboxCleanup(ctx, u, "wallet:1", "10", job.ID, "run", "tester", job.Revision, confirm, offline)
			if !errors.Is(e, adminops.ErrUnclaimedMail) {
				t.Fatal(e)
			}
			unchanged()
			job = advance(job, "cancel")
			if job.Status != "cancelled" {
				t.Fatal(job)
			}
			cmd.Key = "cleanup-confirmed-request"
			cmd.DiscardUnclaimed = true
			job, e = adminops.CreateMailboxCleanup(ctx, u, cmd)
			check(e)
			for job.Status == "building" {
				job = advance(job, "prepare")
			}
			unchanged()
			// Filter predicates are pushed down before the bounded keyset page.
			token, e := s.startSession()
			check(e)
			get := func(path string) (int, map[string]any) {
				return v1TestCall(t, s, token, "GET", "/api/v1/characters/10/"+path)
			}
			status, body := get("mailbox?sender=%25&claimed=claimed&limit=3")
			if status != 200 || len(body["data"].([]any)) != 3 {
				t.Fatal(status, body)
			}
			cursor := body["page"].(map[string]any)["nextCursor"].(string)
			status, _ = get("mailbox?sender=%25&claimed=claimed&limit=3&cursor=" + url.QueryEscape(cursor))
			if status != 200 {
				t.Fatal(status)
			}
			status, _ = get("mailbox?sender=other&claimed=claimed&limit=3&cursor=" + url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal("filter cursor reused", status)
			}
			status, body = get("mailbox?from=2026-10-01T08:30:00%2B08:00&to=2026-10-01T00:40:00Z&limit=100")
			if status != 200 || len(body["data"].([]any)) != 10 {
				t.Fatal(status, body)
			}
			for _, q := range []string{"from=bad", "claimed=other", "to=2026-10-01T00:00:00Z&from=2026-10-02T00:00:00Z", "sender=a&sender=b"} {
				status, _ = get("mailbox?" + q)
				if status != 400 {
					t.Fatal(q, status)
				}
			}
			status, body = get("mailbox?sender=" + url.QueryEscape("' OR 1=1 --"))
			if status != 200 || len(body["data"].([]any)) != 0 {
				t.Fatal(status, body)
			}
			for _, mode := range []string{"job", "receipt", "late-online", "runtime", "confirmation"} {
				worker := u
				guard := offline
				approval := confirm
				calls := 0
				switch mode {
				case "job", "receipt":
					worker = cleanupFailUOW{u, mode}
				case "runtime":
					guard = nil
				case "confirmation":
					approval = func(adminops.MailboxCleanup) error { return adminops.ErrConflict }
				case "late-online":
					guard = func(context.Context, string) error {
						calls++
						if calls == 2 {
							return adminops.ErrCharacterOnline
						}
						return nil
					}
				}
				_, e = adminops.AdvanceMailboxCleanup(ctx, worker, "wallet:1", "10", job.ID, "run", "tester", job.Revision, approval, guard)
				if e == nil {
					t.Fatal(mode, "accepted")
				}
				unchanged()
				if !reflect.DeepEqual(job, read(job.ID)) {
					t.Fatal("checkpoint moved on failure")
				}
			}
			var wg sync.WaitGroup
			results := make(chan adminops.MailboxCleanup, 4)
			errs := make(chan error, 4)
			for range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					v, e := adminops.AdvanceMailboxCleanup(ctx, u, "wallet:1", "10", job.ID, "run", "tester", job.Revision, confirm, offline)
					results <- v
					errs <- e
				}()
			}
			wg.Wait()
			close(results)
			close(errs)
			for e := range errs {
				check(e)
			}
			for v := range results {
				if v.Processed != 100 {
					t.Fatal("batch repeated", v)
				}
			}
			old = job
			job = read(job.ID)
			again, e = adminops.AdvanceMailboxCleanup(ctx, u, "wallet:1", "10", old.ID, "run", "tester", old.Revision, nil, nil)
			check(e)
			if !reflect.DeepEqual(job, again) {
				t.Fatal("lost response replay")
			}
			u = open().Character.(repository.MailboxManagementUnitOfWork)
			job = advance(job, "run")
			if job.Processed != 200 {
				t.Fatal(job)
			}
			job = advance(job, "run")
			if job.Status != "completed" || job.Processed != 205 {
				t.Fatal(job)
			}
			after, _, e := g.Mailbox.Load(ctx, "10")
			check(e)
			for id, m := range before.Mails {
				want := m
				if strings.HasPrefix(m.Title, "目标") {
					want.Deleted = true
					want.Read = true
					want.Gold = 0
					want.Attachments = nil
				}
				if !reflect.DeepEqual(after.Mails[id], want) {
					t.Fatal("unexpected mail change", id)
				}
			}
			rows, e := u.MailboxManagementLog(ctx, "10", 0, 100)
			check(e)
			if len(rows) != 3 {
				t.Fatal(len(rows))
			}
			for _, row := range rows {
				v, e := adminops.DecodeMailboxReceipt(row.Payload)
				check(e)
				if v.CleanupID != job.ID {
					t.Fatal(v)
				}
			}
			_, page, e := adminops.ReadMailboxCleanup(ctx, u, "wallet:1", "10", job.ID, 100, 100)
			check(e)
			if len(page) != 100 || page[0].Ordinal != 101 {
				t.Fatal(len(page))
			}
			_, _, e = adminops.ReadMailboxCleanup(ctx, u, "wallet:2", "20", job.ID, 0, 1)
			if !errors.Is(e, adminops.ErrCleanupMissing) {
				t.Fatal("cross character job", e)
			}
			// A delivery after the preview is preserved and stops the old job.
			cmd.Key = "cleanup-new-mail-conflict"
			cmd.Filter.Query = "其他"
			next, e := adminops.CreateMailboxCleanup(ctx, u, cmd)
			check(e)
			var delivered string
			check(g.MailboxAssets.WithinMailboxAssets(ctx, "10", "10", func(_ repository.CharacterRepository, _ repository.InventoryRepository, m repository.MailboxRepository) error {
				var e error
				delivered, e = repository.AppendSystemMail(ctx, m, repository.SystemMailDelivery{RecipientCharacterID: "10", Title: "其他新邮件", Source: "cleanup-fixture", Gold: 1})
				return e
			}))
			next = advance(next, "run")
			if next.Status != "conflict" || next.Processed != 0 {
				t.Fatal(next)
			}
			mailbox, _, e := g.Mailbox.Load(ctx, "10")
			check(e)
			if mailbox.Mails[delivered].Deleted {
				t.Fatal("new delivery deleted")
			}
			// HTTP confirmation, recovery, frozen pages and record cursor ownership.
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true})
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(e)
			payload := map[string]any{"accountId": "wallet:1", "filter": map[string]any{"q": "其他"}, "reason": "HTTP跨页清理", "discardUnclaimed": true}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, "characters/10/mailbox-cleanups"+path, string(raw), key, headers)
			}
			status, _ = call("", "cleanup-http-create-1", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call("", "cleanup-http-create-1", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			data := body["data"].(map[string]any)
			j := data["job"].(map[string]any)
			id := j["id"].(string)
			payload = map[string]any{"revision": j["revision"], "confirm": true, "planToken": "tampered"}
			status, _ = call("/"+id+"/run", "", nil)
			if status != 409 {
				t.Fatal(status)
			}
			payload["planToken"] = data["planToken"]
			status, body = call("/"+id+"/run", "", nil)
			if status != 200 || body["data"].(map[string]any)["job"].(map[string]any)["status"] != "completed" {
				t.Fatal(status, body)
			}
			s.deps.Runtime = nil
			status, _ = call("/"+id+"/run", "", nil)
			if status != 200 {
				t.Fatal("replay needs runtime", status)
			}
			status, body = get("mailbox-cleanups/" + job.ID + "/items?limit=3")
			if status != 200 || len(body["data"].([]any)) != 3 {
				t.Fatal(status, body)
			}
			cursor = body["page"].(map[string]any)["nextCursor"].(string)
			status, _ = get("mailbox-cleanups/" + id + "/items?limit=3&cursor=" + url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal("cross job item cursor", status)
			}
			status, body = get("mailbox-cleanups?limit=1")
			if status != 200 {
				t.Fatal(status, body)
			}
			cursor = body["page"].(map[string]any)["nextCursor"].(string)
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/20/mailbox-cleanups?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal(status)
			}
		})
	}
}
