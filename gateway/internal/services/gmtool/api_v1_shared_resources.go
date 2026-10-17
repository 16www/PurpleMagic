package gmtool

import (
	"fmt"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

// Empty fixed resource slots have a known zero balance. The present flag keeps
// this projection distinct from a persisted row, including corrupt stored rows.
func v1SelectedInventoryRow(rows []repository.InventorySlot, container string, slot int) (repository.InventorySlot, bool, bool) {
	if len(rows) > 0 {
		return rows[0], true, true
	}
	if container == "shared" {
		if id, ok := repository.AccountWarehouseItemForSlot(int16(slot)); ok {
			return repository.InventorySlot{Key: fmt.Sprintf("0:%d", slot), Slot: slot, Stack: repository.ItemStack{ItemID: id}}, false, true
		}
	}
	return repository.InventorySlot{}, false, false
}

func v1SharedResourcePage(rows []repository.InventorySlot, after, limit int) ([]repository.InventorySlot, map[int]bool) {
	stored := map[int]repository.InventorySlot{}
	for _, row := range rows {
		stored[row.Slot] = row
	}
	out := []repository.InventorySlot{}
	present := map[int]bool{}
	for slot := int(repository.CrystalWarehouseFirstSlot); slot <= int(repository.SoulWarehouseLastSlot) && len(out) < limit; slot++ {
		if slot <= after {
			continue
		}
		row, ok := stored[slot]
		if !ok {
			row, _, _ = v1SelectedInventoryRow(nil, "shared", slot)
		}
		out = append(out, row)
		present[slot] = ok
	}
	return out, present
}
