package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var _ repository.ContainerManagementReader = (*mysqlCharStore)(nil)
var _ repository.ContainerManagementUnitOfWork = (*mysqlCharStore)(nil)

func (s *mysqlCharStore) LoadContainerManagement(ctx context.Context, id string) (repository.ContainerManagementState, error) {
	snapshot, close, err := s.resourceSnapshot(ctx)
	if err != nil {
		return repository.ContainerManagementState{}, err
	}
	defer close()
	return snapshot.loadContainerManagement(ctx, id)
}
func (s *mysqlCharStore) loadContainerManagement(ctx context.Context, id string) (repository.ContainerManagementState, error) {
	var state repository.ContainerManagementState
	c, found, err := s.LoadCharacterGold(ctx, id)
	if err != nil {
		return state, err
	}
	if !found || c.Stats["delete_flag"] != 0 {
		return state, sql.ErrNoRows
	}
	state.Character = c
	scope := repository.CharacterContainerStateScope(id)
	table, err := s.router.readTable(mysqlSettingsTable, scope)
	if err != nil {
		return state, err
	}
	values, err := s.router.readTable(mysqlSettingValuesTable, scope)
	if err != nil {
		return state, err
	}
	var updated sql.NullTime
	err = s.router.db.QueryRowContext(ctx, s.router.selectQuery("SELECT scope,updated_at FROM "+table+" WHERE scope=? AND BINARY scope=BINARY ?"), scope, scope).Scan(&state.Settings.Scope, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.Found = true
	state.Settings.UpdatedAt = scanTime(updated)
	state.Settings.Values = map[string]string{}
	keys := repository.ContainerManagementKeys()
	args := []any{scope}
	for _, key := range keys {
		args = append(args, key)
	}
	query := "SELECT entry_key,CASE WHEN OCTET_LENGTH(entry_value)<=128 THEN entry_value ELSE NULL END FROM " + values + " WHERE scope=? AND entry_key IN (" + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + ") ORDER BY entry_key LIMIT 7"
	rows, err := s.router.db.QueryContext(ctx, s.router.selectQuery(query), args...)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value sql.NullString
		if err = rows.Scan(&key, &value); err != nil {
			return state, err
		}
		if !value.Valid {
			return state, repository.ErrCharacterContainerStateInvalid
		}
		state.Settings.Values[key] = value.String
	}
	return state, rows.Err()
}

type mysqlContainerManagementStore struct {
	base               *mysqlCharStore
	account, character string
}

func (s *mysqlContainerManagementStore) LoadContainerManagement(ctx context.Context, id string) (repository.ContainerManagementState, error) {
	if id != s.character {
		return repository.ContainerManagementState{}, repository.ErrCharacterSummaryRange
	}
	return s.base.loadContainerManagement(ctx, id)
}
func (s *mysqlContainerManagementStore) SaveContainerManagement(ctx context.Context, state repository.ContainerManagementState, keys []string) error {
	if !state.Found || state.Character.CharacterID != s.character || state.Character.AccountID != s.account || state.Settings.Scope != repository.CharacterContainerStateScope(s.character) || state.Settings.UpdatedAt.IsZero() || len(keys) == 0 || len(keys) > 3 {
		return repository.ErrResourceRange
	}
	if _, err := repository.CharacterContainerStateFromSettings(state.Settings, s.character); err != nil {
		return err
	}
	seq, err := strconv.ParseInt(state.Settings.Values[repository.ContainerManagementSequence], 10, 64)
	if err != nil || seq <= 0 || strconv.FormatInt(seq, 10) != state.Settings.Values[repository.ContainerManagementSequence] {
		return repository.ErrResourceRange
	}
	allowed := map[string]bool{}
	for _, c := range repository.ManagedContainers() {
		allowed[c.Key] = true
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if !allowed[key] || seen[key] {
			return repository.ErrResourceRange
		}
		seen[key] = true
	}
	r := s.base.router
	scope := state.Settings.Scope
	table, err := r.writeTable(mysqlSettingsTable, scope)
	if err != nil {
		return err
	}
	values, err := r.writeTable(mysqlSettingValuesTable, scope)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, "UPDATE "+table+" SET updated_at=? WHERE scope=? AND BINARY scope=BINARY ?", state.Settings.UpdatedAt.UTC(), scope, scope)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return repository.ErrResourceRange
	}
	for _, key := range append(append([]string(nil), keys...), repository.ContainerManagementSequence) {
		_, err = r.db.ExecContext(ctx, "INSERT INTO "+values+" (scope,entry_key,entry_value) VALUES(?,?,?) ON DUPLICATE KEY UPDATE entry_value=VALUES(entry_value)", scope, key, state.Settings.Values[key])
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *mysqlCharStore) WithinContainerManagement(ctx context.Context, account, id string, fn func(repository.ContainerManagementRepository, repository.ManagementReceiptStore) error) error {
	if fn == nil {
		return repository.ErrResourceRange
	}
	return s.withinCharacterManagementRouter(ctx, account, id, "container_management_operations", func(r mysqlRouter, receipts repository.ManagementReceiptStore) error {
		store := &mysqlContainerManagementStore{base: &mysqlCharStore{mysqlStoreBase: mysqlStoreBase{router: r}}, account: account, character: id}
		c, found, err := store.base.LoadCharacterGold(ctx, id)
		if err != nil {
			return err
		}
		if !found || c.AccountID != account || c.CharacterID != id || c.Stats["delete_flag"] != 0 {
			return sql.ErrNoRows
		}
		return fn(store, receipts)
	})
}
func (s *mysqlCharStore) ContainerManagementLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return s.characterManagementLog(ctx, id, before, limit, "container_management_operations")
}
