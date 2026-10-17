package equip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func AdministrativeArtifactColor(slot int16) (string, bool) {
	return petArtifactProjectionKey(slot)
}

// AdministrativeArtifactProjection uses the same durable projection as Move.
func AdministrativeArtifactProjection(entry repository.EquipmentEntry) (repository.ItemStack, error) {
	if !isPetArtifactTarget(entry.SlotIndex) || entry.ItemID <= 0 || len(entry.RawEntry) != currentEquipmentRawEntryWireSize || int64(binary.LittleEndian.Uint32(entry.RawEntry[2:6])) != entry.ItemID {
		return repository.ItemStack{}, ErrPetOwnershipMismatch
	}
	stack := stackFromEntry(entry)
	if stack.Count != 1 {
		return repository.ItemStack{}, ErrPetOwnershipMismatch
	}
	return stack, nil
}

func AdministrativeArtifactMatches(entry repository.EquipmentEntry, stored repository.ItemStack) bool {
	want, err := AdministrativeArtifactProjection(entry)
	if err != nil {
		return false
	}
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(stored)
	return bytes.Equal(a, b)
}
