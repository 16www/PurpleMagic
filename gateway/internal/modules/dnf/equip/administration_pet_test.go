package equip

import (
	"encoding/binary"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestAdministrativePetUsesMoveConstructor(t *testing.T) {
	raw := buildPetCreatureEquipEntry(26, 63000, 37)
	stack := repository.ItemStack{ItemID: 63000, Count: 1, RawEntry: raw}
	if serial, err := AdministrativePetSerial(3, 26, stack); err != nil || serial != 37 {
		t.Fatalf("move constructor: %d bytes, serial=%d err=%v", len(raw), serial, err)
	}
	// The duplicate scalar at +5 is not the creature identifier.
	binary.LittleEndian.PutUint32(raw[5:9], 99)
	if serial, err := AdministrativePetSerial(3, 26, stack); err != nil || serial != 37 {
		t.Fatal(serial, err)
	}
	for _, n := range []int{0, 1, 27, 28, 42, 44, 46, 48, 119} {
		bad := stack
		bad.RawEntry = make([]byte, n)
		copy(bad.RawEntry, raw)
		if _, err := AdministrativePetSerial(3, 26, bad); err == nil {
			t.Fatalf("accepted row length %d", n)
		}
	}
	legacy := stack
	legacy.RawEntry = raw[:43]
	if serial, err := AdministrativePetSerial(3, 26, legacy); err != nil || serial != 37 {
		t.Fatal(serial, err)
	}
	stack.Extra = map[string]string{"creature_serial_or_handle": "38"}
	if _, err := AdministrativePetSerial(3, 26, stack); err == nil {
		t.Fatal("accepted inconsistent metadata identity")
	}
}

func TestResolveAdministrativePetRejectsAmbiguousAssociation(t *testing.T) {
	stack := repository.ItemStack{ItemID: 63000, Count: 1, Extra: map[string]string{"creature_serial_or_handle": "37"}}
	inv := repository.InventoryRecord{Slots: map[string]repository.ItemStack{"7:0": stack}}
	pets := repository.PetRecord{Entries: map[string]repository.PetEntry{"37": {PetKey: "37", CreatureKey: 37, ItemID: 63000, SourceListType: 7, SourceSlotIndex: 0}}}
	eq := repository.EquipmentRecord{}
	if key, err := ResolveAdministrativePet(pets, inv, eq, 7, 0, stack); err != nil || key != "37" {
		t.Fatal(key, err)
	}
	for _, mutate := range []func(*repository.PetRecord){
		func(p *repository.PetRecord) { p.EquippedKey = "37" },
		func(p *repository.PetRecord) { delete(p.Entries, "37") },
		func(p *repository.PetRecord) { e := p.Entries["37"]; e.SourceSlotIndex = 1; p.Entries["37"] = e },
		func(p *repository.PetRecord) { e := p.Entries["37"]; e.CreatureKey = 38; p.Entries["37"] = e },
		func(p *repository.PetRecord) {
			e := p.Entries["37"]
			e.PetKey = "38"
			e.CreatureKey = 38
			p.Entries["38"] = e
		},
		func(p *repository.PetRecord) { e := p.Entries["37"]; e.SourceSlotIndex = 1; p.Entries["duplicate"] = e },
	} {
		bad := repository.ClonePet(pets)
		mutate(&bad)
		if _, err := ResolveAdministrativePet(bad, inv, eq, 7, 0, stack); err == nil {
			t.Fatal("accepted ambiguous association", bad)
		}
	}
}
