package kh3

// Inventory is 0x400 two-byte entries: count, then flags.
const (
	InventoryOff   = 0x8F4
	InventoryCount = 0x400

	ItemObtained = 0x01
	ItemNew      = 0x02
	ItemFresh    = ItemObtained | ItemNew // how a just-acquired item reads

	// Equipped-accessory slots. Each entry is 8 bytes: id, item type, _, _,
	// enabled. The id is an AccessoryType index, not an inventory index.
	AccessorySlotOff  = 0xD8
	AccessorySlots    = 8
	EquipEntrySize    = 8
	ItemTypeAccessory = 5
)

// CriticalStartItems is what Critical Mode starts with that other
// difficulties do not: a Soldier's Earring, worth +6 AP.
var CriticalStartItems = map[int]int{256: 1}

func GetItem(p []byte, id int) (count, flags byte) {
	off := InventoryOff + id*2
	return p[off], p[off+1]
}

func SetItem(p []byte, id, count int, flags byte) {
	off := InventoryOff + id*2
	if count > 0xFF {
		count = 0xFF
	}
	p[off] = byte(count)
	p[off+1] = flags
}

// FindEquippedAccessory returns the characters that have accessoryID equipped.
func FindEquippedAccessory(p []byte, accessoryID int) []int {
	var out []int
	for ci := range CharNames {
		base := charOffset(ci) + AccessorySlotOff
		for slot := 0; slot < AccessorySlots; slot++ {
			off := base + slot*EquipEntrySize
			if int(p[off]) == accessoryID && p[off+1] == ItemTypeAccessory {
				out = append(out, ci)
				break
			}
		}
	}
	return out
}
