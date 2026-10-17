package adminops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/equip"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func InventoryPetTarget(v InventorySelection) bool {
	return v.Container == "pets" || (v.Container == "equipped" && v.Slot == 26)
}
func InventoryPetRevision(account, character string, state repository.PetManagementState) string {
	raw, _ := json.Marshal([]any{account, character, state})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type inventoryPetChange struct {
	record    repository.PetRecord
	states    map[string]repository.PetManagementState
	artifacts map[int]string
}

func prepareInventoryPetChange(ctx context.Context, g repository.Group, c InventoryCommand) (*inventoryPetChange, error) {
	selected := []InventorySelection{}
	for _, v := range c.Items {
		if InventoryPetTarget(v) || InventoryArtifactTarget(v) {
			selected = append(selected, v)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	for _, v := range selected {
		if InventoryPetTarget(v) && c.Action != "delete" {
			return nil, &InventoryOperationError{v.Container, v.Slot, ErrInventoryContainer}
		}
	}
	if g.Pet == nil || g.Inventory == nil || g.Equipment == nil {
		return nil, ErrRuntimeUnavailable
	}
	inv, _, err := g.Inventory.Load(ctx, c.CharacterID)
	if err != nil {
		return nil, err
	}
	eq, _, err := g.Equipment.Load(ctx, c.CharacterID)
	if err != nil {
		return nil, err
	}
	record, found, err := g.Pet.Load(ctx, c.CharacterID)
	if err != nil {
		return nil, err
	}
	if !found || record.CharacterID != c.CharacterID {
		return nil, ErrInventorySlot
	}
	plan := &inventoryPetChange{record: repository.ClonePet(record), states: map[string]repository.PetManagementState{}, artifacts: map[int]string{}}
	removed := map[string]bool{}
	for _, v := range selected {
		spec, slotKey, _ := InventoryTarget(v)
		stack, exists := inv.Slots[slotKey]
		if v.Container == "equipped" {
			exists = false
			for _, entry := range eq.Entries {
				if int(entry.SlotIndex) == v.Slot {
					if exists {
						return nil, ErrInventoryInput
					}
					exists = true
					stack = repository.ItemStack{ItemID: entry.ItemID, Count: 1, Bind: entry.Bind, ExpireAt: entry.ExpireAt, RawEntry: entry.RawEntry, Extra: entry.Extra}
				}
			}
		}
		if !exists {
			return nil, &InventoryOperationError{v.Container, v.Slot, ErrInventorySlot}
		}
		if InventoryArtifactTarget(v) {
			color, _ := equip.AdministrativeArtifactColor(int16(v.Slot))
			state := repository.PetArtifactManagementProjection(record, color)
			if !state.Present || !equip.AdministrativeArtifactMatches(artifactEquipmentEntry(v.Slot, stack), state.Stack) {
				return nil, &InventoryOperationError{v.Container, v.Slot, equip.ErrPetOwnershipMismatch}
			}
			if InventoryArtifactRevision(c.AccountID, c.CharacterID, state) != v.RelatedRevision {
				return nil, &InventoryOperationError{v.Container, v.Slot, ErrConflict}
			}
			plan.artifacts[v.Slot] = color
			continue
		}
		key, err := equip.ResolveAdministrativePet(record, inv, eq, byte(spec.List), int16(v.Slot), stack)
		if err != nil {
			return nil, &InventoryOperationError{v.Container, v.Slot, err}
		}
		state := repository.PetManagementProjection(record, key)
		if InventoryPetRevision(c.AccountID, c.CharacterID, state) != v.RelatedRevision {
			return nil, &InventoryOperationError{v.Container, v.Slot, ErrConflict}
		}
		if removed[key] {
			return nil, ErrInventoryInput
		}
		removed[key] = true
		plan.states[slotKey] = state
		delete(plan.record.Entries, key)
		if plan.record.EquippedKey == key {
			plan.record.EquippedKey = ""
		}
	}
	return plan, nil
}

func (p *inventoryPetChange) persist(ctx context.Context, repo repository.PetRepository, at time.Time) error {
	p.record.UpdatedAt = at
	if err := repository.SavePetFields(ctx, repo, p.record, repository.PetFieldEntries, repository.PetFieldEquipped); err != nil {
		return err
	}
	got, found, err := repo.Load(ctx, p.record.CharacterID)
	if err != nil {
		return err
	}
	if !found || !reflect.DeepEqual(repository.ClonePet(got), repository.ClonePet(p.record)) {
		return ErrConflict
	}
	return nil
}
