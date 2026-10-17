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
	"dnf90.local/gateway/internal/modules/dnf/mail"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type heldMailbox struct {
	repository.MailboxRepository
	locked, release chan struct{}
}

func (s heldMailbox) Load(ctx context.Context, id string) (repository.MailboxRecord, bool, error) {
	v, found, e := s.MailboxRepository.Load(ctx, id)
	close(s.locked)
	select {
	case <-s.release:
	case <-ctx.Done():
		return v, found, ctx.Err()
	}
	return v, found, e
}

type heldMailboxAssets struct {
	repository.MailboxAssetUnitOfWork
	locked, release chan struct{}
}

func (s heldMailboxAssets) WithinMailboxAssets(ctx context.Context, asset, mailbox string, fn func(repository.CharacterRepository, repository.InventoryRepository, repository.MailboxRepository) error) error {
	return s.MailboxAssetUnitOfWork.WithinMailboxAssets(ctx, asset, mailbox, func(c repository.CharacterRepository, i repository.InventoryRepository, m repository.MailboxRepository) error {
		return fn(c, i, heldMailbox{m, s.locked, s.release})
	})
}

type mailboxFailStore struct {
	repository.MailboxManagementRepository
}

func (s mailboxFailStore) DeleteManagedMails(ctx context.Context, ids []string, at time.Time) error {
	if e := s.MailboxManagementRepository.DeleteManagedMails(ctx, ids, at); e != nil {
		return e
	}
	return errors.New("injected post-delete failure")
}

type mailboxFailUOW struct {
	repository.MailboxManagementUnitOfWork
	save bool
}

func (s mailboxFailUOW) WithinMailboxManagement(ctx context.Context, account, id string, fn func(repository.MailboxManagementRepository, repository.ManagementReceiptStore) error) error {
	return s.MailboxManagementUnitOfWork.WithinMailboxManagement(ctx, account, id, func(m repository.MailboxManagementRepository, r repository.ManagementReceiptStore) error {
		if s.save {
			m = mailboxFailStore{m}
		} else {
			r = failingReceipts{r}
		}
		return fn(m, r)
	})
}

func TestV1ManagedMailboxMySQL(t *testing.T) {
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
			for i, id := range []string{"10", "11", "20"} {
				account := "wallet:1"
				if id == "20" {
					account = "wallet:2"
				}
				check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: account, Slot: i, Name: "邮箱" + id, Stats: map[string]int64{"gold": 100, "keep": 7}}))
			}
			fixture := repository.MailboxRecord{CharacterID: "10", Mails: map[string]repository.MailRecord{}}
			for i := 1; i <= 110; i++ {
				id := fmt.Sprint(i)
				fixture.Mails[id] = repository.MailRecord{MailID: id, Title: "邮件" + id, Body: "保留正文", SenderName: "发件人", Gold: 100, CreatedAt: time.Now().UTC(), Attachments: []repository.MailAttachment{{ItemID: 123, Count: 3, RawEntry: []byte{1, 2, 3}, Extra: map[string]string{"keep": "original"}}}, Metadata: map[string]string{"delivery_receipt": "preserve"}}
			}
			m := fixture.Mails["1"]
			m.Claimed = true
			fixture.Mails["1"] = m
			m = fixture.Mails["2"]
			m.Gold = 0
			m.Attachments = nil
			fixture.Mails["2"] = m
			// Gold-only mail exercises the actual game Claim path without a PVF resolver.
			m = fixture.Mails["110"]
			m.Attachments = nil
			fixture.Mails["110"] = m
			check(g.Mailbox.Save(ctx, fixture))
			u := g.Character.(repository.MailboxManagementUnitOfWork)
			load := func() repository.MailboxRecord {
				t.Helper()
				v, ok, e := open().Mailbox.Load(ctx, "10")
				check(e)
				if !ok {
					t.Fatal("mailbox missing")
				}
				return v
			}
			baseline := load()
			charBefore, _, _ := g.Character.Load(ctx, "10")
			acctBefore, _, _ := g.Account.Load(ctx, "wallet:1")
			offline := func(context.Context, string) error { return nil }
			cmd := adminops.MailboxCommand{AccountID: "wallet:1", CharacterID: "10", Actor: "测试管理员", Reason: "清理", Items: []adminops.MailSelection{{MailID: "1"}, {MailID: "2"}, {MailID: "3"}}, Key: "mail-delete-request-0001"}
			_, e := adminops.RunMailboxOperation(ctx, u, cmd, false, nil)
			if !errors.Is(e, adminops.ErrUnclaimedMail) {
				t.Fatal(e)
			}
			cmd.DiscardUnclaimed = true
			plan, e := adminops.RunMailboxOperation(ctx, u, cmd, false, nil)
			check(e)
			if plan.Changes[0].DiscardedAssets || plan.Changes[1].DiscardedAssets || !plan.Changes[2].DiscardedAssets {
				t.Fatal(plan)
			}
			for i, v := range plan.Changes {
				cmd.Items[i].Revision = v.Revision
			}
			unchanged := func() {
				t.Helper()
				if !reflect.DeepEqual(baseline, load()) {
					t.Fatal("mailbox changed")
				}
				rows, e := u.MailboxManagementLog(ctx, "10", 0, 10)
				check(e)
				if len(rows) != 0 {
					t.Fatal("unexpected receipt")
				}
			}
			unchanged()
			for _, mode := range []string{"save", "receipt", "late-online", "unknown"} {
				worker := u
				guard := offline
				calls := 0
				switch mode {
				case "save":
					worker = mailboxFailUOW{u, true}
				case "receipt":
					worker = mailboxFailUOW{u, false}
				case "unknown":
					guard = nil
				case "late-online":
					guard = func(context.Context, string) error {
						calls++
						if calls == 2 {
							return adminops.ErrCharacterOnline
						}
						return nil
					}
				}
				if _, e = adminops.RunMailboxOperation(ctx, worker, cmd, true, guard); e == nil {
					t.Fatal(mode, "accepted")
				}
				unchanged()
			}
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := adminops.RunMailboxOperation(ctx, u, cmd, true, offline); errs <- e }()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				check(e)
			}
			after := load()
			for id, before := range baseline.Mails {
				v := after.Mails[id]
				if id == "1" || id == "2" || id == "3" {
					before.Deleted = true
					before.Read = true
					before.Gold = 0
					before.Attachments = nil
				}
				if !reflect.DeepEqual(before, v) {
					t.Fatalf("mail %s: unexpected mutation", id)
				}
			}
			charAfter, _, _ := g.Character.Load(ctx, "10")
			acctAfter, _, _ := g.Account.Load(ctx, "wallet:1")
			if !reflect.DeepEqual(charBefore, charAfter) || !reflect.DeepEqual(acctBefore, acctAfter) {
				t.Fatal("wallet or character changed")
			}
			rows, e := u.MailboxManagementLog(ctx, "10", 0, 10)
			check(e)
			if len(rows) != 1 {
				t.Fatal(len(rows))
			}
			_, e = adminops.RunMailboxOperation(ctx, u, cmd, true, nil)
			check(e)
			bad := cmd
			bad.Reason = "changed"
			_, e = adminops.RunMailboxOperation(ctx, u, bad, true, offline)
			if !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			bad = cmd
			bad.AccountID = "wallet:2"
			if _, e = adminops.RunMailboxOperation(ctx, u, bad, true, offline); e == nil {
				t.Fatal("wrong owner accepted")
			}
			bad = cmd
			bad.Key = "new-key-deleted-mail"
			_, e = adminops.RunMailboxOperation(ctx, u, bad, true, offline)
			if !errors.Is(e, repository.ErrManagedMailMissing) {
				t.Fatal(e)
			}

			prepare := func(ids ...string) adminops.MailboxCommand {
				t.Helper()
				v := cmd
				v.Items = nil
				v.Key = "next-mail-request-0001"
				for _, id := range ids {
					v.Items = append(v.Items, adminops.MailSelection{MailID: id})
				}
				p, e := adminops.RunMailboxOperation(ctx, u, v, false, nil)
				check(e)
				for i, c := range p.Changes {
					v.Items[i].Revision = c.Revision
				}
				return v
			}
			// A real game claim takes the same mailbox parent lock and invalidates deletion.
			stale := prepare("110")
			_, e = mail.NewOwner(g).WithClaimedLetterMode(mail.ClaimedLetterKeep).Claim(ctx, "10", mail.ExtractRequest{MailIDs: []uint32{110}})
			check(e)
			_, e = adminops.RunMailboxOperation(ctx, u, stale, true, offline)
			if !errors.Is(e, adminops.ErrConflict) {
				t.Fatal("claim did not invalidate preview", e)
			}
			fresh := prepare("110")
			_, e = adminops.RunMailboxOperation(ctx, u, fresh, true, offline)
			check(e)
			_, e = mail.NewOwner(g).WithClaimedLetterMode(mail.ClaimedLetterKeep).Claim(ctx, "10", mail.ExtractRequest{MailIDs: []uint32{110}})
			if !errors.Is(e, mail.ErrMailboxMailNotFound) {
				t.Fatal("deleted mail claim", e)
			}

			// Concurrent distinct keys cannot delete the same version twice.
			fresh = prepare("4")
			errs = make(chan error, 2)
			for i := range 2 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					v := fresh
					v.Key = fmt.Sprintf("mail-distinct-request-%d", i)
					_, e := adminops.RunMailboxOperation(ctx, u, v, true, offline)
					errs <- e
				}(i)
			}
			wg.Wait()
			close(errs)
			okCount, conflicts := 0, 0
			for e := range errs {
				if e == nil {
					okCount++
				} else if errors.Is(e, repository.ErrManagedMailMissing) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			}
			if okCount != 1 || conflicts != 1 {
				t.Fatal(okCount, conflicts)
			}

			// A 100-mail batch is atomic; invalid or duplicate selections write nothing.
			ids := []string{}
			for i := 5; i <= 104; i++ {
				ids = append(ids, fmt.Sprint(i))
			}
			fresh = prepare(ids...)
			fresh.Key = "mail-batch-100-request"
			bad = fresh
			bad.Items = append(append([]adminops.MailSelection(nil), fresh.Items...), adminops.MailSelection{MailID: "105", Revision: fresh.Items[0].Revision})
			_, e = adminops.RunMailboxOperation(ctx, u, bad, true, offline)
			if !errors.Is(e, adminops.ErrMailboxInput) {
				t.Fatal(e)
			}
			bad = fresh
			bad.Items = []adminops.MailSelection{fresh.Items[0], fresh.Items[0]}
			_, e = adminops.RunMailboxOperation(ctx, u, bad, true, offline)
			if !errors.Is(e, adminops.ErrMailboxInput) {
				t.Fatal(e)
			}
			_, e = adminops.RunMailboxOperation(ctx, u, fresh, true, offline)
			check(e)
			batchAfter := load()
			for _, id := range ids {
				if !batchAfter.Mails[id].Deleted {
					t.Fatal(id)
				}
			}

			// Hold the real Claim transaction after its locking Load. Deletion
			// waits for the same parent lock, then sees the changed revision.
			mailbox := load()
			m = mailbox.Mails["109"]
			m.Attachments = nil
			mailbox.Mails["109"] = m
			check(g.Mailbox.Save(ctx, mailbox))
			stale = prepare("109")
			stale.Key = "concurrent-claim-delete"
			locked, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			game := open()
			game.MailboxAssets = heldMailboxAssets{game.MailboxAssets, locked, release}
			claimDone, deleteDone := make(chan error, 1), make(chan error, 1)
			lockCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			go func() {
				_, e := mail.NewOwner(game).WithClaimedLetterMode(mail.ClaimedLetterKeep).Claim(lockCtx, "10", mail.ExtractRequest{MailIDs: []uint32{109}})
				claimDone <- e
			}()
			select {
			case <-locked:
			case <-lockCtx.Done():
				t.Fatal("claim failed to acquire lock")
			}
			go func() { _, e := adminops.RunMailboxOperation(lockCtx, u, stale, true, offline); deleteDone <- e }()
			select {
			case e := <-deleteDone:
				t.Fatal("delete bypassed claim lock", e)
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			check(<-claimDone)
			if e = <-deleteDone; !errors.Is(e, adminops.ErrConflict) {
				t.Fatal("concurrent claim lost", e)
			}

			// HTTP: signed server revisions, CSRF, content/owner binding and durable recovery.
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true})
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(e)
			token, e := s.startSession()
			check(e)
			payload := map[string]any{"accountId": "wallet:1", "items": []map[string]any{{"mailId": "105"}}, "discardUnclaimed": true, "reason": "HTTP删除"}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, "characters/10/mailbox-operations"+path, string(raw), key, headers)
			}
			status, body := call("/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call("/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			data := body["data"].(map[string]any)
			payload["items"] = data["items"]
			payload["planToken"] = data["planToken"]
			payload["reason"] = "tampered"
			status, _ = call("", "mail-http-request-0001", nil)
			if status != 409 {
				t.Fatal(status)
			}
			payload["reason"] = "HTTP删除"
			payload["accountId"] = "wallet:2"
			status, _ = call("", "mail-http-request-0001", nil)
			if status != 409 {
				t.Fatal(status)
			}
			payload["accountId"] = "wallet:1"
			status, body = call("", "mail-http-request-0001", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			receipt := body["data"]
			s.deps.Runtime = nil
			status, body = call("", "mail-http-request-0001", nil)
			if status != 200 || !reflect.DeepEqual(receipt, body["data"]) {
				t.Fatal("replay requires runtime or deleted mail", status, body)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/mailbox-operations?limit=1")
			if status != 200 || len(body["data"].([]any)) != 1 {
				t.Fatal(status, body)
			}
			cursor := body["page"].(map[string]any)["nextCursor"].(string)
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/mailbox-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 200 {
				t.Fatal(status)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/20/mailbox-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal(status)
			}
			for _, id := range []string{"11", "20"} {
				rows, e := u.MailboxManagementLog(ctx, id, 0, 10)
				check(e)
				if len(rows) != 0 {
					t.Fatal("cross character receipts")
				}
			}
		})
	}
}
