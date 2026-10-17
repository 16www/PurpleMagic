package inventory

import (
	"errors"
	"math"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

// SetAdministrativeStackCount shares gameplay's quantity aliases and raw row
// projection. The caller verifies the item is stackable using its PVF definition.
func SetAdministrativeStackCount(stack repository.ItemStack, count, pvfLimit int64) (repository.ItemStack, error) {
	if count < 1 || pvfLimit < 1 || count > pvfLimit || count > math.MaxInt32 || (len(stack.RawEntry) != 0 && len(stack.RawEntry) != currentItemListEntrySize) {
		return repository.ItemStack{}, errors.New("quantity exceeds the PVF stack limit or uses an unsupported item row")
	}
	stack.Count = count
	updateStackRawAmount(&stack)
	return stack, nil
}

// Shared resources are account balances in fixed slots. Zero removes the row
// in the enclosing transaction; positive balances use the existing game encoder.
func SetAdministrativeSharedCount(slot int16, stack repository.ItemStack, count int64) (repository.ItemStack, error) {
	id, ok := repository.AccountWarehouseItemForSlot(slot)
	if !ok || stack.ItemID != id || count < 0 || count > math.MaxInt32 || (len(stack.RawEntry) != 0 && len(stack.RawEntry) != currentItemListEntrySize) {
		return repository.ItemStack{}, errors.New("invalid shared resource quantity or instance")
	}
	stack.Count = count
	updateStackRawAmount(&stack)
	if count > 0 && len(stack.RawEntry) == 0 {
		if stack.Extra["item_kind"] != "stackable" || stack.Extra["pvf_path"] == "" || stack.Extra["stackable_type"] == "" {
			return repository.ItemStack{}, errors.New("shared resource PVF metadata missing")
		}
		stack.RawEntry = currentRawEntryForStack(slot, stack)
	}
	return stack, nil
}
