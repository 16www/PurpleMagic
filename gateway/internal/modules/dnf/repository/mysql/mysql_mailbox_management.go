package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const mailboxManagementTable = "mailbox_management_operations"

type mysqlMailboxManagementStore struct {
	router    mysqlRouter
	character string
	account   string
	updated   time.Time
	found     bool
}

func (s *mysqlMailboxManagementStore) LoadManagedMails(ctx context.Context, ids []string) (repository.MailboxManagementState, error) {
	v := repository.MailboxManagementState{UpdatedAt: s.updated, Mails: map[string]repository.MailRecord{}}
	if len(ids) == 0 || len(ids) > 100 {
		return v, repository.ErrResourceRange
	}
	if !s.found {
		return v, repository.ErrManagedMailMissing
	}
	reader := &mysqlCharStore{mysqlStoreBase: mysqlStoreBase{router: s.router}}
	// Selected records only. The parent row lock keeps gameplay claims and
	// deliveries from rewriting this aggregate while the operation is prepared.
	for _, id := range ids {
		m, found, e := reader.mailDetail(ctx, s.character, id)
		if e != nil {
			return v, e
		}
		if !found {
			return v, repository.ErrManagedMailMissing
		}
		if len(m.Title) > 4096 || len(m.SenderName) > 1024 {
			return v, repository.ErrResourceRange
		}
		v.Mails[id] = m
	}
	return v, nil
}
func (s *mysqlMailboxManagementStore) DeleteManagedMails(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 || len(ids) > 100 || !s.found || !at.After(s.updated) {
		return repository.ErrResourceRange
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{s.character, s.character}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 128 || seen[id] {
			return repository.ErrResourceRange
		}
		seen[id] = true
		args = append(args, id)
	}
	where := " WHERE character_id=? AND BINARY character_id=BINARY ? AND BINARY mail_id IN (" + marks + ")"
	table, e := s.router.writeTable(mysqlMailsTable, s.character)
	if e != nil {
		return e
	}
	result, e := s.router.db.ExecContext(ctx, "UPDATE "+table+" SET deleted_flag=1,read_flag=1,gold=0"+where+" AND deleted_flag=0", args...)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != int64(len(ids)) {
		return repository.ErrManagedMailMissing
	}
	for _, name := range []string{mysqlMailAttachmentExtraTable, mysqlMailAttachmentsTable} {
		table, e = s.router.writeTable(name, s.character)
		if e != nil {
			return e
		}
		if _, e = s.router.db.ExecContext(ctx, "DELETE FROM "+table+where, args...); e != nil {
			return e
		}
	}
	table, e = s.router.writeTable(mysqlMailboxTable, s.character)
	if e != nil {
		return e
	}
	_, e = s.router.db.ExecContext(ctx, "UPDATE "+table+" SET updated_at=? WHERE character_id=? AND BINARY character_id=BINARY ?", at.UTC(), s.character, s.character)
	return e
}
func (s *mysqlCharStore) WithinMailboxManagement(ctx context.Context, account, id string, fn func(repository.MailboxManagementRepository, repository.ManagementReceiptStore) error) error {
	if account == "" || id == "" || fn == nil {
		return repository.ErrResourceRange
	}
	beginner, ok := s.router.db.(sqlTxBeginner)
	if !ok {
		return repository.ErrMailboxAssetTransactionUnavailable
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
	if e = lockManagedAccount(ctx, tx, r, account); e != nil {
		return e
	}
	table, e := r.writeTable(mysqlMailboxTable, id)
	if e != nil {
		return e
	}
	var updated sql.NullTime
	e = tx.QueryRowContext(ctx, "SELECT updated_at FROM "+table+" WHERE character_id=? AND BINARY character_id=BINARY ? FOR UPDATE", id, id).Scan(&updated)
	found := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	table, e = r.writeTable(mysqlCharTable, id)
	if e != nil {
		return e
	}
	var actual string
	e = tx.QueryRowContext(ctx, "SELECT character_id FROM "+table+" WHERE character_id=? AND BINARY character_id=BINARY ? AND BINARY account_id=BINARY ? AND delete_flag=0 FOR UPDATE", id, id, account).Scan(&actual)
	if e != nil {
		return e
	}
	table, e = r.writeTable(mailboxManagementTable, id)
	if e != nil {
		return e
	}
	store := &mysqlMailboxManagementStore{router: r, character: id, account: account, updated: scanTime(updated), found: found}
	if e = fn(store, characterManagementReceipts{db: r.db, table: table, character: id, account: account}); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *mysqlCharStore) MailboxManagementLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return s.characterManagementLog(ctx, id, before, limit, mailboxManagementTable)
}
