package mysql

import (
	"context"
	"database/sql"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func (s *mysqlCharStore) PetManagementAt(ctx context.Context, c repository.CharacterSummary, list byte, slot int16) (repository.PetManagementState, bool, error) {
	var out repository.PetManagementState
	if !((list == 7 && slot >= 0 && slot <= 139) || (list == 3 && slot == 26)) {
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
	entries, err := r.readTable(mysqlPetEntriesTable, c.CharacterID)
	if err != nil {
		return out, false, err
	}
	var updated sql.NullTime
	err = r.db.QueryRowContext(ctx, "SELECT equipped_key,town_display,updated_at FROM "+parent+" WHERE character_id=? AND BINARY character_id=BINARY ?", c.CharacterID, c.CharacterID).Scan(&out.EquippedKey, &out.TownDisplay, &updated)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	out.UpdatedAt = scanTime(updated).UTC()
	rows, err := r.db.QueryContext(ctx, "SELECT pet_key,creature_key,item_id,source_list_type,source_slot_index,LEFT(pet_name,513),pet_level,pet_exp,satiety,satiety_micros FROM "+entries+" WHERE character_id=? AND BINARY character_id=BINARY ? AND source_list_type=? AND source_slot_index=? ORDER BY BINARY pet_key LIMIT 2", c.CharacterID, c.CharacterID, list, slot)
	if err != nil {
		return out, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return out, false, rows.Err()
	}
	err = rows.Scan(&out.PetKey, &out.CreatureKey, &out.ItemID, &out.List, &out.Slot, &out.Name, &out.Level, &out.Exp, &out.Satiety, &out.SatietyMicros)
	if err != nil {
		return out, false, err
	}
	if len(out.Name) > 512 || rows.Next() {
		return out, false, repository.ErrResourceRange
	}
	return out, true, rows.Err()
}
