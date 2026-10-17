package gmtool

import (
	"context"
	"errors"

	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/equip"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func v1ArtifactDefinitionMatches(v adminops.InventorySelection, entry itemEntry) bool {
	if v.Container != "pet-equipment" && !adminops.InventoryArtifactTarget(v) {
		return true
	}
	if entry.Kind != "equipment" {
		return false
	}
	if adminops.InventoryArtifactTarget(v) {
		color, _ := equip.AdministrativeArtifactColor(int16(v.Slot))
		return entry.Tag == "artifact "+color
	}
	return entry.Tag == "artifact red" || entry.Tag == "artifact blue" || entry.Tag == "artifact green"
}

func v1AttachArtifactState(ctx context.Context, reader repository.CharacterResourceReader, c repository.CharacterSummary, v adminops.InventorySelection, stack repository.ItemStack, data map[string]any) (bool, error) {
	pets, ok := reader.(repository.PetArtifactManagementReader)
	if !ok {
		return false, errors.New("artifact resource reader unavailable")
	}
	color, _ := equip.AdministrativeArtifactColor(int16(v.Slot))
	state, found, err := pets.PetArtifactManagementAt(ctx, c, color)
	if err != nil {
		return false, err
	}
	entry := repository.EquipmentEntry{SlotIndex: int16(v.Slot), ItemID: stack.ItemID, Bind: stack.Bind, ExpireAt: stack.ExpireAt, RawEntry: stack.RawEntry, Extra: stack.Extra}
	if found && state.Present && equip.AdministrativeArtifactMatches(entry, state.Stack) {
		data["petArtifact"] = adminops.InventoryArtifactState{Color: color, Slot: v.Slot}
		data["relatedRevision"] = adminops.InventoryArtifactRevision(c.AccountID, c.CharacterID, state)
		return true, nil
	}
	data["petArtifactLinkError"] = "宠物装备存档与穿戴实例不一致"
	return false, nil
}
