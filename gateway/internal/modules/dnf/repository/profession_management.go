package repository

import "context"

type ProfessionManagementState struct {
	Growth SkillPointManagementState
	// Only the supplied profession quest IDs are projected. Quest extras are
	// preserved in place by the writer and do not enter this projection.
	Quests QuestRecord
}
type ProfessionManagementReader interface {
	LoadProfessionManagement(context.Context, string, []int64) (ProfessionManagementState, error)
}
type ProfessionManagementRepository interface {
	ProfessionManagementReader
	SaveProfessionManagement(context.Context, ProfessionManagementState, []int64) error
}
type ProfessionManagementUnitOfWork interface {
	WithinProfessionManagement(context.Context, string, string, func(ProfessionManagementRepository, ManagementReceiptStore) error) error
	ProfessionManagementLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
