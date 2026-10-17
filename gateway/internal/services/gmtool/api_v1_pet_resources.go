package gmtool

import (
	"context"
	"errors"
	"strconv"

	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/equip"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func v1AttachPetState(ctx context.Context, reader repository.CharacterResourceReader, c repository.CharacterSummary, v adminops.InventorySelection, stack repository.ItemStack, data map[string]any) (bool, error) {
	if adminops.InventoryArtifactTarget(v) {
		return v1AttachArtifactState(ctx, reader, c, v, stack, data)
	}
	if !adminops.InventoryPetTarget(v) {
		return false, nil
	}
	pets, ok := reader.(repository.PetManagementReader)
	if !ok {
		return false, errors.New("pet resource reader unavailable")
	}
	spec, _ := repository.ResourceContainer(v.Container)
	state, found, err := pets.PetManagementAt(ctx, c, byte(spec.List), int16(v.Slot))
	if err != nil {
		return false, err
	}
	if found {
		serial, e := equip.AdministrativePetSerial(byte(spec.List), int16(v.Slot), stack)
		petSerial := uint64(state.CreatureKey)
		if petSerial == 0 {
			petSerial, _ = strconv.ParseUint(state.PetKey, 0, 32)
		}
		if e == nil && serial == int64(petSerial) && stack.ItemID == state.ItemID && ((spec.List == 3) == (state.EquippedKey == state.PetKey)) {
			data["pet"] = state
			data["relatedRevision"] = adminops.InventoryPetRevision(c.AccountID, c.CharacterID, state)
			return true, nil
		}
	}
	data["petLinkError"] = "宠物存档与物品位置或实例编号不一致"
	return false, nil
}
