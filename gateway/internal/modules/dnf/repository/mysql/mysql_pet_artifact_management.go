package mysql

import (
	"context"
	"database/sql"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func (s *mysqlCharStore) PetArtifactManagementAt(ctx context.Context, c repository.CharacterSummary, color string) (repository.PetArtifactManagementState, bool, error) {
	out := repository.PetArtifactManagementState{Color: color}
	if color != "red" && color != "blue" && color != "green" {
		return out, false, repository.ErrResourceRange
	}
	snapshot, close, err := s.resourceSnapshot(ctx)
	if err != nil {
		return out, false, err
	}
	defer close()
	current, found, err := snapshot.FindSummary(ctx, c.CharacterID)
	if err != nil {
		return out, false, err
	}
	if !found || current.AccountID != c.AccountID {
		return out, false, repository.ErrResourceRange
	}
	r := snapshot.router
	parent, err := r.readTable(mysqlPetTable, c.CharacterID)
	if err != nil {
		return out, false, err
	}
	items, err := r.readTable(mysqlPetArtifactsTable, c.CharacterID)
	if err != nil {
		return out, false, err
	}
	extra, err := r.readTable(mysqlPetArtifactExtraTable, c.CharacterID)
	if err != nil {
		return out, false, err
	}
	var updated, expires sql.NullTime
	err = r.db.QueryRowContext(ctx, "SELECT updated_at FROM "+parent+" WHERE character_id=? AND BINARY character_id=BINARY ?", c.CharacterID, c.CharacterID).Scan(&updated)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.UpdatedAt = scanTime(updated).UTC()
	var rawSize int
	where := "character_id=? AND BINARY character_id=BINARY ? AND entry_key=? AND BINARY entry_key=BINARY ?"
	args := []any{c.CharacterID, c.CharacterID, color, color}
	err = r.db.QueryRowContext(ctx, "SELECT item_id,item_count,bind_flag,expire_at,IF(OCTET_LENGTH(raw_entry)<=4096,raw_entry,NULL),COALESCE(OCTET_LENGTH(raw_entry),0) FROM "+items+" WHERE "+where, args...).Scan(&out.Stack.ItemID, &out.Stack.Count, &out.Stack.Bind, &expires, &out.Stack.RawEntry, &rawSize)
	if err == sql.ErrNoRows {
		return out, true, nil
	}
	if err != nil {
		return out, false, err
	}
	if rawSize > 4096 {
		return out, false, repository.ErrResourceRange
	}
	out.Present = true
	out.Stack.ExpireAt = scanTime(expires)
	rows, err := r.db.QueryContext(ctx, "SELECT extra_key,LEFT(extra_value,4097) FROM "+extra+" WHERE "+where+" ORDER BY extra_key LIMIT 257", args...)
	if err != nil {
		return out, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err = rows.Scan(&k, &v); err != nil {
			return out, false, err
		}
		if len(out.Stack.Extra) >= 256 || len(v) > 4096 {
			return out, false, repository.ErrResourceRange
		}
		if out.Stack.Extra == nil {
			out.Stack.Extra = map[string]string{}
		}
		out.Stack.Extra[k] = v
	}
	return out, true, rows.Err()
}
