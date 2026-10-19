package gmtool

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestV1AccountFenceMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			ctx := context.Background()
			_, _, open := currencyFixture(t, shards)
			g := open()
			if err := g.Character.Save(ctx, repository.CharacterRecord{AccountID: "wallet:1", CharacterID: "10", Name: "fence", Stats: map[string]int64{"gold": 100}}); err != nil {
				t.Fatal(err)
			}
			fence := g.Account.(repository.AccountEditFence)
			aborted := errors.New("rollback fixture")
			check := func() error {
				// Separate SQL transaction, like a new or restarted game process.
				waitCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
				defer cancel()
				if err := fence.WaitForAccountEdits(waitCtx, "wallet:1"); !errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("account fence not held: %w", err)
				}
				otherCtx, otherCancel := context.WithTimeout(ctx, time.Second)
				defer otherCancel()
				if err := fence.WaitForAccountEdits(otherCtx, "wallet:2"); err != nil {
					return fmt.Errorf("unrelated account blocked: %w", err)
				}
				return aborted
			}
			checks := map[string]func() error{
				"character": func() error {
					return g.Character.(repository.CharacterManagementUnitOfWork).WithinCharacterManagement(ctx, "wallet:1", "10", func(repository.Group, repository.ManagementReceiptStore) error { return check() })
				},
				"account-inventory": func() error {
					return g.AccountInventory.(repository.InventoryManagementUnitOfWork).WithinInventoryManagement(ctx, "wallet:1", "10", func(repository.Group, repository.ManagementReceiptStore) error { return check() })
				},
				"mailbox": func() error {
					return g.Character.(repository.MailboxManagementUnitOfWork).WithinMailboxManagement(ctx, "wallet:1", "10", func(repository.MailboxManagementRepository, repository.ManagementReceiptStore) error { return check() })
				},
				"vault": func() error {
					return g.Account.(repository.AccountVaultUnitOfWork).WithinAccountVault(ctx, "wallet:1", func(repository.AccountVaultRepository, repository.ManagementReceiptStore) error { return check() })
				},
			}
			for name, run := range checks {
				t.Run(name, func(t *testing.T) {
					if err := run(); !errors.Is(err, aborted) {
						t.Fatal(err)
					}
					if err := fence.WaitForAccountEdits(ctx, "wallet:1"); err != nil {
						t.Fatal("rollback retained lock", err)
					}
				})
			}
			gold := g.Character.(repository.CharacterGoldUnitOfWork)
			for _, commit := range []bool{false, true} {
				started, done := make(chan struct{}), make(chan error, 1)
				err := gold.WithinCharacterGold(ctx, "wallet:1", "10", func(r repository.CharacterGoldRepository, receipts repository.ManagementReceiptStore) error {
					c, _, e := r.LoadCharacterGold(ctx, "10")
					if e != nil {
						return e
					}
					c.Stats["gold"] = 321
					c.Stats["gm_gold_sequence_v1"]++
					c.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
					if e = r.SaveCharacterGold(ctx, c); e != nil {
						return e
					}
					if e = receipts.Append(ctx, "account-fence-commit", []byte(`{"ok":true}`)); e != nil {
						return e
					}
					go func() { close(started); done <- fence.WaitForAccountEdits(ctx, "wallet:1") }()
					<-started
					select {
					case e := <-done:
						return fmt.Errorf("login passed before commit: %v", e)
					case <-time.After(50 * time.Millisecond):
					}
					if !commit {
						return aborted
					}
					return nil
				})
				if commit && err != nil || !commit && !errors.Is(err, aborted) {
					t.Fatal(err)
				}
				select {
				case e := <-done:
					if e != nil {
						t.Fatal(e)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("login still blocked after transaction")
				}
				c, _, err := open().Character.(repository.CharacterGoldReader).LoadCharacterGold(ctx, "10")
				if err != nil {
					t.Fatal(err)
				}
				want := int64(100)
				wantReceipts := 0
				if commit {
					want = 321
					wantReceipts = 1
				}
				if c.Stats["gold"] != want {
					t.Fatal("login would load stale gold", c.Stats)
				}
				rows, err := gold.CharacterGoldLog(ctx, "10", 0, 20)
				if err != nil || len(rows) != wantReceipts {
					t.Fatal("receipt atomicity", len(rows), err)
				}
			}
		})
	}
}
