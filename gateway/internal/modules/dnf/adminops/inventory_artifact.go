package adminops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"dnf90.local/gateway/internal/modules/dnf/equip"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type InventoryArtifactState struct {
	Color string `json:"color"`
	Slot  int    `json:"slot"`
}

func InventoryArtifactTarget(v InventorySelection) bool {
	return v.Container == "equipped" && v.Slot >= 27 && v.Slot <= 29
}

func InventoryArtifactRevision(account, character string, state repository.PetArtifactManagementState) string {
	raw, _ := json.Marshal([]any{account, character, state})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func artifactEquipmentEntry(slot int, stack repository.ItemStack) repository.EquipmentEntry {
	return repository.EquipmentEntry{SlotIndex: int16(slot), ItemID: stack.ItemID, Bind: stack.Bind, ExpireAt: stack.ExpireAt, RawEntry: stack.RawEntry, Extra: stack.Extra}
}

func (p *inventoryPetChange) updateArtifact(v InventorySelection, view repository.InventoryRecord) error {
	color, ok := p.artifacts[v.Slot]
	if !ok || !InventoryArtifactTarget(v) {
		return nil
	}
	_, key, _ := InventoryTarget(v)
	stack, exists := view.Slots[key]
	if !exists {
		delete(p.record.Artifacts, color)
		return nil
	}
	projected, err := equip.AdministrativeArtifactProjection(artifactEquipmentEntry(v.Slot, stack))
	if err != nil {
		return err
	}
	p.record.Artifacts[color] = projected
	return nil
}
