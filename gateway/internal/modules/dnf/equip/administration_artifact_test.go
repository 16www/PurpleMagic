package equip

import (
	"context"
	"encoding/binary"
	"reflect"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/repository/memory"
)

func TestAdministrativeArtifactMatchesGameplayProjection(t *testing.T) {
	for _, slot := range []int16{27, 28, 29} {
		ctx := context.Background()
		g := memory.NewMemoryGroup()
		raw := make([]byte, 119)
		binary.LittleEndian.PutUint32(raw[2:6], 2747155)
		saveTestInventory(t, ctx, g, repository.InventoryRecord{CharacterID: "77", Slots: map[string]repository.ItemStack{"7:140": {ItemID: 2747155, Count: 1, RawEntry: raw, Extra: map[string]string{"item_kind": "equipment", "opaque": "preserve"}}}})
		saveTestEquipment(t, ctx, g, repository.EquipmentRecord{CharacterID: "77", Entries: map[string]repository.EquipmentEntry{}})
		o, err := NewOwnerWithPlacementValidator(g, &recordingPlacementValidator{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = o.Move(ctx, MoveCommand{SelectedCharacterID: 77, SourceListType: wireListPet, SourceSlotIndex: 140, DestinationListType: wireListEquipment, DestinationSlotIndex: slot, MoveCount: 1})
		if err != nil {
			t.Fatal(err)
		}
		eq := loadTestEquipment(t, ctx, g, "77")
		pets, _, err := g.Pet.Load(ctx, "77")
		if err != nil {
			t.Fatal(err)
		}
		color, _ := AdministrativeArtifactColor(slot)
		entry := eq.Entries[entryKey(slot)]
		before := repository.CloneEquipment(eq)
		if !AdministrativeArtifactMatches(entry, pets.Artifacts[color]) {
			t.Fatal("game projection mismatch", color)
		}
		if !reflect.DeepEqual(before, eq) {
			t.Fatal("projection modified equipment")
		}
		bad := pets.Artifacts[color]
		bad.Count = 2
		if AdministrativeArtifactMatches(entry, bad) {
			t.Fatal("corrupt count accepted")
		}
		entry.RawEntry = entry.RawEntry[:43]
		if _, err := AdministrativeArtifactProjection(entry); err == nil {
			t.Fatal("unsupported summary accepted")
		}
	}
}
