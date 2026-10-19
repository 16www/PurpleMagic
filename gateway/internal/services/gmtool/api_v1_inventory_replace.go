package gmtool

import (
	"encoding/binary"
	"errors"
	"strconv"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

// Replace an ID within the same PVF item family. Instance bytes, binding,
// expiration and serial are retained; pet links and fixed account counters use
// separate domain contracts and cannot be reinterpreted as another item.
func (s *Server) v1ReplaceInventoryItem(v adminops.InventorySelection, stack repository.ItemStack, old itemEntry, value string, at time.Time) (repository.ItemStack, error) {
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != value || v.Container == "shared" || adminops.InventoryPetTarget(v) || adminops.InventoryArtifactTarget(v) {
		return repository.ItemStack{}, adminops.ErrInventoryInput
	}
	next, err := s.itemcfgEntry(int64(id))
	if err != nil {
		return repository.ItemStack{}, err
	}
	if next.Invalid || old.Tag == "" || old.Kind != next.Kind || old.Tag != next.Tag || old.Segment != next.Segment || old.Special != next.Special || old.SlotStart != next.SlotStart || old.SlotEnd != next.SlotEnd {
		return repository.ItemStack{}, errors.New("请选择同类物品：目标物品的类型、部位或存储方式与当前实例不匹配")
	}
	if next.Kind != "equipment" && next.Kind != "stackable" {
		return repository.ItemStack{}, adminops.ErrInventoryInput
	}
	if next.Kind == "stackable" && stack.Count > v1InventoryCountLimit(next, v.Container) {
		return repository.ItemStack{}, errors.New("当前数量超过目标物品堆叠上限，请先调整数量")
	}
	if stack.ItemID == int64(id) {
		return repository.ItemStack{}, errors.New("物品 ID 未改变")
	}
	stack.ItemID = int64(id)
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	if len(stack.RawEntry) != 0 {
		if len(stack.RawEntry) != itemcfgItemListEntrySize {
			return repository.ItemStack{}, adminops.ErrInventoryInput
		}
		binary.LittleEndian.PutUint32(stack.RawEntry[2:6], uint32(id))
	}
	extra := make(map[string]string, len(stack.Extra)+3)
	for k, val := range stack.Extra {
		extra[k] = val
	}
	extra["pvf_path"] = next.documentPath()
	if next.Kind == "stackable" {
		extra["stackable_type"] = next.Tag
		extra["stack_limit"] = strconv.FormatInt(next.StackLimit, 10)
	}
	stack.Extra = extra
	return itemcfgStampAuditKey(stack, at), nil
}
