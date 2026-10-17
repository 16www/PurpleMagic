package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const skillPointOperationsTable = "skill_point_management_operations"

var _ repository.SkillPointManagementReader = (*mysqlCharStore)(nil)
var _ repository.SkillPointManagementUnitOfWork = (*mysqlCharStore)(nil)

func (s *mysqlCharStore) LoadSkillPointManagement(ctx context.Context, id string) (repository.SkillPointManagementState, error) {
	snapshot, close, err := s.resourceSnapshot(ctx)
	if err != nil {
		return repository.SkillPointManagementState{}, err
	}
	defer close()
	return snapshot.loadSkillPointManagement(ctx, id)
}
func (s *mysqlCharStore) loadSkillPointManagement(ctx context.Context, id string) (repository.SkillPointManagementState, error) {
	var v repository.SkillPointManagementState
	if id == "" || len(id) > 128 {
		return v, repository.ErrResourceRange
	}
	r := s.router
	ct, err := r.readTable(mysqlCharTable, id)
	if err != nil {
		return v, err
	}
	st, err := r.readTable(mysqlCharacterStatsTable, id)
	if err != nil {
		return v, err
	}
	field := func(k string) string {
		return "(SELECT stat_value FROM " + st + " WHERE character_id=c.character_id AND stat_key='" + k + "')"
	}
	deleted, sp, tp := "c.delete_flag", "c.bonus_sp", "c.bonus_tp"
	if !characterMirrorStatsAuthoritative() {
		deleted = "COALESCE(" + field("delete_flag") + ",c.delete_flag)"
		sp = "COALESCE(" + field("bonus_sp") + ",c.bonus_sp)"
		tp = "COALESCE(" + field("bonus_tp") + ",c.bonus_tp)"
	}
	var bonusSP, bonusTP, sequence, deleteFlag int64
	var updated sql.NullTime
	err = r.db.QueryRowContext(ctx, r.selectQuery("SELECT c.character_id,c.account_id,c.level,"+deleted+",c.updated_at,"+sp+","+tp+",COALESCE("+field("gm_skill_points_sequence_v1")+",0) FROM "+ct+" c WHERE c.character_id=? AND BINARY c.character_id=BINARY ?"), id, id).Scan(&v.Character.CharacterID, &v.Character.AccountID, &v.Character.Level, &deleteFlag, &updated, &bonusSP, &bonusTP, &sequence)
	if err != nil {
		return v, err
	}
	v.Character.UpdatedAt = scanTime(updated)
	v.Character.Stats = map[string]int64{"bonus_sp": bonusSP, "bonus_tp": bonusTP, "delete_flag": deleteFlag, "gm_skill_points_sequence_v1": sequence}
	skills, err := r.readTable(mysqlSkillTable, id)
	if err != nil {
		return v, err
	}
	err = r.db.QueryRowContext(ctx, r.selectQuery("SELECT character_id,total_sp,remaining_sp,total_tp,remaining_tp,synced_level,updated_at FROM "+skills+" WHERE character_id=? AND BINARY character_id=BINARY ?"), id, id).Scan(&v.Skill.CharacterID, &v.Skill.Points.TotalSP, &v.Skill.Points.RemainingSP, &v.Skill.Points.TotalTP, &v.Skill.Points.RemainingTP, &v.Skill.Points.SyncedLevel, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Found = true
	v.Skill.UpdatedAt = scanTime(updated)
	return v, nil
}

type mysqlSkillPointManagementStore struct {
	base               *mysqlCharStore
	account, character string
}

func (s *mysqlSkillPointManagementStore) LoadSkillPointManagement(ctx context.Context, id string) (repository.SkillPointManagementState, error) {
	if id != s.character {
		return repository.SkillPointManagementState{}, repository.ErrResourceRange
	}
	return s.base.loadSkillPointManagement(ctx, id)
}
func (s *mysqlSkillPointManagementStore) SaveSkillPointManagement(ctx context.Context, v repository.SkillPointManagementState) error {
	c, p := v.Character, v.Skill.Points
	if !v.Found || c.CharacterID != s.character || c.AccountID != s.account || v.Skill.CharacterID != s.character || c.UpdatedAt.IsZero() || v.Skill.UpdatedAt.IsZero() || c.Stats["gm_skill_points_sequence_v1"] <= 0 || p.SyncedLevel != c.Level || p.TotalSP < 0 || p.TotalSP > math.MaxUint16 || p.TotalTP < 0 || p.TotalTP > math.MaxUint16 || p.RemainingSP < 0 || p.RemainingSP > p.TotalSP || p.RemainingTP < 0 || p.RemainingTP > p.TotalTP {
		return repository.ErrResourceRange
	}
	for _, k := range []string{"bonus_sp", "bonus_tp"} {
		n, ok := c.Stats[k]
		if !ok || n < 0 || n > math.MaxUint16 {
			return repository.ErrResourceRange
		}
	}
	r := s.base.router
	ct, err := r.writeTable(mysqlCharTable, s.character)
	if err != nil {
		return err
	}
	st, err := r.writeTable(mysqlCharacterStatsTable, s.character)
	if err != nil {
		return err
	}
	kt, err := r.writeTable(mysqlSkillTable, s.character)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, "UPDATE "+ct+" SET bonus_sp=?,bonus_tp=?,updated_at=? WHERE character_id=? AND BINARY character_id=BINARY ? AND BINARY account_id=BINARY ? AND delete_flag=0", c.Stats["bonus_sp"], c.Stats["bonus_tp"], c.UpdatedAt.UTC(), s.character, s.character, s.account)
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
	if !characterMirrorStatsAuthoritative() {
		for _, k := range []string{"bonus_sp", "bonus_tp"} {
			if _, err = r.db.ExecContext(ctx, "UPDATE "+st+" SET stat_value=? WHERE character_id=? AND stat_key=?", c.Stats[k], s.character, k); err != nil {
				return err
			}
		}
	}
	if _, err = r.db.ExecContext(ctx, "INSERT INTO "+st+"(character_id,stat_key,stat_value) VALUES(?,'gm_skill_points_sequence_v1',?) ON DUPLICATE KEY UPDATE stat_value=VALUES(stat_value)", s.character, c.Stats["gm_skill_points_sequence_v1"]); err != nil {
		return err
	}
	result, err = r.db.ExecContext(ctx, "UPDATE "+kt+" SET total_sp=?,remaining_sp=?,total_tp=?,remaining_tp=?,synced_level=?,updated_at=? WHERE character_id=? AND BINARY character_id=BINARY ?", p.TotalSP, p.RemainingSP, p.TotalTP, p.RemainingTP, p.SyncedLevel, v.Skill.UpdatedAt.UTC(), s.character, s.character)
	if err != nil {
		return err
	}
	n, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return repository.ErrResourceRange
	}
	return nil
}
func (s *mysqlCharStore) WithinSkillPointManagement(ctx context.Context, account, id string, fn func(repository.SkillPointManagementRepository, repository.ManagementReceiptStore) error) error {
	if fn == nil {
		return repository.ErrResourceRange
	}
	return s.withinCharacterManagementRouter(ctx, account, id, skillPointOperationsTable, func(r mysqlRouter, receipts repository.ManagementReceiptStore) error {
		// Take both parent locks before the character stat subqueries establish
		// a snapshot. The game can update skills without locking the character.
		// Waiting after that snapshot causes MariaDB 1020 on the newer skill row.
		table, err := r.writeTable(mysqlSkillTable, id)
		if err != nil {
			return err
		}
		var locked string
		err = r.db.QueryRowContext(ctx, "SELECT character_id FROM "+table+" WHERE character_id=? AND BINARY character_id=BINARY ? FOR UPDATE", id, id).Scan(&locked)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return fn(&mysqlSkillPointManagementStore{base: &mysqlCharStore{mysqlStoreBase: mysqlStoreBase{router: r}}, account: account, character: id}, receipts)
	})
}
func (s *mysqlCharStore) SkillPointManagementLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return s.characterManagementLog(ctx, id, before, limit, skillPointOperationsTable)
}
