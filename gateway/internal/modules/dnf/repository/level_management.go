package repository

import "context"

// Level changes use the character/point-ledger projection, plus exactly these
// PVF-derived stats. Unrelated mirrors and all learned-skill rows stay outside it.
var LevelStatKeys = [...]string{
	"stat_hp_max", "stat_mp_max", "stat_strength", "stat_intelligence", "stat_vitality", "stat_spirit",
	"stat_physical_attack", "stat_physical_defense", "stat_magical_attack", "stat_magical_defense", "stat_independent_attack",
	"stat_fire_resistance", "stat_water_resistance", "stat_dark_resistance", "stat_light_resistance", "stat_inventory_limit",
	"stat_hp_regen_speed", "stat_mp_regen_speed", "stat_move_speed", "stat_attack_speed", "stat_cast_speed", "stat_hit_recovery", "stat_jump_power", "stat_weight",
}

type LevelManagementReader interface {
	LoadLevelManagement(context.Context, string) (SkillPointManagementState, error)
}
type LevelManagementRepository interface {
	LevelManagementReader
	SaveLevelManagement(context.Context, SkillPointManagementState) error
}
type LevelManagementUnitOfWork interface {
	WithinLevelManagement(context.Context, string, string, func(LevelManagementRepository, ManagementReceiptStore) error) error
	LevelManagementLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
