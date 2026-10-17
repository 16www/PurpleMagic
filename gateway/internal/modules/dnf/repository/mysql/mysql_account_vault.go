package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const accountVaultOperationsTable = "account_vault_operations"

var _ repository.AccountVaultReader = (*mysqlAccountStore)(nil)
var _ repository.AccountVaultUnitOfWork = (*mysqlAccountStore)(nil)

func (s *mysqlAccountStore) LoadAccountVault(ctx context.Context, id string) (repository.AccountVaultState, error) {
	base := &mysqlCharStore{mysqlStoreBase: s.mysqlStoreBase}
	snapshot, close, e := base.resourceSnapshot(ctx)
	if e != nil {
		return repository.AccountVaultState{}, e
	}
	defer close()
	return loadAccountVault(ctx, snapshot.router, id)
}
func loadAccountVault(ctx context.Context, r mysqlRouter, id string) (repository.AccountVaultState, error) {
	var state repository.AccountVaultState
	if id == "" || len(id) > 128 {
		return state, repository.ErrResourceRange
	}
	table, e := r.readTable(mysqlAccountTable, id)
	if e != nil {
		return state, e
	}
	values, e := r.readTable(mysqlAccountMetadataTable, id)
	if e != nil {
		return state, e
	}
	var updated sql.NullTime
	e = r.db.QueryRowContext(ctx, r.selectQuery("SELECT account_id,updated_at FROM "+table+" WHERE account_id=? AND BINARY account_id=BINARY ?"), id, id).Scan(&state.AccountID, &updated)
	if e != nil {
		return state, e
	}
	state.UpdatedAt = scanTime(updated)
	state.Values = map[string]string{}
	keys := repository.AccountVaultKeys()
	args := []any{id}
	for _, key := range keys {
		args = append(args, key)
	}
	query := "SELECT entry_key,CASE WHEN OCTET_LENGTH(entry_value)<=128 THEN entry_value ELSE NULL END FROM " + values + " WHERE account_id=? AND BINARY entry_key IN (" + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + ") ORDER BY entry_key LIMIT 5"
	rows, e := r.db.QueryContext(ctx, r.selectQuery(query), args...)
	if e != nil {
		return state, e
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value sql.NullString
		if e = rows.Scan(&key, &value); e != nil {
			return state, e
		}
		if !value.Valid {
			return state, repository.ErrVaultState
		}
		state.Values[key] = value.String
	}
	return state, rows.Err()
}

type mysqlAccountVaultStore struct {
	router  mysqlRouter
	account string
}

func (s *mysqlAccountVaultStore) LoadAccountVault(ctx context.Context, id string) (repository.AccountVaultState, error) {
	if e := repository.TransactionKeyError(ctx, s.account, id); e != nil {
		return repository.AccountVaultState{}, e
	}
	return loadAccountVault(ctx, s.router, id)
}
func (s *mysqlAccountVaultStore) VaultCharacters(ctx context.Context) ([]string, error) {
	ids := []string{}
	seen := map[string]bool{}
	for _, database := range s.router.readDBs {
		table := mysqlTable(database, s.router.tablePrefix, mysqlCharTable)
		rows, e := s.router.db.QueryContext(ctx, s.router.selectQuery("SELECT character_id FROM "+table+" WHERE account_id=? AND BINARY account_id=BINARY ? AND delete_flag=0 ORDER BY BINARY character_id LIMIT ?"), s.account, s.account, repository.DefaultCharacterSlotLimit+1)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			expected, e := s.router.readTable(mysqlCharTable, id)
			if e != nil || expected != table || seen[id] || len(ids) >= repository.DefaultCharacterSlotLimit {
				rows.Close()
				return nil, repository.ErrCharacterSummaryRange
			}
			seen[id] = true
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	return ids, nil
}
func (s *mysqlAccountVaultStore) SaveAccountVault(ctx context.Context, state repository.AccountVaultState, keys []string) error {
	if e := repository.TransactionKeyError(ctx, s.account, state.AccountID); e != nil {
		return e
	}
	if state.UpdatedAt.IsZero() || len(keys) == 0 || len(keys) > 3 {
		return repository.ErrResourceRange
	}
	seq, e := strconv.ParseInt(state.Values[repository.VaultSequenceKey], 10, 64)
	if e != nil || seq <= 0 {
		return repository.ErrVaultState
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			return repository.ErrResourceRange
		}
		seen[key] = true
		switch key {
		case repository.VaultGoldKey, repository.VaultCapacityKey, repository.VaultCreatedKey:
		default:
			return repository.ErrResourceRange
		}
		if _, found := state.Values[key]; !found || len(state.Values[key]) > 128 {
			return repository.ErrVaultState
		}
	}
	table, e := s.router.writeTable(mysqlAccountTable, s.account)
	if e != nil {
		return e
	}
	values, e := s.router.writeTable(mysqlAccountMetadataTable, s.account)
	if e != nil {
		return e
	}
	result, e := s.router.db.ExecContext(ctx, "UPDATE "+table+" SET updated_at=? WHERE account_id=? AND BINARY account_id=BINARY ?", state.UpdatedAt.UTC(), s.account, s.account)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return repository.ErrResourceRange
	}
	for _, key := range append(append([]string(nil), keys...), repository.VaultSequenceKey) {
		_, e = s.router.db.ExecContext(ctx, "INSERT INTO "+values+" (account_id,entry_key,entry_value) VALUES(?,?,?) ON DUPLICATE KEY UPDATE entry_value=VALUES(entry_value)", s.account, key, state.Values[key])
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *mysqlAccountStore) WithinAccountVault(ctx context.Context, id string, fn func(repository.AccountVaultRepository, repository.ManagementReceiptStore) error) error {
	if id == "" || fn == nil {
		return repository.ErrResourceRange
	}
	beginner, ok := s.router.db.(sqlTxBeginner)
	if !ok {
		return errors.New("account vault transaction unavailable")
	}
	tx, e := beginner.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r := s.router
	r.db = sqlTransactionDB{tx: tx}
	r.readDBs = append([]string(nil), r.writeDBs...)
	r.lockReads = true
	table, e := r.writeTable(mysqlAccountTable, id)
	if e != nil {
		return e
	}
	var actual string
	e = tx.QueryRowContext(ctx, "SELECT account_id FROM "+table+" WHERE account_id=? AND BINARY account_id=BINARY ? FOR UPDATE", id, id).Scan(&actual)
	if e != nil {
		return e
	}
	table, e = r.writeTable(accountVaultOperationsTable, id)
	if e != nil {
		return e
	}
	if e = fn(&mysqlAccountVaultStore{router: r, account: id}, accountManagementReceipts{db: r.db, table: table, account: id}); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *mysqlAccountStore) AccountVaultLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return readAccountManagementLog(ctx, s.router, accountVaultOperationsTable, id, before, limit)
}
