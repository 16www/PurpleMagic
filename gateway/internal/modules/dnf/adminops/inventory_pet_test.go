package adminops

import (
	"strings"
	"testing"
)

func TestArtifactRequiresLinkedRevision(t *testing.T) {
	for _, slot := range []int{27, 28, 29} {
		v := InventorySelection{Container: "equipped", Slot: slot, Revision: strings.Repeat("a", 64)}
		c := InventoryCommand{AccountID: "account", CharacterID: "10", Action: "delete", Actor: "admin", Reason: "remove", PVFChecksum: strings.Repeat("b", 64), Items: []InventorySelection{v}}
		if err := validInventoryCommand(c, false); err == nil {
			t.Fatal("missing linked revision accepted", slot)
		}
		c.Items[0].RelatedRevision = strings.Repeat("c", 64)
		if err := validInventoryCommand(c, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []InventorySelection{{Container: "pets", Slot: 0}, {Container: "equipped", Slot: 26}, {Container: "equipped", Slot: 30}, {Container: "pet-consumables", Slot: 189}} {
		if _, _, err := InventoryTarget(v); err != nil {
			t.Fatalf("unrelated container rejected: %+v %v", v, err)
		}
	}
}
