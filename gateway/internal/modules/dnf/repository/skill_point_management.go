package repository

import "context"

// A bounded projection: no learned skills, layouts, cooldowns or inventory.
type SkillPointManagementState struct {
	Character CharacterRecord
	Skill     SkillRecord
	Found     bool
}

type SkillPointManagementReader interface {
	LoadSkillPointManagement(context.Context, string) (SkillPointManagementState, error)
}
type SkillPointManagementRepository interface {
	SkillPointManagementReader
	SaveSkillPointManagement(context.Context, SkillPointManagementState) error
}
type SkillPointManagementUnitOfWork interface {
	WithinSkillPointManagement(context.Context, string, string, func(SkillPointManagementRepository, ManagementReceiptStore) error) error
	SkillPointManagementLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
