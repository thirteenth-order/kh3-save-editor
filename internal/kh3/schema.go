package kh3

import "sort"

// The schema describes every field this program can edit: what it is called in
// a dump, what kind of value it holds, which table names its ids, and what
// range the format allows. One description serves three callers that would
// otherwise each grow their own copy and drift apart:
//
//   - the browser UI, which builds an editor out of it rather than carrying a
//     hand-written widget per field;
//   - a validator that can answer before a write instead of after it;
//   - TestSchemaCoversEveryDumpedKey, which walks a dump and fails if anything
//     Dump can say is missing here, or anything here is missing from Dump.
//
// That last one is the point. A schema that is merely documentation goes stale
// the first time a field is added; one the tests hold against the dump cannot.
const (
	KindInt   = "int"     // a plain number
	KindEnum  = "enum"    // a number whose meaning comes from a table
	KindBool  = "bool"    // stored as 0 or 1
	KindText  = "text"    // a fixed-width NUL-terminated string
	KindFlags = "flags"   // a number with named bits
	KindEquip = "equip"   // an equipment slot, or null for empty
	KindWord  = "word"    // a 32-bit ability word with named bits
	KindDerv  = "derived" // rendered by Dump, ignored by Patch
)

// Bit is one named bit of a KindFlags or KindWord field.
type Bit struct {
	Bit   int    `json:"bit"`
	Key   string `json:"key"`
	Label string `json:"label"`
	Note  string `json:"note,omitempty"`
}

// Field describes one editable value.
//
// Min/Max are what the *format* allows, and a document outside them is an
// error. SoftMin/SoftMax are what the *game* allows, and a document outside
// them is a warning: level 120 fits in the byte perfectly well, it is just not
// a level the game will ever show you. Keeping the two apart is what lets the
// editor say "that will not do what you think" without refusing to write it.
type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Kind     string `json:"kind"`
	Table    string `json:"table,omitempty"`
	Min      *int64 `json:"min,omitempty"`
	Max      *int64 `json:"max,omitempty"`
	SoftMin  *int64 `json:"softMin,omitempty"`
	SoftMax  *int64 `json:"softMax,omitempty"`
	SoftNote string `json:"softNote,omitempty"`
	MaxLen   int    `json:"maxLen,omitempty"`
	Readonly bool   `json:"readonly,omitempty"`
	Note     string `json:"note,omitempty"`
	Offset   string `json:"offset,omitempty"`
	Bits     []Bit  `json:"bits,omitempty"`

	// SlotOnly marks a field the small system file does not carry. Dump stops
	// at the difficulty byte for that file, and so does the editor.
	SlotOnly bool `json:"slotOnly,omitempty"`
}

// Section is one region of a dump. Shape says how it is keyed:
//
//	object  a fixed set of named fields          header, records.scores
//	index   entries keyed by a decimal index     party, materials, magic
//	names   entries keyed by a name              characters, shortcuts pages
//	group   nothing of its own, only subsections records, equipment
type Section struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Shape string `json:"shape"`
	Note  string `json:"note,omitempty"`

	// Count and IndexTable apply to an index shape: how many entries there
	// are, and the table that names them.
	Count      int      `json:"count,omitempty"`
	IndexTable string   `json:"indexTable,omitempty"`
	Keys       []string `json:"keys,omitempty"` // for a names shape

	// EntryKeys splits each entry of an index shape one level further, into a
	// fixed set of named slots. Only the shortcut pages do this: a page is
	// indexed 0 to 2 and each page holds four bindings named after the face
	// buttons.
	EntryKeys []string `json:"entryKeys,omitempty"`

	// Sparse marks a section Dump omits entries from when they are empty, so
	// the editor offers to add one rather than showing a thousand blanks.
	Sparse bool `json:"sparse,omitempty"`

	// Requires names a capability the buffer must have, currently only
	// "records": the synthetic saves the tests build stop before that block.
	Requires string `json:"requires,omitempty"`

	// Spoils is the warning to show over a region whose contents say what is
	// ahead of the player rather than what is behind them -- who they will
	// travel with, which worlds there are. Non-empty means an interface should
	// cover it until asked, and the text is what it should say.
	//
	// It lives here rather than in the page because two views render these
	// regions and a third prints them, and a list of "the spoilery ones" kept
	// in one of them would go stale the moment a section moved. This is the
	// same reason the ranges and the provenance notes are here.
	Spoils string `json:"spoils,omitempty"`

	Fields   []Field   `json:"fields,omitempty"`   // object shape
	Entry    []Field   `json:"entry,omitempty"`    // index / names shape
	Sections []Section `json:"sections,omitempty"` // group shape
}

// Schema is the whole description, plus the tables its enums point at.
type Schema struct {
	DocFormat string              `json:"docFormat"`
	Sections  []Section           `json:"sections"`
	Tables    map[string][]TableE `json:"tables"`

	// EquipTables is the type-byte dispatch, so an editor can offer the right
	// list once a slot's type is chosen instead of one flat list of ids that
	// mean different things.
	EquipTables map[int]string `json:"equipTables"`
}

// TableE is one id/name pair of an enum table.
type TableE struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func i64(v int64) *int64 { return &v }

// u8, u16, u32 and i32 are the storage bounds the format imposes.
var (
	u8Min, u8Max   = i64(0), i64(0xFF)
	u16Min, u16Max = i64(0), i64(0xFFFF)
	u32Min, u32Max = i64(0), i64(0xFFFFFFFF)
	i32Min, i32Max = i64(-2147483648), i64(2147483647)
)

// itemFlagBits names the flags byte beside each inventory count. The names are
// KHSave.Lib3's InventoryEntry, whose bools pack into exactly these bits.
var itemFlagBits = []Bit{
	{0, "obtained", "Obtained", "the item is in the inventory at all"},
	{1, "new", "Unseen", "the ! marker the menu draws on something just picked up"},
	{2, "shop_1", "Shop flag 1", ""},
	{3, "shop_2", "Shop flag 2", ""},
	{4, "flag_4", "Flag 4", ""},
	{5, "flag_5", "Flag 5", ""},
	{6, "flag_6", "Flag 6", ""},
	{7, "flag_7", "Flag 7", ""},
}

// abilityWordBits names the low four bits of an ability word. Everything from
// bit 12 up is the equipment that granted it, which Dump reports as "source".
var abilityWordBits = []Bit{
	{0, "owned", "Owned", ""},
	{1, "equipped", "Equipped", ""},
	{2, "new", "Unseen", ""},
	{3, "innate", "Innate", "set only when no equipment source bit is present"},
}

func tableNames(t map[int]string) []TableE {
	out := make([]TableE, 0, len(t))
	for id, name := range t {
		out = append(out, TableE{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Tables is every enum a schema field can point at, by the name it uses.
func Tables() map[string][]TableE {
	diff := map[int]string{}
	for k, v := range Difficulties {
		diff[int(k)] = v
	}
	shot := map[int]string{}
	for i := 0; i < ShotlockUseCount; i++ {
		shot[i] = lookup(RecordShotlocks, i)
	}
	chars := map[int]string{}
	for i, n := range CharNames {
		chars[i] = n
	}
	flans := map[int]string{}
	for i, n := range FlanNames {
		flans[i] = n
	}
	scores := map[int]string{}
	for i, n := range RecordScores {
		scores[i] = n
	}
	return map[string][]TableE{
		"Difficulties":       tableNames(diff),
		"Worlds":             tableNames(Worlds),
		"Locations":          tableNames(Locations),
		"CharacterIcons":     tableNames(CharacterIcons),
		"PartyCharacters":    tableNames(PartyCharacters),
		"PlayableCharacters": tableNames(chars),
		"DesireChoices":      tableNames(DesireChoices),
		"PowerChoices":       tableNames(PowerChoices),
		"AiCombatStyles":     tableNames(AiCombatStyles),
		"AiAbilityUse":       tableNames(AiAbilityUse),
		"AiRecoveryUse":      tableNames(AiRecoveryUse),
		"Commands":           tableNames(Commands),
		"Items":              tableNames(Items),
		"Materials":          tableNames(Materials),
		"StoryFlags":         tableNames(StoryFlags),
		"Abilities":          tableNames(Abilities),
		"ItemTypes":          tableNames(ItemTypes),
		"RecordAttractions":  tableNames(RecordAttractions),
		"RecordShotlocks":    tableNames(shot),
		"RecordScores":       tableNames(scores),
		"Flans":              tableNames(flans),
		"Weapons":            tableNames(Weapons),
		"Armors":             tableNames(Armors),
		"Accessories":        tableNames(Accessories),
		"Consumables":        tableNames(Consumables),
		"Tents":              tableNames(Tents),
		"Snacks":             tableNames(Snacks),
		"Synthesis":          tableNames(Synthesis),
		"Foods":              tableNames(Foods),
		"KeyItems":           tableNames(KeyItems),
		"MogItems":           tableNames(MogItems),
	}
}

// EquipTables maps an item type byte to the table that names ids of that type,
// which is the same dispatch EquipName does. The editor needs it to know which
// list to offer once a slot's type is chosen.
func EquipTables() map[int]string {
	return map[int]string{
		ItemTypeConsumable: "Consumables",
		1:                  "Consumables",
		ItemTypeTent:       "Tents",
		ItemTypeWeapon:     "Weapons",
		ItemTypeArmor:      "Armors",
		ItemTypeAccessory:  "Accessories",
		ItemTypeSnack:      "Snacks",
		ItemTypeSynthesis:  "Synthesis",
		ItemTypeFood:       "Foods",
		ItemTypeKeyItem:    "KeyItems",
		ItemTypeMog:        "MogItems",
	}
}

// enumF, intF and friends keep the section table below readable: without them
// every line is three quarters struct literal.
func enumF(key, label, table, off string) Field {
	return Field{Key: key, Label: label, Kind: KindEnum, Table: table, Offset: off, SlotOnly: true}
}

func intF(key, label, off string, min, max *int64) Field {
	return Field{Key: key, Label: label, Kind: KindInt, Min: min, Max: max, Offset: off, SlotOnly: true}
}

func (f Field) soft(min, max int64, note string) Field {
	f.SoftMin, f.SoftMax, f.SoftNote = i64(min), i64(max), note
	return f
}

func (f Field) with(note string) Field { f.Note = note; return f }

func (f Field) always() Field { f.SlotOnly = false; return f }

func (f Field) ro() Field { f.Readonly = true; return f }

func textF(sf StringField) Field {
	return Field{Key: sf.Name, Label: sf.Name, Kind: KindText, MaxLen: sf.Len - 1,
		Offset: hexOff(sf.Off), Note: sf.Note, SlotOnly: true}
}

func hexOff(off int) string {
	const digits = "0123456789ABCDEF"
	if off == 0 {
		return "0x0"
	}
	var b []byte
	for off > 0 {
		b = append([]byte{digits[off&0xF]}, b...)
		off >>= 4
	}
	return "0x" + string(b)
}

// headerFieldsSchema mirrors headerFields, in the same order, so the two
// cannot describe different things. Anything readable but not writable is
// marked read-only rather than left out: a dump is patched back whole, and a
// field the editor cannot see is a field somebody edits blind.
func headerSchema() []Field {
	out := []Field{
		intF("filesize", "File size", "0x04", u32Min, u32Max).always().ro(),
		intF("version_major", "Version major", "0x08", u16Min, u16Max).always().ro(),
		intF("version_minor", "Version minor", "0x0A", u16Min, u16Max).always().ro(),
		intF("checksum_crc32", "CRC32", "0x0C", u32Min, u32Max).always().ro().
			with("rebuilt on every write; the value here is what the file currently holds"),
		enumF("difficulty", "Difficulty", "Difficulties", "0x14").always().
			with("changing this alone is not a faithful difficulty change; use swap for that"),
		enumF("world_logo", "World", "Worlds", "0x18"),
		intF("playtime_seconds", "Playtime", "0x20", u32Min, u32Max),
		intF("total_exp", "Total EXP", "0x24", u32Min, u32Max),
		intF("munny", "Munny", "0x28", u32Min, u32Max).
			soft(0, 9999999, "the game caps munny at 9,999,999").
			with("a balance: munny_earned minus munny_spent. Patch keeps the three consistent, so moving this moves munny_earned with it"),
		intF("munny_earned", "Munny earned", hexOff(MunnyEarnedOff), u32Min, u32Max).
			with("munny earned to date. No soft cap: the running total outlives any one balance"),
		intF("munny_spent", "Munny spent", hexOff(MunnySpentOff), u32Min, u32Max).
			with("munny spent to date. Moving this moves munny, not munny_earned: what was spent is history"),
		intF("level", "Level", "0x2C", u8Min, u8Max).
			soft(1, 99, "the game stops at level 99"),
		enumF("desire_choice", "Desire", "DesireChoices", "0x30"),
		enumF("power_choice", "Power", "PowerChoices", "0x31"),
		Field{Key: "save_clear", Label: "Cleared", Kind: KindBool, Offset: "0x39", SlotOnly: true},
		enumF("location", "Location", "Locations", "0x54"),
		enumF("save_icon", "Save icon", "CharacterIcons", "0x60"),
		enumF("dlc_save_icon", "DLC save icon", "CharacterIcons", "0x68"),
		intF("enemies_defeated", "Enemies defeated", "0x70", u32Min, u32Max),
		intF("saves_count", "Times saved", "0x5B8", u16Min, u16Max),
		intF("crabs", "Lucky emblems", hexOff(CrabsOff), i32Min, i32Max).
			soft(0, 90, "there are ninety lucky emblems in the game"),
		intF("bonus_hp", "Bonus HP", "0xB49C", i32Min, i32Max).
			with("unknown whether this is an HP total or a count of bonus levels; reads 0 in every sample save"),
		intF("bonus_mp", "Bonus MP", "0xB4A0", i32Min, i32Max).
			with("see bonus_hp"),
		intF("bonus_strength", "Bonus strength", "0xB4A4", i32Min, i32Max),
		intF("bonus_magic", "Bonus magic", "0xB4A8", i32Min, i32Max),
		intF("bonus_defense", "Bonus defense", "0xB4AC", i32Min, i32Max),
	}
	for _, sf := range StringFields {
		out = append(out, textF(sf))
	}
	// Rendered for the reader and ignored on the way back in, because they are
	// derived from a field that is already here. Naming them is what lets a
	// document be checked for typos: a header key that is neither editable nor
	// one of these is a mistake, not something to skip quietly.
	for _, d := range []struct{ key, label, from string }{
		{"playtime", "Playtime, written out", "playtime_seconds"},
		{"world_logo_name", "World name", "world_logo"},
		{"location_name", "Location name", "location"},
		{"save_icon_name", "Save icon name", "save_icon"},
	} {
		out = append(out, Field{Key: d.key, Label: d.label, Kind: KindDerv,
			SlotOnly: true, Note: "derived from " + d.from + "; ignored on patch"})
	}
	// Read-only for a reason the format forces: the value is an int64 tick
	// count too large for a double to hold exactly, and a document that makes
	// the round trip through a browser's JSON would come back rounded. Making
	// it editable means a string-typed field, not an int one.
	out = append(out, Field{Key: "saved_at", Label: "Written at", Kind: KindDerv,
		SlotOnly: true, Offset: hexOff(SavedAtOff),
		Note: "wall-clock time the game wrote the save, UTC; an int64 of 100ns " +
			"ticks since 0001-01-01 (UE4 FDateTime). Empty if the save carries " +
			"none. Reported, never written, and omitted from a dump unless the " +
			"account id is explicitly requested"})
	return out
}

// Sections is the whole document, in the order Dump writes it.
func Sections() []Section {
	cmd := []Field{{Key: "id", Label: "Command", Kind: KindEnum, Table: "Commands"},
		{Key: "name", Label: "Name", Kind: KindDerv}}
	return []Section{
		{Key: "header", Label: "Header", Shape: "object", Fields: headerSchema()},
		{
			Key: "party", Label: "Party", Shape: "index", Count: PartySlots,
			Note:   "Sora is not in here; the array holds who stands beside him",
			Spoils: "Names who is travelling with Sora in this save, and the picker lists every character who ever can.",
			Entry:  []Field{{Key: "id", Label: "Character", Kind: KindEnum, Table: "PartyCharacters"}, {Key: "name", Kind: KindDerv}},
		},
		{
			Key: "shortcuts", Label: "Shortcuts", Shape: "index", Count: ShortcutPages,
			Note:      "three pages of four face-button bindings",
			EntryKeys: ShortcutButtonNames[:], Entry: cmd,
		},
		{Key: "magic", Label: "Magic", Shape: "index", Count: MagicCount, Entry: cmd,
			Note: "the offset is confirmed and the slot ordering is not: both sample saves " +
				"read Water in slot 1 an hour and a half into Olympus, where Water is not " +
				"obtainable. Reported as stored."},
		{Key: "links", Label: "Links", Shape: "index", Count: LinkCount, Entry: cmd},
		{
			Key: "story_flags", Label: "Story progress", Shape: "index",
			Count: StoryFlagCount, IndexTable: "StoryFlags", Sparse: true,
			Note:   "each entry is a story-label number, not a boolean: it counts how far that world got",
			Spoils: "Names every world in the game, and how far this save has got through each one.",
			Entry: []Field{{Key: "value", Label: "Progress", Kind: KindInt, Min: i32Min, Max: i32Max},
				{Key: "name", Kind: KindDerv}},
		},
		{
			Key: "materials", Label: "Synthesis materials", Shape: "index",
			Count: MaterialCount, IndexTable: "Materials", Sparse: true,
			Entry: []Field{{Key: "count", Label: "Held", Kind: KindInt, Min: u16Min, Max: u16Max},
				{Key: "name", Kind: KindDerv}},
		},
		{
			Key: "inventory", Label: "Inventory", Shape: "index",
			Count: InventoryCount, IndexTable: "Items", Sparse: true,
			Note: "a count of zero drops the entry and clears its flags, which is how an " +
				"item is removed",
			Entry: []Field{
				{Key: "count", Label: "Held", Kind: KindInt, Min: u8Min, Max: u8Max},
				{Key: "flags", Label: "Flags", Kind: KindFlags, Min: u8Min, Max: u8Max, Bits: itemFlagBits},
				{Key: "name", Kind: KindDerv},
			},
		},
		{
			Key: "keychain_upgrades", Label: "Keychain upgrades", Shape: "index",
			Count: KeychainUpgradeCount, Sparse: false,
			Note: "the one offset in this program with no confirmation: it comes from " +
				"kikeprime/KH-Save-Editor and reads 0 in every sample save",
			Entry: []Field{{Key: "value", Label: "Level", Kind: KindInt, Min: u8Min, Max: u8Max}},
		},
		recordsSection(),
		charactersSection(),
	}
}

func recordsSection() Section {
	return Section{
		Key: "records", Label: "Records", Shape: "group",
		Note: "the use counters are confirmed against gameplay; the bests sit in a block " +
			"whose offset is derived from this build's file-size delta, see records.go",
		Sections: []Section{
			{
				Key: "attractions", Label: "Attractions", Shape: "index",
				Count: AttractionUseCount, IndexTable: "RecordAttractions",
				Entry: []Field{
					{Key: "uses", Label: "Times used", Kind: KindInt, Min: u16Min, Max: u16Max, Offset: hexOff(AttractionUseOff)},
					{Key: "high_score", Label: "Best", Kind: KindInt, Min: i32Min, Max: i32Max, Offset: hexOff(RecordsOff + recAttractHi)},
					{Key: "name", Kind: KindDerv},
				},
			},
			{
				Key: "shotlocks", Label: "Shotlocks", Shape: "index",
				Count: ShotlockUseCount, IndexTable: "RecordShotlocks", Sparse: true,
				Entry: []Field{
					{Key: "uses", Label: "Times used", Kind: KindInt, Min: u16Min, Max: u16Max, Offset: hexOff(ShotlockUseOff)},
					{Key: "high_score", Label: "Best", Kind: KindInt, Min: i64(-32768), Max: i64(32767), Offset: hexOff(RecordsOff + recShotlockHi)},
					{Key: "name", Kind: KindDerv},
				},
			},
			minigameSection(),
			{
				Key: "flans", Label: "Flantastic Seven", Shape: "names",
				Keys: FlanNames, Requires: "records",
				Entry: []Field{
					{Key: "high_score", Label: "Best", Kind: KindInt, Min: i32Min, Max: i32Max},
					{Key: "high_score_2", Label: "Second score", Kind: KindInt, Min: i32Min, Max: i32Max,
						Note: "upstream names two score fields and both read zero everywhere, so which is which is reported as stored"},
					{Key: "attempts", Label: "Attempts", Kind: KindInt, Min: i32Min, Max: i32Max},
				},
			},
			{
				Key: "album", Label: "Photo album", Shape: "object", Requires: "records",
				Note: "the album itself is not mapped: it is 98.6% of the file and holds image blobs",
				Fields: []Field{{Key: "photo_max_count", Label: "Album limit", Kind: KindInt,
					Min: i32Min, Max: i32Max, Offset: hexOff(PhotoMaxCountOff), SlotOnly: true}},
			},
		},
	}
}

func charactersSection() Section {
	stats := []Field{
		{Key: "hp", Label: "Current HP", Kind: KindInt, Min: i32Min, Max: i32Max, Offset: hexOff(StatHP),
			Note: "max HP is not stored; this is the current value the save resumes with"},
		{Key: "mp", Label: "Current MP", Kind: KindInt, Min: i32Min, Max: i32Max, Offset: hexOff(StatMP)},
		{Key: "focus", Label: "Focus", Kind: KindInt, Min: i32Min, Max: i32Max, Offset: hexOff(StatFocus)},
		{Key: "atk_boost", Label: "Power boosts", Kind: KindInt, Min: u8Min, Max: u8Max, Offset: hexOff(StatAtkBoost)},
		{Key: "mag_boost", Label: "Magic boosts", Kind: KindInt, Min: u8Min, Max: u8Max, Offset: hexOff(StatMagBoost)},
		{Key: "def_boost", Label: "Defense boosts", Kind: KindInt, Min: u8Min, Max: u8Max, Offset: hexOff(StatDefBoost)},
		{Key: "ap_boost", Label: "AP boosts", Kind: KindInt, Min: u8Min, Max: u8Max, Offset: hexOff(StatApBoost)},
		{Key: "current_weapon", Label: "Drawn weapon slot", Kind: KindInt, Min: i64(0), Max: i64(WeaponSlots - 1),
			Offset: hexOff(CurrentWeaponOff), Note: "an index into this character's own weapon slots"},
	}

	equip := Section{Key: "equipment", Label: "Equipment", Shape: "group"}
	for _, k := range EquipKinds {
		equip.Sections = append(equip.Sections, Section{
			Key: k.Name, Label: k.Name, Shape: "index", Count: k.Slots, Sparse: true,
			Note: "null clears a slot; an entry without a type is an error, because an id " +
				"means nothing until the type byte says which table it is an id in",
			Entry: []Field{
				{Key: "type", Label: "Item type", Kind: KindEnum, Table: "ItemTypes"},
				{Key: "id", Label: "Item", Kind: KindEquip, Min: u8Min, Max: u8Max},
				{Key: "enabled", Label: "Enabled", Kind: KindBool},
				{Key: "type_name", Kind: KindDerv},
				{Key: "name", Kind: KindDerv},
			},
		})
	}

	return Section{
		Key: "characters", Label: "Characters", Shape: "names", Keys: CharNames,
		Note: "sixteen playable-character structs at " + hexOff(CharBase) +
			", " + hexOff(CharSize) + " bytes apart",
		// The struct array is fixed at sixteen whatever the save holds, so
		// this names every playable character in the game and not merely the
		// ones met so far.
		Spoils: "Names all sixteen playable characters, met or not.",
		Entry:  stats,
		Sections: []Section{
			aiSection(),
			equip,
			{
				Key: "abilities", Label: "Abilities", Shape: "index",
				Count: AbilityCount, IndexTable: "Abilities", Sparse: true,
				Note: "an entry reading 0x00000444 is absent and is not dumped; " +
					"0x0000044B is what a difficulty-granted default reads as",
				Entry: []Field{
					{Key: "word", Label: "Word", Kind: KindWord, Bits: abilityWordBits},
					{Key: "owned", Kind: KindDerv}, {Key: "equipped", Kind: KindDerv},
					{Key: "new", Kind: KindDerv}, {Key: "innate", Kind: KindDerv},
					{Key: "source", Kind: KindDerv},
					{Key: "name", Kind: KindDerv},
				},
			},
		},
	}
}

// Describe returns the whole schema, ready to serialize.
func Describe() Schema {
	return Schema{DocFormat: DocFormat, Sections: Sections(), Tables: Tables(),
		EquipTables: EquipTables()}
}

// minigameSection is the eleven scalar records, one field each. They are named
// rather than indexed because a bare number would tell nobody which timer they
// were looking at.
func minigameSection() Section {
	out := Section{Key: "minigames", Label: "Minigames", Shape: "object", Requires: "records",
		Note: "every one of these reads zero in all five sample saves, which is what an " +
			"hour and a half into Olympus should look like: none of this content is " +
			"reachable that early"}
	for i, n := range RecordScores {
		out.Fields = append(out.Fields, Field{Key: n, Label: n, Kind: KindInt,
			Min: i32Min, Max: i32Max, SlotOnly: true,
			Offset: hexOff(RecordsOff + recScoresOff + i*4)})
	}
	return out
}

// aiSection describes the three behavior bytes and the target mask. Each of
// the three is dumped as an id beside the name that id reads as, so each is a
// small object of its own rather than a bare number.
func aiSection() Section {
	pick := func(key, label, table, off string) Section {
		return Section{Key: key, Label: label, Shape: "object", Fields: []Field{
			{Key: "id", Label: label, Kind: KindEnum, Table: table, Offset: off, SlotOnly: true},
			{Key: "name", Kind: KindDerv, SlotOnly: true},
		}}
	}
	return Section{
		Key: "ai", Label: "AI", Shape: "object",
		Note: "only meaningful for a party member",
		Fields: []Field{{Key: "recovery_targets", Label: "Recovery targets", Kind: KindInt,
			Min: u8Min, Max: u8Max, SlotOnly: true, Offset: hexOff(AIRecoveryTargetOff),
			Note: "a mask whose bit meanings are not known"}},
		Sections: []Section{
			pick("combat_style", "Combat style", "AiCombatStyles", hexOff(AICombatStyleOff)),
			pick("ability_use", "Ability use", "AiAbilityUse", hexOff(AIAbilityOff)),
			pick("recovery_use", "Item use", "AiRecoveryUse", hexOff(AIRecoveryOff)),
		},
	}
}
