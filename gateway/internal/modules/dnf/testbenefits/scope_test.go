package testbenefits_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func TestItemRuleAccountScopeAcrossRolesAndRecreation(t *testing.T) {
	g, job, first := fixture(t)
	ctx := context.Background()
	job.CampaignID = strings.Repeat("b", 32)
	job.Scope = testbenefits.ScopeAccount
	second := first
	second.CharacterID = "2"
	second.CreatedAt = first.CreatedAt.Add(time.Hour)
	require(t, g.Character.Save(ctx, repository.CharacterRecord{AccountID: second.AccountID, CharacterID: second.CharacterID, Slot: 1, CreatedAt: second.CreatedAt}))
	var wg sync.WaitGroup
	results := make(chan testbenefits.DeliveryReceipt, 12)
	for i := 0; i < 12; i++ {
		r := first
		if i%2 == 1 {
			r = second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := testbenefits.DeliverWithReceipt(ctx, g, job, r)
			if err != nil {
				t.Error(err)
				return
			}
			results <- receipt
		}()
	}
	wg.Wait()
	close(results)
	var actual testbenefits.DeliveryReceipt
	for receipt := range results {
		if actual.MailID == "" {
			actual = receipt
		}
		if !reflect.DeepEqual(actual, receipt) {
			t.Fatal("cross-role retries disagree on actual recipient")
		}
	}
	total := 0
	for _, id := range []string{"1", "2"} {
		box, _, err := g.Mailbox.Load(ctx, id)
		require(t, err)
		total += len(box.Mails)
		if id != actual.CharacterID && len(box.Mails) != 0 {
			t.Fatal("mail delivered to both roles")
		}
		require(t, g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: id}))
	}
	if total != 1 {
		t.Fatalf("account received %d mails", total)
	}
	// Mail deletion and recreation do not restore account-wide eligibility.
	c, _, err := g.Character.Load(ctx, first.CharacterID)
	require(t, err)
	c.CreatedAt = c.CreatedAt.Add(24 * time.Hour)
	require(t, g.Character.Save(ctx, c))
	first.CreatedAt = c.CreatedAt
	replayed, err := testbenefits.DeliverWithReceipt(ctx, g, job, first)
	require(t, err)
	if !reflect.DeepEqual(replayed, actual) {
		t.Fatal("permanent account receipt changed")
	}
	for _, id := range []string{"1", "2"} {
		box, _, err := g.Mailbox.Load(ctx, id)
		require(t, err)
		if len(box.Mails) != 0 {
			t.Fatal("account grant repeated after deletion/recreation")
		}
	}
	changed := job
	changed.Gold++
	if _, err := testbenefits.Deliver(ctx, g, changed, second); err == nil {
		t.Fatal("changed content accepted")
	}
	// The same rule remains independently available to another account.
	require(t, g.Account.Save(ctx, repository.AccountRecord{AccountID: "other"}))
	require(t, g.Character.Save(ctx, repository.CharacterRecord{AccountID: "other", CharacterID: "3", CreatedAt: first.CreatedAt}))
	other, err := testbenefits.DeliverWithReceipt(ctx, g, job, testbenefits.Recipient{AccountID: "other", CharacterID: "3", CreatedAt: first.CreatedAt})
	require(t, err)
	if other.CharacterID != "3" || other.MailID == "" {
		t.Fatal(other)
	}
}

func TestItemRuleScopeCompatibilityValidationAndRollback(t *testing.T) {
	g, job, r := fixture(t)
	ctx := context.Background()
	job.CampaignID = strings.Repeat("b", 32)
	id, err := testbenefits.Deliver(ctx, g, job, r) // old JSON without scope
	require(t, err)
	job.Scope = testbenefits.ScopeCharacter
	got, err := testbenefits.Deliver(ctx, g, job, r)
	require(t, err)
	box, _, err := g.Mailbox.Load(ctx, r.CharacterID)
	require(t, err)
	if got != id || len(box.Mails) != 1 {
		t.Fatal("explicit character scope invalidated legacy receipt")
	}
	job.Scope = "typo"
	if _, err := testbenefits.Deliver(ctx, g, job, r); err == nil {
		t.Fatal("invalid scope accepted")
	}
	job.Scope = testbenefits.ScopeAccount
	g.MailboxAssets = failingReceipt{g.MailboxAssets, g.MailboxAssets.(repository.AccountMailboxCharacterUnitOfWork)}
	if _, err := testbenefits.Deliver(ctx, g, job, r); err == nil {
		t.Fatal("account receipt failure ignored")
	}
	box, _, err = g.Mailbox.Load(ctx, r.CharacterID)
	require(t, err)
	if len(box.Mails) != 1 {
		t.Fatal("account mail escaped receipt rollback")
	}
}
