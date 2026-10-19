package adminops

import (
	"context"
	"errors"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type emptyVaultAccount struct {
	repository.AccountVaultRepository
}

func (emptyVaultAccount) VaultCharacters(context.Context) ([]string, error) { return nil, nil }

func TestVaultAccountWithoutCharactersRequiresEditCheck(t *testing.T) {
	calls := 0
	err := vaultOffline(context.Background(), emptyVaultAccount{}, func(_ context.Context, id string) error {
		calls++
		if id != "" {
			t.Fatal("expected account-only edit check")
		}
		return ErrCharacterOnline
	})
	if !errors.Is(err, ErrCharacterOnline) || calls != 1 {
		t.Fatal("connected account without characters accepted", err, calls)
	}
}
