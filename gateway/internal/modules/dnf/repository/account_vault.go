package repository

import (
	"context"
	"errors"
	"time"
)

const (
	VaultGoldKey     = "account_cargo_gold"
	VaultCapacityKey = "account_cargo_level"
	VaultCreatedKey  = "account_cargo_created"
	VaultSequenceKey = "gm_vault_sequence_v1"
)

var ErrVaultState = errors.New("invalid account vault state")

// AccountVaultState contains only the four metadata entries needed by this API.
type AccountVaultState struct {
	AccountID string
	UpdatedAt time.Time
	Values    map[string]string
}

func AccountVaultKeys() []string {
	return []string{VaultGoldKey, VaultCapacityKey, VaultCreatedKey, VaultSequenceKey}
}

type AccountVaultReader interface {
	LoadAccountVault(context.Context, string) (AccountVaultState, error)
}
type AccountVaultRepository interface {
	AccountVaultReader
	SaveAccountVault(context.Context, AccountVaultState, []string) error
	// Current reads, bounded by the game's account character limit.
	VaultCharacters(context.Context) ([]string, error)
}
type AccountVaultUnitOfWork interface {
	WithinAccountVault(context.Context, string, func(AccountVaultRepository, ManagementReceiptStore) error) error
	AccountVaultLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
