package mysql

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const expertJobOperationsTable = "expert_job_management_operations"
const expertJobStatsFilter = "(BINARY LEFT(stat_key,18)=BINARY 'expert_job_recipe_' OR stat_key IN ('expert_job_machine_grade','expert_job_machine_endurance','expert_job_give_up_state'))"

func (s *mysqlCharStore) LoadExpertJobManagement(ctx context.Context, id string, ids []int64) (repository.ExpertJobManagementState, error) {
	snap, close, e := s.resourceSnapshot(ctx)
	if e != nil {
		return repository.ExpertJobManagementState{}, e
	}
	defer close()
	return snap.loadExpertJobManagement(ctx, id, ids)
}
func (s *mysqlCharStore) loadExpertJobManagement(ctx context.Context, id string, ids []int64) (repository.ExpertJobManagementState, error) {
	var v repository.ExpertJobManagementState
	if id == "" || len(id) > 128 || len(ids) > 256 {
		return v, repository.ErrResourceRange
	}
	filter, args, e := professionQuestFilter(ids)
	if e != nil {
		return v, e
	}
	r := s.router
	t, e := r.readTable(mysqlCharTable, id)
	if e != nil {
		return v, e
	}
	st, e := r.readTable(mysqlCharacterStatsTable, id)
	if e != nil {
		return v, e
	}
	field := func(key string) string {
		return "(SELECT stat_value FROM " + st + " WHERE character_id=c.character_id AND stat_key='" + key + "')"
	}
	column := func(key string) string {
		if characterMirrorStatsAuthoritative() {
			return "c." + key
		}
		return "COALESCE(" + field(key) + ",c." + key + ")"
	}
	var typ, exp, deleted, seq int64
	var at sql.NullTime
	e = r.db.QueryRowContext(ctx, "SELECT c.character_id,c.account_id,c.level,"+column("expert_job_type")+","+column("expert_job_exp")+","+column("delete_flag")+",COALESCE("+field("gm_expert_job_sequence_v1")+",0),c.updated_at FROM "+t+" c WHERE c.character_id=? AND BINARY c.character_id=BINARY ?", id, id).Scan(&v.Character.CharacterID, &v.Character.AccountID, &v.Character.Level, &typ, &exp, &deleted, &seq, &at)
	if e != nil {
		return v, e
	}
	v.Character.Stats = map[string]int64{"expert_job_type": typ, "expert_job_exp": exp, "delete_flag": deleted, "gm_expert_job_sequence_v1": seq}
	v.Character.UpdatedAt = scanTime(at)
	// Recovery reads only identity and columns, before loading recipe/quest rows.
	if ids == nil {
		return v, nil
	}
	rows, e := r.db.QueryContext(ctx, r.selectQuery("SELECT stat_key,stat_value FROM "+st+" WHERE character_id=? AND "+expertJobStatsFilter+" ORDER BY stat_key LIMIT 1025"), id)
	if e != nil {
		return v, e
	}
	count := 0
	for rows.Next() {
		count++
		var key string
		var n int64
		if e = rows.Scan(&key, &n); e != nil {
			break
		}
		v.Character.Stats[key] = n
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return v, e
	}
	if count > 1024 {
		return v, repository.ErrResourceRange
	}
	v.Quests = repository.QuestRecord{CharacterID: id, States: map[int64]repository.QuestState{}, Progress: map[int64]repository.QuestState{}}
	qt, e := r.readTable(mysqlQuestTable, id)
	if e != nil {
		return v, e
	}
	e = r.db.QueryRowContext(ctx, r.selectQuery("SELECT updated_at FROM "+qt+" WHERE character_id=?"), id).Scan(&at)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return v, e
	}
	if e == nil {
		v.Quests.UpdatedAt = scanTime(at)
	}
	qt, e = r.readTable(mysqlQuestStatesTable, id)
	if e != nil {
		return v, e
	}
	args = append([]any{id}, args...)
	rows, e = r.db.QueryContext(ctx, r.selectQuery("SELECT state_group,quest_id,status,progress_value,state_updated_at FROM "+qt+" WHERE character_id=?"+filter+" AND state_group IN ('states','progress') ORDER BY state_group,quest_id"), args...)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var group string
		var id int64
		var q repository.QuestState
		var at sql.NullTime
		if e = rows.Scan(&group, &id, &q.Status, &q.ProgressValue, &at); e != nil {
			return v, e
		}
		q.UpdatedAt = scanTime(at)
		if group == "states" {
			v.Quests.States[id] = q
		} else {
			v.Quests.Progress[id] = q
		}
	}
	return v, rows.Err()
}

type mysqlExpertJobManagementStore struct {
	base               *mysqlCharStore
	account, character string
	ids                []int64
	oldType            int64
}

func (s *mysqlExpertJobManagementStore) LoadExpertJobManagement(ctx context.Context, id string, ids []int64) (repository.ExpertJobManagementState, error) {
	if id != s.character {
		return repository.ExpertJobManagementState{}, repository.ErrResourceRange
	}
	v, e := s.base.loadExpertJobManagement(ctx, id, ids)
	if e == nil {
		s.ids = append([]int64(nil), ids...)
		s.oldType = v.Character.Stats["expert_job_type"]
	}
	return v, e
}
func (s *mysqlExpertJobManagementStore) SaveExpertJobManagement(ctx context.Context, v repository.ExpertJobManagementState, reset bool, target []int64) error {
	c := v.Character
	typ, exp, seq := c.Stats["expert_job_type"], c.Stats["expert_job_exp"], c.Stats["gm_expert_job_sequence_v1"]
	if c.CharacterID != s.character || c.AccountID != s.account || typ < 0 || typ > 4 || exp < 0 || exp > math.MaxUint32 || (typ == 0 && exp != 0) || seq <= 0 || c.UpdatedAt.IsZero() || reset != (typ != s.oldType) {
		return repository.ErrResourceRange
	}
	if _, _, e := professionQuestFilter(target); e != nil {
		return e
	}
	allowed := map[int64]bool{}
	for _, id := range s.ids {
		allowed[id] = true
	}
	for _, id := range target {
		if !allowed[id] {
			return repository.ErrResourceRange
		}
	}
	r := s.base.router
	t, e := r.writeTable(mysqlCharTable, s.character)
	if e != nil {
		return e
	}
	st, e := r.writeTable(mysqlCharacterStatsTable, s.character)
	if e != nil {
		return e
	}
	result, e := r.db.ExecContext(ctx, "UPDATE "+t+" SET expert_job_type=?,expert_job_exp=?,updated_at=? WHERE character_id=? AND BINARY account_id=BINARY ? AND delete_flag=0", typ, exp, c.UpdatedAt, s.character, s.account)
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
		for _, key := range []string{"expert_job_type", "expert_job_exp"} {
			if _, e = r.db.ExecContext(ctx, "UPDATE "+st+" SET stat_value=? WHERE character_id=? AND stat_key=?", c.Stats[key], s.character, key); e != nil {
				return e
			}
		}
	}
	if _, e = r.db.ExecContext(ctx, "INSERT INTO "+st+"(character_id,stat_key,stat_value) VALUES(?,'gm_expert_job_sequence_v1',?) ON DUPLICATE KEY UPDATE stat_value=VALUES(stat_value)", s.character, seq); e != nil {
		return e
	}
	if !reset {
		return nil
	}
	if _, e = r.db.ExecContext(ctx, "DELETE FROM "+st+" WHERE character_id=? AND (BINARY LEFT(stat_key,18)=BINARY 'expert_job_recipe_' OR stat_key IN ('expert_job_machine_grade','expert_job_machine_endurance'))", s.character); e != nil {
		return e
	}
	qt, e := r.writeTable(mysqlQuestTable, s.character)
	if e != nil {
		return e
	}
	qs, e := r.writeTable(mysqlQuestStatesTable, s.character)
	if e != nil {
		return e
	}
	qe, e := r.writeTable(mysqlQuestExtraTable, s.character)
	if e != nil {
		return e
	}
	selected := map[int64]bool{}
	for _, id := range target {
		selected[id] = true
	}
	// Other transition branches are removed just as in the game's give-up path;
	// completion metadata for retained target rows stays in place.
	for _, id := range s.ids {
		if selected[id] {
			continue
		}
		for _, table := range []string{qe, qs} {
			if _, e = r.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE character_id=? AND quest_id=? AND state_group IN ('states','progress')", s.character, id); e != nil {
				return e
			}
		}
	}
	for _, id := range target {
		for _, group := range []string{"states", "progress"} {
			m := v.Quests.States
			if group == "progress" {
				m = v.Quests.Progress
			}
			_, exists := m[id]
			if group == "progress" && !exists {
				continue
			}
			if _, e = r.db.ExecContext(ctx, "INSERT INTO "+qs+"(character_id,state_group,quest_id,status,progress_value,state_updated_at) VALUES(?,?,?,'completed',0,?) ON DUPLICATE KEY UPDATE status=VALUES(status),progress_value=VALUES(progress_value),state_updated_at=VALUES(state_updated_at)", s.character, group, id, c.UpdatedAt); e != nil {
				return e
			}
			if !exists {
				if _, e = r.db.ExecContext(ctx, "INSERT IGNORE INTO "+qe+"(character_id,state_group,quest_id,extra_key,extra_value) VALUES(?,?,?,'gm_override','gm_set_expert_job')", s.character, group, id); e != nil {
					return e
				}
			}
		}
	}
	_, e = r.db.ExecContext(ctx, "INSERT INTO "+qt+"(character_id,updated_at) VALUES(?,?) ON DUPLICATE KEY UPDATE updated_at=VALUES(updated_at)", s.character, c.UpdatedAt)
	return e
}
func (s *mysqlCharStore) WithinExpertJobManagement(ctx context.Context, a, id string, fn func(repository.ExpertJobManagementRepository, repository.ManagementReceiptStore) error) error {
	if fn == nil {
		return repository.ErrResourceRange
	}
	return s.withinCharacterManagementRouter(ctx, a, id, expertJobOperationsTable, func(r mysqlRouter, receipts repository.ManagementReceiptStore) error {
		t, e := r.writeTable(mysqlQuestTable, id)
		if e != nil {
			return e
		}
		var actual string
		e = r.db.QueryRowContext(ctx, "SELECT character_id FROM "+t+" WHERE character_id=? FOR UPDATE", id).Scan(&actual)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		return fn(&mysqlExpertJobManagementStore{base: &mysqlCharStore{mysqlStoreBase: mysqlStoreBase{router: r}}, account: a, character: id}, receipts)
	})
}
func (s *mysqlCharStore) ExpertJobManagementLog(ctx context.Context, id string, before int64, limit int) ([]repository.ManagementLogRow, error) {
	return s.characterManagementLog(ctx, id, before, limit, expertJobOperationsTable)
}
