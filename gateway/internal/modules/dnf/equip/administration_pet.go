package equip

import (
	"encoding/binary"
	"strconv"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

// AdministrativePetSerial reuses the current move path's instance identity.
// Worn creature rows have their own layout, distinct from a 119-byte bag row.
func AdministrativePetSerial(list byte, slot int16, stack repository.ItemStack) (int64, error) {
	var serial int64
	if list == wireListPet && slot >= 0 && slot <= 139 {
		if stack.Count != 1 {
			return 0, ErrPetOwnershipMismatch
		}
		serial = petStackSerial(stack)
		if len(stack.RawEntry) > 0 && (len(stack.RawEntry) != 119 || int64(binary.LittleEndian.Uint32(stack.RawEntry[2:6])) != stack.ItemID) {
			return 0, ErrPetOwnershipMismatch
		}
	} else if list == wireListEquipment && slot == petCreatureWornSlot {
		// Persisted creature summaries use 43 bytes; the current move constructor
		// appends four more bytes. Both keep the authoritative serial at +24.
		if len(stack.RawEntry) > 0 && ((len(stack.RawEntry) != 43 && len(stack.RawEntry) != 47) || int64(binary.LittleEndian.Uint32(stack.RawEntry[1:5])) != stack.ItemID) {
			return 0, ErrPetOwnershipMismatch
		}
		serial = petEquipmentSerial(repository.EquipmentEntry{SlotIndex: slot, ItemID: stack.ItemID, RawEntry: stack.RawEntry, Extra: stack.Extra})
		if extra := petStackSerial(stack); extra > 0 && serial != extra {
			return 0, ErrPetOwnershipMismatch
		}
	} else {
		return 0, ErrPetOwnershipMismatch
	}
	if serial <= 0 || serial > int64(^uint32(0)) || stack.ItemID <= 0 {
		return 0, ErrPetOwnershipMismatch
	}
	return serial, nil
}

// ResolveAdministrativePet validates the selected instance against typed state
// and every physical creature location before removal. It never reconciles data.
func ResolveAdministrativePet(pets repository.PetRecord, inventory repository.InventoryRecord, equipment repository.EquipmentRecord, list byte, slot int16, stack repository.ItemStack) (string, error) {
	serial, err := AdministrativePetSerial(list, slot, stack)
	if err != nil {
		return "", err
	}
	key, err := findPetEntryKey(pets, serial, stack.ItemID)
	if err != nil {
		return "", err
	}
	entry := pets.Entries[key]
	if entry.SourceListType != list || entry.SourceSlotIndex != slot || ((list == 3) != (pets.EquippedKey == key)) {
		return "", ErrPetOwnershipMismatch
	}
	for otherKey, other := range pets.Entries {
		if otherKey != key && other.SourceListType == list && other.SourceSlotIndex == slot {
			return "", ErrPetOwnershipMismatch
		}
	}
	locations := 0
	for rawKey, v := range inventory.Slots {
		parts := strings.Split(rawKey, ":")
		if len(parts) != 2 || parts[0] != "7" {
			continue
		}
		i, e := strconv.Atoi(parts[1])
		if e != nil || i < 0 || i > 139 {
			continue
		}
		if petStackSerial(v) == serial {
			locations++
			if list != 7 || int(slot) != i || v.ItemID != stack.ItemID || v.Count != 1 || rawKey != inventoryKey(7, slot) {
				return "", ErrPetOwnershipMismatch
			}
		}
	}
	for _, v := range equipment.Entries {
		if v.SlotIndex != petCreatureWornSlot {
			continue
		}
		if petEquipmentSerial(v) == serial {
			locations++
			if list != 3 || v.ItemID != stack.ItemID {
				return "", ErrPetOwnershipMismatch
			}
		}
	}
	if locations != 1 {
		return "", ErrPetOwnershipMismatch
	}
	return key, nil
}
