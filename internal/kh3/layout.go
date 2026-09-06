package kh3

import "encoding/binary"

// The rest of the map. save.go covers difficulty, stats and abilities, which
// is what the difficulty swap needs; this file covers everything else a save
// stores about a playthrough.
//
// Every offset here was read back out of a real Critical save and checked
// against what the game shows, except KeychainUpgradeOff, which is noted
// below. Offsets are for the container this tool targets, the one whose
// filesize field is 0x94F4D8. They match KHSave.Lib3's SaveKh3u109 up to the
// link array; past that the two builds diverge and this file stops.
const (
	// Global arrays.
	PartyOff   = 0x32 // 5 PartyCharacters ids, Sora is implicit
	PartySlots = 5

	DLCSaveIconOff = 0x68 // CharacterIcons id, twin of SaveIcon at 0x60

	AttractionUseOff   = 0x696 // u16 per RecordAttractions entry
	AttractionUseCount = 5
	ShotlockUseOff     = 0x6D0 // u16 per RecordShotlocks entry
	ShotlockUseCount   = 30

	MaterialOff   = 0x165E // u16 per Materials entry
	MaterialCount = 100

	CrabsOff = 0x17EC // i32, the 100 lucky emblems' cousin

	// The munny ledger. Neither offset is upstream's; both were measured, and
	// the measurement is an identity rather than a resemblance: across all five
	// sample saves u32(0x840) - u32(0x844) equals the munny at 0x28 exactly.
	//
	//	earned  100  1177  1183  1895  1895
	//	spent     0   600   600   600   600
	//	munny   100   577   583  1295  1295
	//
	// So 0x28 is a balance and these two are the ledger it comes from. Whether
	// the game recomputes 0x28 from the pair on load or merely cross-checks it
	// is not known -- that needs the game -- which is exactly why Patch keeps
	// all three consistent instead of writing 0x28 and hoping.
	MunnyEarnedOff = 0x840 // u32, munny earned to date
	MunnySpentOff  = 0x844 // u32, munny spent to date

	// Two fields the format stores a second copy of, and only two: a sweep of
	// every mapped scalar that varies across the five samples, matching each
	// one's whole five-value sequence against every offset in the file, finds
	// exactly these. playtime, exp, munny, level, location, difficulty,
	// saves_count and the per-character HP and MP have no second copy at all.
	//
	// SaveIconMirrorOff is the stronger of the two, because save_icon is the
	// one mapped field that does not simply climb: it reads 12, 11, 16, 11, 12
	// across the samples in time order, and 0xC310 reads the same five. A
	// coincidence does not follow a sequence back down.
	//
	// EnemiesDefeatedMirrorOff sits in the counter block at 0x4EC and agrees
	// with 0x70 in all five. What the samples cannot settle is mirror versus
	// "defeated in the current world", because all five are in Olympus and a
	// per-world counter would read identically. Location varies across them and
	// this does not, so it is at least not per-location. Patch handles the
	// ambiguity by only ever keeping the two equal when they already were,
	// which is right under either reading.
	EnemiesDefeatedMirrorOff = 0x504  // u32, twin of EnemiesDefeated at 0x70
	SaveIconMirrorOff        = 0xC310 // u32, twin of SaveIcon at 0x60

	StoryFlagOff   = 0xB4C4 // i32 per StoryFlags entry: a story-label number
	StoryFlagCount = 80

	// KeychainUpgradeOff is the one offset here with no confirmation: it comes
	// from kikeprime/KH-Save-Editor and reads 0 in every sample save, so it is
	// reported but never written by anything that guesses.
	KeychainUpgradeOff   = 0xBB78
	KeychainUpgradeCount = 24

	PlayerScriptOff    = 0xBCE0 // NUL-terminated, 0x100 bytes
	PlayerScriptLen    = 0x100
	PlayerCharacterOff = 0xBDE0
	PlayerCharacterLen = 0x100

	// Shortcuts are three pages of four face-button bindings.
	ShortcutOff     = 0xBF20
	ShortcutPages   = 3
	ShortcutButtons = 4

	// Magic and link slots hold Commands ids. MagicCount is 6 because Commands
	// has exactly six magic families, each occupying four consecutive ids:
	// Fire 29, Blizzard 33, Thunder 37, Water 41, Aero 45, Cure 49.
	//
	// All five sample saves read Fire in slot 0 and Water in slot 1 and nothing
	// else, which is correct and was once mistaken for an anomaly: Sora starts
	// with Fire and learns Water after the Darkside fight in the prologue,
	// before Olympus, and the next spell he gets is Cure in Twilight Town, the
	// world after. So a save anywhere in Olympus holds exactly these two.
	//
	// That rules out an array indexed by family -- Water would sit in slot 3 --
	// and leaves a compact list of the spells known, at most one per family.
	// Whether the order is acquisition order or a fixed menu order cannot be
	// told from two entries that agree on both, so slots are still reported
	// and written as stored.
	MagicOff   = 0xBF50
	MagicCount = 6
	LinkOff    = 0xBF68
	LinkCount  = 5
)

// Per-character equipment. The four arrays are contiguous from 0x80 to 0x158
// and share one slot shape, so one set of helpers covers all of them.
const (
	CurrentWeaponOff = 0x06 // index into the character's own weapon slots

	WeaponSlotOff = 0x80
	WeaponSlots   = 3
	ArmorSlotOff  = 0x98
	ArmorSlots    = 8
	ItemSlotOff   = 0x118
	ItemSlots     = 8

	// AI settings, one byte each, only meaningful for party members.
	AICombatStyleOff    = 0x158
	AIAbilityOff        = 0x159
	AIRecoveryOff       = 0x15A
	AIRecoveryTargetOff = 0x15B
)

// Item type discriminators. An equipment slot stores an id plus one of these,
// and the type is what says which table the id means something in.
const (
	ItemTypeConsumable = 0
	ItemTypeTent       = 2
	ItemTypeWeapon     = 3
	ItemTypeArmor      = 4
	ItemTypeSnack      = 6
	ItemTypeSynthesis  = 7
	ItemTypeFood       = 8
	ItemTypeKeyItem    = 9
	ItemTypeMog        = 10
)

// ShortcutButtons in order. The game draws them as face buttons; these are the
// PlayStation names because that is what the KH3 menus use.
var ShortcutButtonNames = [ShortcutButtons]string{"circle", "triangle", "square", "cross"}

// EquipKinds are the four equipment arrays, in save order.
var EquipKinds = []struct {
	Name  string
	Off   int
	Slots int
}{
	{"weapons", WeaponSlotOff, WeaponSlots},
	{"armor", ArmorSlotOff, ArmorSlots},
	{"accessories", AccessorySlotOff, AccessorySlots},
	{"items", ItemSlotOff, ItemSlots},
}

// EquipName renders an equipment slot's id against the table its type byte
// selects. An unknown pairing reads "?" rather than silently naming the id out
// of the wrong space, which is how an editor hands someone a keyblade that
// reads as a snack.
func EquipName(id, itemType int) string {
	var t map[int]string
	switch itemType {
	case ItemTypeConsumable, 1:
		t = Consumables
	case ItemTypeTent:
		t = Tents
	case ItemTypeWeapon:
		t = Weapons
	case ItemTypeArmor:
		t = Armors
	case ItemTypeAccessory:
		t = Accessories
	case ItemTypeSnack:
		t = Snacks
	case ItemTypeSynthesis:
		t = Synthesis
	case ItemTypeFood:
		t = Foods
	case ItemTypeKeyItem:
		t = KeyItems
	case ItemTypeMog:
		t = MogItems
	default:
		return "?"
	}
	if n, ok := t[id]; ok {
		return n
	}
	return "?"
}

// Equip is one slot of one of the four equipment arrays.
type Equip struct {
	ID       int
	ItemType int
	Enabled  bool
}

// Empty reports a slot holding nothing. An unused slot reads as id 0 of type
// 0, which is a real consumable id, so both halves have to be zero.
func (e Equip) Empty() bool { return e.ID == 0 && e.ItemType == 0 }

func (e Equip) Name() string { return EquipName(e.ID, e.ItemType) }

func equipOffset(char, arrayOff, slot int) int {
	return charOffset(char) + arrayOff + slot*EquipEntrySize
}

func GetEquip(p []byte, char, arrayOff, slot int) Equip {
	off := equipOffset(char, arrayOff, slot)
	return Equip{ID: int(p[off]), ItemType: int(p[off+1]), Enabled: p[off+4] != 0}
}

// SetEquip writes a slot, leaving the three bytes this format has never been
// seen to use alone rather than zeroing what we do not understand.
func SetEquip(p []byte, char, arrayOff, slot int, e Equip) {
	off := equipOffset(char, arrayOff, slot)
	p[off] = byte(e.ID)
	p[off+1] = byte(e.ItemType)
	p[off+4] = 0
	if e.Enabled {
		p[off+4] = 1
	}
}

// AI is a party member's three behavior settings plus the target mask the
// recovery setting applies to.
type AI struct {
	CombatStyle     int
	AbilityUse      int
	RecoveryUse     int
	RecoveryTargets int
}

func GetAI(p []byte, char int) AI {
	b := charOffset(char)
	return AI{
		CombatStyle:     int(p[b+AICombatStyleOff]),
		AbilityUse:      int(p[b+AIAbilityOff]),
		RecoveryUse:     int(p[b+AIRecoveryOff]),
		RecoveryTargets: int(p[b+AIRecoveryTargetOff]),
	}
}

func SetAI(p []byte, char int, a AI) {
	b := charOffset(char)
	p[b+AICombatStyleOff] = byte(a.CombatStyle)
	p[b+AIAbilityOff] = byte(a.AbilityUse)
	p[b+AIRecoveryOff] = byte(a.RecoveryUse)
	p[b+AIRecoveryTargetOff] = byte(a.RecoveryTargets)
}

func GetCurrentWeapon(p []byte, char int) int {
	return int(p[charOffset(char)+CurrentWeaponOff])
}

func SetCurrentWeapon(p []byte, char, slot int) {
	p[charOffset(char)+CurrentWeaponOff] = byte(slot)
}

// Fixed-width array accessors. Each one bounds its index against the count the
// format declares, so a bad id from a JSON document cannot reach into the
// neighboring structure.

func GetU16Array(p []byte, off, count, i int) int {
	if i < 0 || i >= count {
		return 0
	}
	return int(binary.LittleEndian.Uint16(p[off+i*2:]))
}

func SetU16Array(p []byte, off, count, i, v int) {
	if i < 0 || i >= count {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 0xFFFF {
		v = 0xFFFF
	}
	binary.LittleEndian.PutUint16(p[off+i*2:], uint16(v))
}

func GetI32Array(p []byte, off, count, i int) int32 {
	if i < 0 || i >= count {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(p[off+i*4:]))
}

func SetI32Array(p []byte, off, count, i int, v int32) {
	if i < 0 || i >= count {
		return
	}
	binary.LittleEndian.PutUint32(p[off+i*4:], uint32(v))
}

func GetMaterial(p []byte, id int) int    { return GetU16Array(p, MaterialOff, MaterialCount, id) }
func SetMaterial(p []byte, id, v int)     { SetU16Array(p, MaterialOff, MaterialCount, id, v) }
func GetStoryFlag(p []byte, id int) int32 { return GetI32Array(p, StoryFlagOff, StoryFlagCount, id) }
func SetStoryFlag(p []byte, id int, v int32) {
	SetI32Array(p, StoryFlagOff, StoryFlagCount, id, v)
}

func GetAttractionUse(p []byte, id int) int {
	return GetU16Array(p, AttractionUseOff, AttractionUseCount, id)
}

func GetShotlockUse(p []byte, id int) int {
	return GetU16Array(p, ShotlockUseOff, ShotlockUseCount, id)
}

// GetParty returns the five party slots. Sora is not in it; he is always
// present and the array holds who stands beside him.
func GetParty(p []byte) [PartySlots]int {
	var out [PartySlots]int
	for i := range out {
		out[i] = int(p[PartyOff+i])
	}
	return out
}

func SetPartySlot(p []byte, slot, id int) {
	if slot < 0 || slot >= PartySlots {
		return
	}
	p[PartyOff+slot] = byte(id)
}

// GetShortcut reads one face-button binding of one shortcut page.
func GetShortcut(p []byte, page, button int) int {
	if page < 0 || page >= ShortcutPages || button < 0 || button >= ShortcutButtons {
		return 0
	}
	return int(le32(p[ShortcutOff+(page*ShortcutButtons+button)*4:]))
}

func SetShortcut(p []byte, page, button, cmd int) {
	if page < 0 || page >= ShortcutPages || button < 0 || button >= ShortcutButtons {
		return
	}
	putLe32(p[ShortcutOff+(page*ShortcutButtons+button)*4:], uint32(cmd))
}

func GetMagic(p []byte, i int) int { return int(GetI32Array(p, MagicOff, MagicCount, i)) }
func SetMagic(p []byte, i, v int)  { SetI32Array(p, MagicOff, MagicCount, i, int32(v)) }
func GetLink(p []byte, i int) int  { return int(GetI32Array(p, LinkOff, LinkCount, i)) }
func SetLink(p []byte, i, v int)   { SetI32Array(p, LinkOff, LinkCount, i, int32(v)) }
func GetCrabs(p []byte) int32      { return int32(le32(p[CrabsOff:])) }
func SetCrabs(p []byte, v int32)   { putLe32(p[CrabsOff:], uint32(v)) }
func GetKeychainUpgrade(p []byte, i int) int {
	if i < 0 || i >= KeychainUpgradeCount {
		return 0
	}
	return int(p[KeychainUpgradeOff+i])
}

// CommandName renders a Commands id, used by the shortcut, magic and link
// arrays alike.
func CommandName(id int) string {
	if n, ok := Commands[id]; ok {
		return n
	}
	return "?"
}

// MaterialName renders a synthesis material.
func MaterialName(id int) string {
	if n, ok := Materials[id]; ok {
		return n
	}
	return "?"
}

// lookup is the shared "name it or say ?" for the display-only tables.
func lookup(t map[int]string, id int) string {
	if n, ok := t[id]; ok {
		return n
	}
	return "?"
}

func WorldName(id int) string    { return lookup(Worlds, id) }
func LocationName(id int) string { return lookup(Locations, id) }
func IconName(id int) string     { return lookup(CharacterIcons, id) }
func PartyName(id int) string    { return lookup(PartyCharacters, id) }
func StoryFlagName(id int) string {
	return lookup(StoryFlags, id)
}
