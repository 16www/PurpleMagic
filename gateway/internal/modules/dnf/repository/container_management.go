package repository

import "context"

// ContainerManagementState is a bounded view of the persisted list headers.
type ContainerManagementState struct {
	Character CharacterRecord
	Settings  SettingsRecord
	Found     bool
}
type ContainerManagementReader interface {
	LoadContainerManagement(context.Context, string) (ContainerManagementState, error)
}
type ContainerManagementRepository interface {
	ContainerManagementReader
	SaveContainerManagement(context.Context, ContainerManagementState, []string) error
}
type ContainerManagementUnitOfWork interface {
	// The transaction locks the character and validates its owner/deletion state.
	WithinContainerManagement(context.Context, string, string, func(ContainerManagementRepository, ManagementReceiptStore) error) error
	ContainerManagementLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}

const ContainerManagementSequence = "gm_container_sequence_v1"

// These are the persisted protocol values; main/avatar are expansion counts.
type ManagedContainer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Key    string `json:"-"`
	Unit   string `json:"unit"`
	Values []int  `json:"values"`
}

func ManagedContainers() []ManagedContainer {
	return []ManagedContainer{
		{ID: "main", Name: "主背包扩展", Key: "main_list_param16", Unit: "扩展格数", Values: []int{0, 8, 16, 24}},
		{ID: "avatars", Name: "装扮栏扩展", Key: "avatar_list_param16", Unit: "扩展格数", Values: []int{0, 7, 14, 21, 28, 35, 42, 49, 56}},
		{ID: "warehouse", Name: "个人仓库", Key: "personal_cargo_list_param16", Unit: "格", Values: []int{8, 24, 40, 56, 72, 88, 104, 120, 136, 152, 168, 184, 200}},
	}
}
func ContainerManagementKeys() []string {
	return []string{"main_list_param16", "avatar_list_param16", "personal_cargo_list_param16", "account_cargo_selection_key", "account_cargo_value32", ContainerManagementSequence}
}
