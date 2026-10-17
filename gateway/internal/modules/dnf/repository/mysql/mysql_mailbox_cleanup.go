package mysql

import (
	"context"
	"database/sql"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"errors"
	"time"
)

const mailboxCleanupTable = "mailbox_cleanup_jobs"
const mailboxCleanupItemsTable = "mailbox_cleanup_items"

func (s *mysqlMailboxManagementStore) MailboxVersion() time.Time { return s.updated }
func (s *mysqlMailboxManagementStore) CleanupCandidates(ctx context.Context, f repository.MailboxFilter, after string, limit int) ([]repository.MailRecord, error) {
	r := &mysqlCharStore{mysqlStoreBase: mysqlStoreBase{router: s.router}}
	return r.MailboxFilteredPage(ctx, s.character, f, after, limit)
}
func (s *mysqlMailboxManagementStore) LoadCleanup(ctx context.Context, id string) ([]byte, bool, error) {
	t, e := s.router.writeTable(mailboxCleanupTable, s.character)
	if e != nil {
		return nil, false, e
	}
	var raw []byte
	e = s.router.db.QueryRowContext(ctx, "SELECT payload FROM "+t+" WHERE character_id=? AND request_id=?", s.character, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, false, nil
	}
	return raw, e == nil, e
}
func (s *mysqlMailboxManagementStore) SaveCleanup(ctx context.Context, id string, raw []byte) error {
	if len(id) < 16 || len(id) > 128 || len(raw) > 16384 {
		return repository.ErrResourceRange
	}
	t, e := s.router.writeTable(mailboxCleanupTable, s.character)
	if e != nil {
		return e
	}
	_, e = s.router.db.ExecContext(ctx, "INSERT INTO "+t+" (character_id,account_id,request_id,payload,created_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE payload=VALUES(payload)", s.character, s.account, id, raw)
	return e
}
func (s *mysqlMailboxManagementStore) AppendCleanupItems(ctx context.Context, id string, items []repository.MailboxCleanupItem) error {
	if len(items) > 100 {
		return repository.ErrResourceRange
	}
	t, e := s.router.writeTable(mailboxCleanupItemsTable, s.character)
	if e != nil {
		return e
	}
	for _, item := range items {
		if item.Ordinal < 1 || len(item.Payload) > 65536 {
			return repository.ErrResourceRange
		}
		if _, e = s.router.db.ExecContext(ctx, "INSERT INTO "+t+" (character_id,job_id,ordinal,payload) VALUES(?,?,?,?)", s.character, id, item.Ordinal, item.Payload); e != nil {
			return e
		}
	}
	return nil
}
func (s *mysqlMailboxManagementStore) CleanupItems(ctx context.Context, id string, after int64, limit int) ([]repository.MailboxCleanupItem, error) {
	if after < 0 || limit < 1 || limit > 101 {
		return nil, repository.ErrResourceRange
	}
	t, e := s.router.writeTable(mailboxCleanupItemsTable, s.character)
	if e != nil {
		return nil, e
	}
	rows, e := s.router.db.QueryContext(ctx, "SELECT ordinal,payload FROM "+t+" WHERE character_id=? AND job_id=? AND ordinal>? ORDER BY ordinal LIMIT ?", s.character, id, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []repository.MailboxCleanupItem{}
	for rows.Next() {
		var item repository.MailboxCleanupItem
		if e = rows.Scan(&item.Ordinal, &item.Payload); e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *mysqlCharStore) MailboxCleanupLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return s.characterManagementLog(ctx, id, before, limit, mailboxCleanupTable)
}
