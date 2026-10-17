package mysql

import (
	"context"
	"database/sql"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func mailboxFilterSQL(id string, filter repository.MailboxFilter) (string, []any, error) {
	f, e := filter.Normalize()
	if e != nil {
		return "", nil, e
	}
	where := "m.character_id=? AND BINARY m.character_id=BINARY ? AND m.deleted_flag=0"
	args := []any{id, id}
	if f.Query != "" {
		where += " AND (LOCATE(?,m.title)>0 OR LOCATE(?,m.mail_id)>0)"
		args = append(args, f.Query, f.Query)
	}
	if f.Sender != "" {
		where += " AND LOCATE(?,m.sender_name)>0"
		args = append(args, f.Sender)
	}
	if f.Claimed != "all" {
		where += " AND m.claimed_flag=?"
		args = append(args, f.Claimed == "claimed")
	}
	if !f.From.IsZero() {
		where += " AND m.created_at>=?"
		args = append(args, f.From)
	}
	if !f.To.IsZero() {
		where += " AND m.created_at<?"
		args = append(args, f.To)
	}
	return where, args, nil
}

func (s *mysqlCharStore) MailboxFilteredPage(ctx context.Context, id string, f repository.MailboxFilter, after string, limit int) ([]repository.MailRecord, error) {
	if limit < 1 || limit > 101 || len(after) > 128 {
		return nil, repository.ErrResourceRange
	}
	where, args, e := mailboxFilterSQL(id, f)
	if e != nil {
		return nil, e
	}
	table, e := s.router.readTable(mysqlMailsTable, id)
	if e != nil {
		return nil, e
	}
	args = append(args, after, limit)
	rows, e := s.router.db.QueryContext(ctx, "SELECT m.mail_id,m.sender_name,m.title,m.gold,m.read_flag,m.claimed_flag,m.created_at,m.expire_at FROM "+table+" m WHERE "+where+" AND BINARY m.mail_id>? ORDER BY BINARY m.mail_id LIMIT ?", args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []repository.MailRecord{}
	for rows.Next() {
		var m repository.MailRecord
		var created, expiry sql.NullTime
		if e = rows.Scan(&m.MailID, &m.SenderName, &m.Title, &m.Gold, &m.Read, &m.Claimed, &created, &expiry); e != nil {
			return nil, e
		}
		m.CreatedAt = scanTime(created)
		m.ExpireAt = scanTime(expiry)
		out = append(out, m)
	}
	return out, rows.Err()
}
