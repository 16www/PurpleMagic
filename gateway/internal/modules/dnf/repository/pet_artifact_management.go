package repository

import (
	"context"
	"time"
)

type PetArtifactManagementState struct {
	Color     string
	Present   bool
	Stack     ItemStack
	UpdatedAt time.Time
}

type PetArtifactManagementReader interface {
	PetArtifactManagementAt(context.Context, CharacterSummary, string) (PetArtifactManagementState, bool, error)
}

func PetArtifactManagementProjection(record PetRecord, color string) PetArtifactManagementState {
	stack, found := record.Artifacts[color]
	return PetArtifactManagementState{Color: color, Present: found, Stack: stack, UpdatedAt: record.UpdatedAt.UTC()}
}
