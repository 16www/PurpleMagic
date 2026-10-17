package repository

import "context"

type ExpertJobManagementState struct {
	Character CharacterRecord
	Quests    QuestRecord
}
type ExpertJobManagementReader interface {
	LoadExpertJobManagement(context.Context, string, []int64) (ExpertJobManagementState, error)
}
type ExpertJobManagementRepository interface {
	ExpertJobManagementReader
	SaveExpertJobManagement(context.Context, ExpertJobManagementState, bool, []int64) error
}
type ExpertJobManagementUnitOfWork interface {
	WithinExpertJobManagement(context.Context, string, string, func(ExpertJobManagementRepository, ManagementReceiptStore) error) error
	ExpertJobManagementLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
