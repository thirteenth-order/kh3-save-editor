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

// Build returns a structurally valid decrypted save.
func Build(o Options) []byte {
	p := make([]byte, PlainLen)
	copy(p[0:4], kh3.Magic)
	binary.LittleEndian.PutUint32(p[0x04:], FileSize)
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

	binary.LittleEndian.PutUint32(p[0x0C:], crc32.ChecksumIEEE(p[0x10:0x10+FileSize]))
	return p
}

// Slots returns the three saves the generator writes, keyed by slot number:
// slot0 Standard, slot1 Critical, slot2 Beginner, at ascending levels.
func Slots() map[int][]byte {
	out := map[int][]byte{}
	for slot, diff := range map[int]byte{0: 1, 1: 3, 2: 0} {
		o := Default()
		o.Difficulty = diff
		o.Level = byte(6 + slot)
		out[slot] = Build(o)
	}
	return out
}
