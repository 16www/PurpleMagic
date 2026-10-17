package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const levelOperationsTable = "level_management_operations"

var _ repository.LevelManagementReader = (*mysqlCharStore)(nil)
var _ repository.LevelManagementUnitOfWork = (*mysqlCharStore)(nil)

func (s *mysqlCharStore) LoadLevelManagement(ctx context.Context, id string) (repository.SkillPointManagementState, error) {
	snapshot, close, e := s.resourceSnapshot(ctx)
	if e != nil {
		return repository.SkillPointManagementState{}, e
	}
	defer close()
	return snapshot.loadLevelManagement(ctx, id)
}
func (s *mysqlCharStore) loadLevelManagement(ctx context.Context, id string) (repository.SkillPointManagementState, error) {
	v, e := s.loadSkillPointManagement(ctx, id)
	if e != nil {
		return v, e
	}
	r := s.router
	ct, e := r.readTable(mysqlCharTable, id)
	if e != nil {
		return v, e
	}
	st, e := r.readTable(mysqlCharacterStatsTable, id)
	if e != nil {
		return v, e
	}
	keys := append([]string{"exp", "grow_type"}, repository.LevelStatKeys[:]...)
	fields := []string{"c.job"}
	dest := []any{&v.Character.Job}
	values := make([]int64, len(keys)+1)
	for i, k := range keys {
		field := "c." + k
		if !characterMirrorStatsAuthoritative() {
			field = "COALESCE((SELECT stat_value FROM " + st + " WHERE character_id=c.character_id AND stat_key='" + k + "')," + field + ")"
		}
		fields = append(fields, field)
		dest = append(dest, &values[i])
	}
	fields = append(fields, "COALESCE((SELECT stat_value FROM "+st+" WHERE character_id=c.character_id AND stat_key='gm_level_sequence_v1'),0)")
	dest = append(dest, &values[len(keys)])
	e = r.db.QueryRowContext(ctx, r.selectQuery("SELECT "+strings.Join(fields, ",")+" FROM "+ct+" c WHERE c.character_id=? AND BINARY c.character_id=BINARY ?"), id, id).Scan(dest...)
	if e != nil {
		return v, e
	}
	for i, k := range keys {
		v.Character.Stats[k] = values[i]
	}
	v.Character.Stats["gm_level_sequence_v1"] = values[len(keys)]
	return v, nil
}

type mysqlLevelManagementStore struct {
	base               *mysqlCharStore
	account, character string
}

func (s *mysqlLevelManagementStore) LoadLevelManagement(ctx context.Context, id string) (repository.SkillPointManagementState, error) {
	if id != s.character {
		return repository.SkillPointManagementState{}, repository.ErrResourceRange
	}
	return s.base.loadLevelManagement(ctx, id)
}
func (s *mysqlLevelManagementStore) SaveLevelManagement(ctx context.Context, v repository.SkillPointManagementState) error {
	c, p := v.Character, v.Skill.Points
	if !v.Found || c.CharacterID != s.character || c.AccountID != s.account || v.Skill.CharacterID != s.character || c.Level < 1 || c.Level > 90 || c.UpdatedAt.IsZero() || v.Skill.UpdatedAt.IsZero() || c.Stats["exp"] < 0 || c.Stats["exp"] > math.MaxUint32 || c.Stats["gm_level_sequence_v1"] <= 0 || p.SyncedLevel != c.Level || p.TotalSP < 0 || p.TotalSP > math.MaxUint16 || p.TotalTP < 0 || p.TotalTP > math.MaxUint16 || p.RemainingSP < 0 || p.RemainingSP > p.TotalSP || p.RemainingTP < 0 || p.RemainingTP > p.TotalTP {
		return repository.ErrResourceRange
	}
	r := s.base.router
	ct, e := r.writeTable(mysqlCharTable, s.character)
	if e != nil {
		return e
	}
	st, e := r.writeTable(mysqlCharacterStatsTable, s.character)
	if e != nil {
		return e
	}
	kt, e := r.writeTable(mysqlSkillTable, s.character)
	if e != nil {
		return e
	}
	keys := append([]string{"exp"}, repository.LevelStatKeys[:]...)
	sets := []string{"level=?", "updated_at=?"}
	args := []any{c.Level, c.UpdatedAt.UTC()}
	for _, key := range keys {
		value, ok := c.Stats[key]
		if !ok {
			return repository.ErrResourceRange
		}
		sets = append(sets, key+"=?")
		args = append(args, value)
	}
	args = append(args, s.character, s.character, s.account)
	result, e := r.db.ExecContext(ctx, "UPDATE "+ct+" SET "+strings.Join(sets, ",")+" WHERE character_id=? AND BINARY character_id=BINARY ? AND BINARY account_id=BINARY ? AND delete_flag=0", args...)
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
	if !characterMirrorStatsAuthoritative() {
		for _, key := range keys {
			if _, e = r.db.ExecContext(ctx, "UPDATE "+st+" SET stat_value=? WHERE character_id=? AND stat_key=?", c.Stats[key], s.character, key); e != nil {
				return e
			}
		}
	}
	if _, e = r.db.ExecContext(ctx, "INSERT INTO "+st+"(character_id,stat_key,stat_value) VALUES(?,'gm_level_sequence_v1',?) ON DUPLICATE KEY UPDATE stat_value=VALUES(stat_value)", s.character, c.Stats["gm_level_sequence_v1"]); e != nil {
		return e
	}
	result, e = r.db.ExecContext(ctx, "UPDATE "+kt+" SET total_sp=?,remaining_sp=?,total_tp=?,remaining_tp=?,synced_level=?,updated_at=? WHERE character_id=? AND BINARY character_id=BINARY ?", p.TotalSP, p.RemainingSP, p.TotalTP, p.RemainingTP, p.SyncedLevel, v.Skill.UpdatedAt.UTC(), s.character, s.character)
	if e != nil {
		return e
	}
	n, e = result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return repository.ErrResourceRange
	}
	return nil
}
func (s *mysqlCharStore) WithinLevelManagement(ctx context.Context, account, id string, fn func(repository.LevelManagementRepository, repository.ManagementReceiptStore) error) error {
	if fn == nil {
		return repository.ErrResourceRange
	}
	return s.withinCharacterManagementRouter(ctx, account, id, levelOperationsTable, func(r mysqlRouter, receipts repository.ManagementReceiptStore) error {
		// As with point management, lock skills before any scalar stat subquery.
		kt, e := r.writeTable(mysqlSkillTable, id)
		if e != nil {
			return e
		}
		var locked string
		e = r.db.QueryRowContext(ctx, "SELECT character_id FROM "+kt+" WHERE character_id=? AND BINARY character_id=BINARY ? FOR UPDATE", id, id).Scan(&locked)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		return fn(&mysqlLevelManagementStore{base: &mysqlCharStore{mysqlStoreBase: mysqlStoreBase{router: r}}, account: account, character: id}, receipts)
	})
}
func (s *mysqlCharStore) LevelManagementLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return s.characterManagementLog(ctx, id, before, limit, levelOperationsTable)
}
