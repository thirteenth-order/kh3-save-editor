// Package fixture builds synthetic Kingdom Hearts III saves.
//
// Tests and CI never touch a real save: one embeds the owner's SteamID64 and
// their entire playthrough. Everything is built here from scratch under a
// synthetic account instead.
//
// This is a port of the Python generator that produced the golden vectors in
// testdata/. It must stay byte-identical to it, or those vectors stop meaning
// anything. TestGoldenFixturesStillMatch asserts exactly that.
package fixture

import (
	"encoding/binary"
	"hash/crc32"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// Account is the synthetic id every fixture is keyed to. It is deliberately a
// valid-looking SteamID64 that belongs to nobody.
const Account = "76561190000000000"

const (
	PlainLen = 0x20000
	FileSize = PlainLen - 24

	// A full-size save. The short fixture above is a valid save structure and
	// is what the golden vectors were built on, so it must never move; it also
	// stops at 0x20000 and so never reaches the record block at the tail.
	// FullPlainLen is the length a real decrypted slot has, so a test can
	// exercise the far regions without touching the frozen fixture or a real
	// save.
	FullPlainLen = 0x94F4F0
	FullFileSize = 0x94F4D8
)

// Options are the fields a caller may vary. The rest are fixed so that the
// same inputs always produce the same bytes.
type Options struct {
	Difficulty byte
	Level      byte
	HP, MP     int32
}

// Default returns the options the generator used for slot0.
func Default() Options { return Options{Difficulty: 1, Level: 6, HP: 145, MP: 115} }

// Build returns a structurally valid decrypted save. Its bytes are frozen:
// TestGoldenFixturesStillMatch fails if any of them move, because the golden
// vectors were computed from exactly this input.
func Build(o Options) []byte { return build(PlainLen, FileSize, o) }

// BuildFull returns a save the size of a real one, with the tail regions the
// short fixture cannot reach filled in. Nothing in it comes from a real save.
func BuildFull(o Options) []byte {
	p := build(FullPlainLen, FullFileSize, o)

	// Use counters and the bests that pair with them, chosen so the two agree:
	// the attractions with a non-zero count are the ones with a best, which is
	// the relationship the block was identified by.
	for i, uses := range []int{2, 5, 0, 0, 0} {
		kh3.SetAttractionUse(p, i, uses)
		if uses > 0 {
			kh3.SetAttractionHigh(p, i, int32(100+i*11))
		}
	}
	kh3.SetShotlockUse(p, 4, 3)
	kh3.SetShotlockHigh(p, 4, 812)

	kh3.SetRecordScore(p, 0, 4200) // verum_rex_high_score
	kh3.SetRecordScore(p, 8, 3)    // frozen_slider_medals
	kh3.SetFlan(p, 1, kh3.Flan{HighScore: 640, HighScore2: 12, Attempts: 4})
	kh3.SetPhotoMaxCount(p, 200)

	kh3.SetString(p, kh3.StringFields[2], "/Game/Blueprints/Player/Sora")

	// Sparse regions need at least one live entry each, or a document built
	// from this save cannot show what those regions look like when they are
	// used, and the schema test cannot check them against it.
	kh3.SetStoryFlag(p, 12, 40)
	kh3.SetStoryFlag(p, 13, 7)
	kh3.SetMaterial(p, 1, 12)
	kh3.SetMaterial(p, 34, 3)
	kh3.SetKeychainUpgrade(p, 0, 2)

	kh3.SetPartySlot(p, 0, 1)
	kh3.SetPartySlot(p, 1, 2)
	kh3.SetMagic(p, 0, 29) // Fire
	kh3.SetLink(p, 0, 70)  // Meow Wow Balloon
	kh3.SetShortcut(p, 0, 0, 29)

	// Every character gets one slot of each of the four equipment arrays. The
	// real evidence that the per-character stride and the four offsets are all
	// right at once is that a full save names each guest's own weapon, so a
	// fixture that leaves the arrays empty tests none of that.
	for ci := range kh3.CharNames {
		for _, e := range []struct {
			off  int
			slot int
			eq   kh3.Equip
		}{
			{kh3.WeaponSlotOff, 0, kh3.Equip{ID: 1, ItemType: kh3.ItemTypeWeapon, Enabled: true}},
			{kh3.ArmorSlotOff, 0, kh3.Equip{ID: 1, ItemType: kh3.ItemTypeArmor, Enabled: true}},
			{kh3.AccessorySlotOff, 0, kh3.Equip{ID: 1, ItemType: kh3.ItemTypeAccessory, Enabled: true}},
			{kh3.ItemSlotOff, 0, kh3.Equip{ID: 1, ItemType: kh3.ItemTypeConsumable, Enabled: true}},
		} {
			kh3.SetEquip(p, ci, e.off, e.slot, e.eq)
		}
		kh3.SetAI(p, ci, kh3.AI{CombatStyle: 1, AbilityUse: 1, RecoveryUse: 2, RecoveryTargets: 3})
	}

	reseal(p, FullFileSize)
	return p
}

func build(size, fileSize int, o Options) []byte {
	p := make([]byte, size)
	copy(p[0:4], kh3.Magic)
	binary.LittleEndian.PutUint32(p[0x04:], uint32(fileSize))
	binary.LittleEndian.PutUint16(p[0x08:], 5)
	binary.LittleEndian.PutUint16(p[0x0A:], 2)
	p[0x14] = o.Difficulty
	p[0x18] = 4
	binary.LittleEndian.PutUint32(p[0x20:], 5896)
	binary.LittleEndian.PutUint32(p[0x24:], 1896)
	binary.LittleEndian.PutUint32(p[0x28:], 583)
	p[0x2C] = o.Level
	p[0x54] = 67
	binary.LittleEndian.PutUint32(p[0x70:], 276)
	binary.LittleEndian.PutUint16(p[0x5B8:], 7)

	for ci := range kh3.CharNames {
		for aid := 0; aid < kh3.AbilityCount; aid++ {
			kh3.SetAbility(p, ci, aid, kh3.AbilityAbsent)
		}
		// A spread of states so every branch of the swap logic is exercised.
		kh3.SetAbility(p, ci, 0x003, kh3.AbilityInnateEquipped) // innate
		kh3.SetAbility(p, ci, 0x01A, 0x00000449)                // owned, unequipped
		kh3.SetAbility(p, ci, 0x01C, 0x00008443)                // granted by gear
		kh3.SetStat(p, ci, kh3.StatHP, o.HP)
		kh3.SetStat(p, ci, kh3.StatMP, o.MP)
		kh3.SetStat(p, ci, kh3.StatFocus, 100)
	}

	if o.Difficulty == kh3.Critical {
		for _, aid := range kh3.CriticalAbilities {
			kh3.SetAbility(p, kh3.Sora, aid, kh3.AbilityInnateEquipped)
		}
	}

	kh3.SetItem(p, 1, 5, kh3.ItemFresh)  // Potions
	kh3.SetItem(p, 21, 2, kh3.ItemFresh) // AP Boost
	kh3.SetItem(p, 227, 1, 0x0D)         // Ability Ring

	copy(p[0xBBA0:], "/Game/Levels/he/he_02/he_02")
	copy(p[0xBCA0:], "he_02_Lv_Save_02")

	reseal(p, fileSize)
	return p
}

// reseal rebuilds the CRC over the prefix the filesize field bounds, which is
// the one integrity field a decrypted save carries.
func reseal(p []byte, fileSize int) {
	binary.LittleEndian.PutUint32(p[0x0C:], crc32.ChecksumIEEE(p[0x10:0x10+fileSize]))
}

// Slots returns the three saves the generator writes, keyed by slot number:
// slot0 Standard, slot1 Critical, slot2 Beginner, at ascending levels.
func Slots() map[int][]byte { return slots(Build) }

// FullSlots is the same three at the size of a real save, so a fixture tree
// exercises the tail regions the short one stops short of.
func FullSlots() map[int][]byte { return slots(BuildFull) }

func slots(make func(Options) []byte) map[int][]byte {
	out := map[int][]byte{}
	for slot, diff := range map[int]byte{0: 1, 1: 3, 2: 0} {
		o := Default()
		o.Difficulty = diff
		o.Level = byte(6 + slot)
		out[slot] = make(o)
	}
	return out
}
