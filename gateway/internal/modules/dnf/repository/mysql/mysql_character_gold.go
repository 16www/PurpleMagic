package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var _ repository.CharacterGoldReader = (*mysqlCharStore)(nil)
var _ repository.CharacterGoldUnitOfWork = (*mysqlCharStore)(nil)

// Only exposed within the character-row transaction, with a fixed owner/scope.
type mysqlCharacterGoldStore struct {
	base               *mysqlCharStore
	account, character string
}

func (s *mysqlCharacterGoldStore) LoadCharacterGold(ctx context.Context, id string) (repository.CharacterRecord, bool, error) {
	if id != s.character {
		return repository.CharacterRecord{}, false, repository.ErrCharacterSummaryRange
	}
	return s.base.LoadCharacterGold(ctx, id)
}
func (s *mysqlCharacterGoldStore) SaveCharacterGold(ctx context.Context, c repository.CharacterRecord) error {
	gold, hasGold := c.Stats["gold"]
	seq := c.Stats["gm_gold_sequence_v1"]
	if c.CharacterID != s.character || c.AccountID != s.account || !hasGold || gold < 0 || gold > math.MaxInt32 || seq <= 0 || c.UpdatedAt.IsZero() {
		return repository.ErrCharacterSummaryRange
	}
	r := s.base.router
	table, err := r.writeTable(mysqlCharTable, s.character)
	if err != nil {
		return err
	}
	stats, err := r.writeTable(mysqlCharacterStatsTable, s.character)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, "UPDATE "+table+" SET gold=?,updated_at=? WHERE character_id=? AND BINARY character_id=BINARY ? AND BINARY account_id=BINARY ? AND delete_flag=0", gold, c.UpdatedAt.UTC(), s.character, s.character, s.account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return repository.ErrCharacterSummaryRange
	}
	if !characterMirrorStatsAuthoritative() {
		if _, err = r.db.ExecContext(ctx, "UPDATE "+stats+" SET stat_value=? WHERE character_id=? AND stat_key='gold'", gold, s.character); err != nil {
			return err
		}
	}
	_, err = r.db.ExecContext(ctx, "INSERT INTO "+stats+" (character_id,stat_key,stat_value) VALUES (?,'gm_gold_sequence_v1',?) ON DUPLICATE KEY UPDATE stat_value=VALUES(stat_value)", s.character, seq)
	return err
}

func (s *mysqlCharStore) LoadCharacterGold(ctx context.Context, id string) (repository.CharacterRecord, bool, error) {
	var c repository.CharacterRecord
	if id == "" || len(id) > 128 {
		return c, false, repository.ErrCharacterSummaryRange
	}
	table, err := s.router.readTable(mysqlCharTable, id)
	if err != nil {
		return c, false, err
	}
	stats, err := s.router.readTable(mysqlCharacterStatsTable, id)
	if err != nil {
		return c, false, err
	}
	// The scalar subquery is bounded by the (character_id, stat_key) primary key.
	field := func(key string) string {
		return "(SELECT stat_value FROM " + stats + " WHERE character_id=c.character_id AND stat_key='" + key + "')"
	}
	gold, deleted := "c.gold", "c.delete_flag"
	if !characterMirrorStatsAuthoritative() {
		gold = "COALESCE(" + field("gold") + ",c.gold)"
		deleted = "COALESCE(" + field("delete_flag") + ",c.delete_flag)"
	}
	var balance, sequence, deleteFlag int64
	var updated sql.NullTime
	query := "SELECT c.character_id,c.account_id," + gold + ",COALESCE(" + field("gm_gold_sequence_v1") + ",0)," + deleted + ",c.updated_at FROM " + table + " c WHERE c.character_id=? AND BINARY c.character_id=BINARY ?"
	err = s.router.db.QueryRowContext(ctx, query, id, id).Scan(&c.CharacterID, &c.AccountID, &balance, &sequence, &deleteFlag, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	c.Stats = map[string]int64{"gold": balance, "gm_gold_sequence_v1": sequence, "delete_flag": deleteFlag}
	c.UpdatedAt = scanTime(updated)
	return c, true, nil
}
