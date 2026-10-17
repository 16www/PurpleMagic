package repository

import (
	"context"
	"time"
)

// PetManagementState is the bounded, selected creature projection used by GM.
// Clear tokens, raw rows, artifacts and other creatures are not loaded for it.
type PetManagementState struct {
	PetKey        string    `json:"petKey"`
	CreatureKey   uint32    `json:"creatureKey"`
	ItemID        int64     `json:"itemId,string"`
	List          byte      `json:"list"`
	Slot          int16     `json:"slot"`
	Name          string    `json:"name"`
	Level         int64     `json:"level,string"`
	Exp           int64     `json:"experience,string"`
	Satiety       byte      `json:"satiety"`
	SatietyMicros int64     `json:"satietyMicros,string"`
	EquippedKey   string    `json:"equippedKey"`
	TownDisplay   bool      `json:"townDisplay"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type PetManagementReader interface {
	PetManagementAt(context.Context, CharacterSummary, byte, int16) (PetManagementState, bool, error)
}

func PetManagementProjection(record PetRecord, key string) PetManagementState {
	entry := record.Entries[key]
	return PetManagementState{PetKey: key, CreatureKey: entry.CreatureKey, ItemID: entry.ItemID, List: entry.SourceListType, Slot: entry.SourceSlotIndex, Name: entry.Name, Level: entry.Level, Exp: entry.Exp, Satiety: entry.Satiety, SatietyMicros: entry.SatietyMicros, EquippedKey: record.EquippedKey, TownDisplay: record.TownDisplay, UpdatedAt: record.UpdatedAt.UTC()}
}
