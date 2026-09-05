package kh3

import "encoding/binary"

const (
	DifficultyOffset = 0x14

	CharBase = 0x1880
	CharSize = 0x9C0
	Sora     = 0

	AbilityOff   = 0x160
	AbilityCount = 512

	StatAtkBoost = 0x980
	StatMagBoost = 0x981
	StatDefBoost = 0x982
	StatApBoost  = 0x983
	StatHP       = 0x984
	StatMP       = 0x988
	StatFocus    = 0x98C

	AbilityInnateEquipped = 0x0000044B
	AbilityAbsent         = 0x00000444

	Critical = 3
)

// CriticalAbilities are the three default abilities Critical Mode grants,
// and the only abilities any difficulty grants.
var CriticalAbilities = [3]int{0x068, 0x069, 0x06A}

// CharNames indexes the 16 playable-character structs at 0x1880.
var CharNames = [16]string{
	"Sora", "Donald", "Goofy", "Hercules", "Woody", "Buzz", "Rapunzel",
	"Flynn", "Sulley", "Mike", "Marshmallow", "Baymax", "Jack", "Riku",
	"Mickey", "Unused",
}

var Difficulties = map[byte]string{0: "Beginner", 1: "Standard", 2: "Proud", 3: "Critical"}

func charOffset(i int) int { return CharBase + i*CharSize }

func GetDifficulty(p []byte) byte { return p[DifficultyOffset] }

func GetAbility(p []byte, char, id int) uint32 {
	return binary.LittleEndian.Uint32(p[charOffset(char)+AbilityOff+id*4:])
}

func SetAbility(p []byte, char, id int, v uint32) {
	binary.LittleEndian.PutUint32(p[charOffset(char)+AbilityOff+id*4:], v)
}

func GetStat(p []byte, char, off int) int32 {
	return int32(binary.LittleEndian.Uint32(p[charOffset(char)+off:]))
}

func SetStat(p []byte, char, off int, v int32) {
	binary.LittleEndian.PutUint32(p[charOffset(char)+off:], uint32(v))
}

// IsInnatelyOwned reports an ability the character owns outright: bit0 owned,
// bit3 innate, and no equipment source bits above bit 11.
func IsInnatelyOwned(w uint32) bool { return w&1 != 0 && w&8 != 0 && w>>12 == 0 }

func le32(b []byte) uint32       { return binary.LittleEndian.Uint32(b) }
func putLe32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
