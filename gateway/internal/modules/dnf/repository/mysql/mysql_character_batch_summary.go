package mysql

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var _ repository.CharacterBatchSummaryReader = (*mysqlCharStore)(nil)

func (s *mysqlCharStore) SummariesForCharacters(ctx context.Context, ids []string) ([]repository.CharacterSummary, error) {
	out := []repository.CharacterSummary{}
	if len(ids) > 100 {
		return nil, repository.ErrCharacterSummaryRange
	}
	seen := map[string]bool{}
	tables := map[string][]any{}
	for _, id := range ids {
		n, e := strconv.ParseUint(id, 10, 16)
		if e != nil || n == 0 || strconv.FormatUint(n, 10) != id || seen[id] {
			return nil, repository.ErrCharacterSummaryRange
		}
		seen[id] = true
		table, e := s.router.readTable(mysqlCharTable, id)
		if e != nil {
			return nil, e
		}
		tables[table] = append(tables[table], id)
	}
	total := len(ids)
	for table, ids := range tables {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := append([]any(nil), ids...)
		args = append(args, ids...)
		args = append(args, len(ids)+1)
		rows, e := s.router.db.QueryContext(ctx, "SELECT "+summaryColumns+" FROM "+table+" WHERE delete_flag=0 AND character_id IN ("+marks+") AND BINARY character_id IN ("+marks+") ORDER BY BINARY character_id LIMIT ?", args...)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			c, e := scanCharacterSummary(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			expected, e := s.router.readTable(mysqlCharTable, c.CharacterID)
			if e != nil || expected != table || !seen[c.CharacterID] || c.AccountID == "" || len(out) >= total {
				rows.Close()
				return nil, repository.ErrCharacterSummaryRange
			}
			seen[c.CharacterID] = false
			out = append(out, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CharacterID < out[j].CharacterID })
	return out, nil
}
