package kh3

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ItemName and AbilityName render "?" for ids the generated tables do not
// cover; real saves contain a few (736-738 in the sample set).
func ItemName(id int) string {
	if n, ok := Items[id]; ok {
		return n
	}
	return "?"
}

func AbilityName(id int) string {
	if n, ok := Abilities[id]; ok {
		return n
	}
	return "?"
}

// DocFormat tags documents this tool produced, so patch can reject a file
// meant for something else.
const DocFormat = "kh3-save-editor/1"

// derivedHeader keys are rendered by Dump out of a number that is already in
// the document, so patching them back would either be a no-op or a fight with
// the field they came from. They are accepted and ignored, which is what makes
// "dump, change one number, patch the whole thing back" work.
var derivedHeader = map[string]bool{
	"playtime": true, "world_logo_name": true,
	"location_name": true, "save_icon_name": true,
	"saved_at": true,
}

// ReadonlyHeader fields would break the container if rewritten, and the
// checksum is recomputed on save anyway.
var ReadonlyHeader = map[string]bool{
	"filesize": true, "version_major": true,
	"version_minor": true, "checksum_crc32": true,
}

type headerField struct {
	off  int
	kind string // u8 u16 u32 i32
	name string
}

// headerFields is ordered so the JSON reads the same way the Python does.
var headerFields = []headerField{
	{0x04, "u32", "filesize"},
	{0x08, "u16", "version_major"},
	{0x0A, "u16", "version_minor"},
	{0x0C, "u32", "checksum_crc32"},
	{0x14, "u8", "difficulty"},
	{0x18, "u8", "world_logo"},
	{0x20, "u32", "playtime_seconds"},
	{0x24, "u32", "total_exp"},
	{0x28, "u32", "munny"},
	{MunnyEarnedOff, "u32", "munny_earned"},
	{MunnySpentOff, "u32", "munny_spent"},
	{0x2C, "u8", "level"},
	{0x30, "u8", "desire_choice"},
	{0x31, "u8", "power_choice"},
	{0x39, "u8", "save_clear"},
	{0x54, "u8", "location"},
	{0x60, "u8", "save_icon"},
	{0x68, "u8", "dlc_save_icon"},
	{0x70, "u32", "enemies_defeated"},
	{0x5B8, "u16", "saves_count"},
	{CrabsOff, "i32", "crabs"},
	{0xB49C, "i32", "bonus_hp"},
	{0xB4A0, "i32", "bonus_mp"},
	{0xB4A4, "i32", "bonus_strength"},
	{0xB4A8, "i32", "bonus_magic"},
	{0xB4AC, "i32", "bonus_defense"},
}

var charStats = []struct {
	name string
	off  int
	kind string
}{
	{"hp", StatHP, "i32"}, {"mp", StatMP, "i32"}, {"focus", StatFocus, "i32"},
	{"atk_boost", StatAtkBoost, "u8"}, {"mag_boost", StatMagBoost, "u8"},
	{"def_boost", StatDefBoost, "u8"}, {"ap_boost", StatApBoost, "u8"},
}

func readField(p []byte, off int, kind string) int64 {
	switch kind {
	case "u8":
		return int64(p[off])
	case "u16":
		return int64(binary.LittleEndian.Uint16(p[off:]))
	case "u32":
		return int64(binary.LittleEndian.Uint32(p[off:]))
	default:
		return int64(int32(binary.LittleEndian.Uint32(p[off:])))
	}
}

func writeField(p []byte, off int, kind string, v int64) {
	switch kind {
	case "u8":
		p[off] = byte(v)
	case "u16":
		binary.LittleEndian.PutUint16(p[off:], uint16(v))
	default:
		binary.LittleEndian.PutUint32(p[off:], uint32(v))
	}
}

// ordered marshals key/value pairs in insertion order, which json's map
// handling will not do.
type ordered struct {
	keys []string
	vals map[string]any
}

func newOrdered() *ordered { return &ordered{vals: map[string]any{}} }

func (o *ordered) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *ordered) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// Dump renders the mapped state of a save as JSON.
func Dump(plain []byte, account string, characters int) ([]byte, error) {
	doc := newOrdered()
	doc.set("_format", DocFormat)
	var ro []string
	for k := range ReadonlyHeader {
		ro = append(ro, k)
	}
	sort.Strings(ro)
	doc.set("_note", "Absent keys are left unchanged on patch. The account id and "+
		"saved_at are omitted unless explicitly requested, because a dump is often "+
		"shared. Read-only: "+strings.Join(ro, ", "))
	if account == "" {
		doc.set("account", nil)
	} else {
		doc.set("account", account)
	}

	h := newOrdered()
	slot := IsSlot(plain)
	for _, f := range headerFields {
		if !slot && f.off > 0x14 {
			continue
		}
		h.set(f.name, readField(plain, f.off, f.kind))
	}
	if slot {
		// The four NUL-terminated fields, then the values derived from
		// numbers already above. Patch reads the first group and ignores the
		// second; the schema says which is which.
		for _, sf := range StringFields {
			h.set(sf.Name, GetString(plain, sf))
		}
		h.set("playtime", ReadHeader(plain).Playtime())
		// saved_at is the one field that says when somebody was playing, to the
		// millisecond, and a dump is the thing people paste into an issue. It is
		// owner context rather than playthrough state, the same class as the
		// account id, so it travels on the same explicit opt-in and is absent by
		// default. Patch ignores it either way.
		if account != "" {
			h.set("saved_at", ReadHeader(plain).SavedAtString())
		}
		h.set("world_logo_name", WorldName(int(plain[0x18])))
		h.set("location_name", LocationName(int(plain[0x54])))
		h.set("save_icon_name", IconName(int(plain[0x60])))
	}
	doc.set("header", h)

	if slot {
		doc.set("party", dumpParty(plain))
		doc.set("shortcuts", dumpShortcuts(plain))
		doc.set("magic", dumpCommandArray(plain, GetMagic, MagicCount))
		doc.set("links", dumpCommandArray(plain, GetLink, LinkCount))
		doc.set("story_flags", dumpStoryFlags(plain))
		doc.set("keychain_upgrades", dumpKeychain(plain))
		doc.set("records", dumpRecords(plain))
	}

	chars := newOrdered()
	if slot {
		if characters > len(CharNames) {
			characters = len(CharNames)
		}
		for ci := 0; ci < characters; ci++ {
			e := newOrdered()
			for _, st := range charStats {
				e.set(st.name, readField(plain, charOffset(ci)+st.off, st.kind))
			}
			e.set("current_weapon", GetCurrentWeapon(plain, ci))
			e.set("ai", dumpAI(plain, ci))
			e.set("equipment", dumpEquipment(plain, ci))
			abil := newOrdered()
			for aid := 0; aid < AbilityCount; aid++ {
				w := GetAbility(plain, ci, aid)
				if w == AbilityAbsent {
					continue
				}
				a := newOrdered()
				a.set("word", fmt.Sprintf("0x%08X", w))
				a.set("name", AbilityName(aid))
				a.set("owned", w&1 != 0)
				a.set("equipped", w&2 != 0)
				a.set("new", w&4 != 0)
				a.set("innate", w&8 != 0)
				a.set("source", int(w>>12))
				abil.set(fmt.Sprintf("0x%03X", aid), a)
			}
			e.set("abilities", abil)
			chars.set(CharNames[ci], e)
		}
	}
	doc.set("characters", chars)

	inv := newOrdered()
	if slot {
		for id := 0; id < InventoryCount; id++ {
			count, flags := GetItem(plain, id)
			if count == 0 {
				continue
			}
			e := newOrdered()
			e.set("count", int(count))
			e.set("flags", fmt.Sprintf("0x%02X", flags))
			e.set("name", ItemName(id))
			inv.set(strconv.Itoa(id), e)
		}
	}
	doc.set("inventory", inv)

	mats := newOrdered()
	if slot {
		for id := 0; id < MaterialCount; id++ {
			n := GetMaterial(plain, id)
			if n == 0 {
				continue
			}
			e := newOrdered()
			e.set("count", n)
			e.set("name", MaterialName(id))
			mats.set(strconv.Itoa(id), e)
		}
	}
	doc.set("materials", mats)

	return json.MarshalIndent(doc, "", "  ")
}

// The dump* helpers below render one region each. They all key an array by its
// decimal index rather than emitting a JSON list, so a patch can name a single
// slot without restating the ones it is not touching.

func dumpParty(p []byte) *ordered {
	out := newOrdered()
	for slot, id := range GetParty(p) {
		e := newOrdered()
		e.set("id", id)
		e.set("name", PartyName(id))
		out.set(strconv.Itoa(slot), e)
	}
	return out
}

func dumpShortcuts(p []byte) *ordered {
	out := newOrdered()
	for page := 0; page < ShortcutPages; page++ {
		g := newOrdered()
		for b, name := range ShortcutButtonNames {
			cmd := GetShortcut(p, page, b)
			e := newOrdered()
			e.set("id", cmd)
			e.set("name", CommandName(cmd))
			g.set(name, e)
		}
		out.set(strconv.Itoa(page), g)
	}
	return out
}

func dumpCommandArray(p []byte, get func([]byte, int) int, count int) *ordered {
	out := newOrdered()
	for i := 0; i < count; i++ {
		cmd := get(p, i)
		e := newOrdered()
		e.set("id", cmd)
		e.set("name", CommandName(cmd))
		out.set(strconv.Itoa(i), e)
	}
	return out
}

// dumpStoryFlags omits the flags still at zero. Eighty entries of which a
// handful are set makes a wall of noise out of the interesting half-dozen.
func dumpStoryFlags(p []byte) *ordered {
	out := newOrdered()
	for id := 0; id < StoryFlagCount; id++ {
		v := GetStoryFlag(p, id)
		if v == 0 {
			continue
		}
		e := newOrdered()
		e.set("value", v)
		e.set("name", StoryFlagName(id))
		out.set(strconv.Itoa(id), e)
	}
	return out
}

// dumpKeychain reports all 24 entries, zeros included. The offset is the one
// in this program with no confirmation and it reads 0 everywhere, so hiding
// the zeros would leave nothing at all and nobody able to tell whether the
// field was checked or missing.
func dumpKeychain(p []byte) *ordered {
	out := newOrdered()
	for i := 0; i < KeychainUpgradeCount; i++ {
		e := newOrdered()
		e.set("value", GetKeychainUpgrade(p, i))
		out.set(strconv.Itoa(i), e)
	}
	return out
}

// dumpRecords renders the use counters, which every save carries, and the
// bests, which live in a block a short synthetic save does not reach. The two
// are kept in one section because they describe the same five attractions and
// the same thirty shotlocks, and splitting them would mean naming both twice.
func dumpRecords(p []byte) *ordered {
	full := HasRecords(p)

	att := newOrdered()
	for id := 0; id < AttractionUseCount; id++ {
		e := newOrdered()
		e.set("uses", GetAttractionUse(p, id))
		if full {
			e.set("high_score", GetAttractionHigh(p, id))
		}
		e.set("name", lookup(RecordAttractions, id))
		att.set(strconv.Itoa(id), e)
	}

	shot := newOrdered()
	for id := 0; id < ShotlockUseCount; id++ {
		n := GetShotlockUse(p, id)
		hi := 0
		if full {
			hi = GetShotlockHigh(p, id)
		}
		if n == 0 && hi == 0 {
			continue
		}
		e := newOrdered()
		e.set("uses", n)
		if full {
			e.set("high_score", hi)
		}
		e.set("name", lookup(RecordShotlocks, id))
		shot.set(strconv.Itoa(id), e)
	}

	out := newOrdered()
	out.set("attractions", att)
	out.set("shotlocks", shot)
	if !full {
		return out
	}

	mini := newOrdered()
	for i, n := range RecordScores {
		mini.set(n, GetRecordScore(p, i))
	}
	out.set("minigames", mini)

	flans := newOrdered()
	for i, n := range FlanNames {
		f := GetFlan(p, i)
		e := newOrdered()
		e.set("high_score", f.HighScore)
		e.set("high_score_2", f.HighScore2)
		e.set("attempts", f.Attempts)
		flans.set(n, e)
	}
	out.set("flans", flans)

	album := newOrdered()
	album.set("photo_max_count", GetPhotoMaxCount(p))
	out.set("album", album)
	return out
}

func dumpAI(p []byte, ci int) *ordered {
	a := GetAI(p, ci)
	out := newOrdered()
	for _, f := range aiFields {
		e := newOrdered()
		v := f.get(a)
		e.set("id", v)
		e.set("name", lookup(f.table, v))
		out.set(f.name, e)
	}
	out.set("recovery_targets", a.RecoveryTargets)
	return out
}

func dumpEquipment(p []byte, ci int) *ordered {
	out := newOrdered()
	for _, k := range EquipKinds {
		g := newOrdered()
		for s := 0; s < k.Slots; s++ {
			eq := GetEquip(p, ci, k.Off, s)
			if eq.Empty() {
				continue
			}
			e := newOrdered()
			e.set("id", eq.ID)
			e.set("type", eq.ItemType)
			e.set("type_name", lookup(ItemTypes, eq.ItemType))
			e.set("name", eq.Name())
			e.set("enabled", eq.Enabled)
			g.set(strconv.Itoa(s), e)
		}
		out.set(k.Name, g)
	}
	return out
}

// aiFields ties each AI byte to the table that names it, so dump and patch
// cannot disagree about which of the three a key means.
var aiFields = []struct {
	name  string
	table map[int]string
	get   func(AI) int
	set   func(*AI, int)
}{
	{"combat_style", AiCombatStyles, func(a AI) int { return a.CombatStyle },
		func(a *AI, v int) { a.CombatStyle = v }},
	{"ability_use", AiAbilityUse, func(a AI) int { return a.AbilityUse },
		func(a *AI, v int) { a.AbilityUse = v }},
	{"recovery_use", AiRecoveryUse, func(a AI) int { return a.RecoveryUse },
		func(a *AI, v int) { a.RecoveryUse = v }},
}

func asInt(v any) (int64, error) {
	switch t := v.(type) {
	case float64:
		if t != math.Trunc(t) {
			return 0, fmt.Errorf("%v is not an integer", t)
		}
		return int64(t), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(t), 0, 64)
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("cannot read %v as an integer", v)
}

// storeRange is what each storage kind holds. Every Min/Max schema.go declares
// is one of these pairs, which is what lets one check here mirror what the
// browser refuses.
var storeRange = map[string][2]int64{
	"u8":  {0, 0xFF},
	"u16": {0, 0xFFFF},
	"i16": {-32768, 32767},
	"u32": {0, 0xFFFFFFFF},
	"i32": {-2147483648, 2147483647},
}

// fits refuses a value the field cannot hold, rather than storing whatever is
// left of it after the conversion.
//
// The setters truncate or clamp, which is right for them -- they take a Go int
// and must put something in the byte -- but wrong as the answer to a document.
// Without this check Patch accepted 92 out-of-range values the browser
// validator rejects outright, and wrote something else: header.level 300 stored
// 44, inventory count 300 stored 255 while the change line said 300, and
// inventory count -5 stored 251. The two halves are supposed to agree in both
// directions, so this is the Go half of that and
// TestPatchRefusesWhatTheSchemaCallsOutOfRange is what holds them together.
//
// It checks only where the schema declares a range. A field the schema leaves
// unbounded -- the enum ids in party, magic, links and shortcuts -- is left
// alone on purpose: refusing one here and not in the browser would break the
// rule from the other side.
func fits(kind, where string, v int64) error {
	r, ok := storeRange[kind]
	if !ok {
		return fmt.Errorf("%s: no storage range for kind %q", where, kind)
	}
	if v < r[0] || v > r[1] {
		return fmt.Errorf("%s: %d is outside %d to %d, which is what the field holds",
			where, v, r[0], r[1])
	}
	return nil
}

// The munny ledger is three fields holding two numbers' worth of information:
// munny_earned - munny_spent == munny. A document that moves one of them and
// leaves the others at their dumped values has asked for something arithmetic,
// not something contradictory, so Patch works out which and writes it rather
// than either refusing or letting the three drift apart.
//
// Which field gives way follows from what each one is. munny_spent is history
// and nothing should rewrite it on somebody's behalf; munny is a balance, so it
// absorbs any change to the ledger; munny_earned absorbs a change to the
// balance, which is what "set my munny to 9999999" means. That leaves exactly
// one rule: recompute the field the document left alone, and when it left two
// alone, recompute the one on the other side of the identity.
//
// A document that moves all three is taken at its word if it balances and
// rejected if it does not, because there is no third number to solve for and
// guessing which of the three was the typo is not this function's business.
//
// None of that happens unless the ledger balanced to begin with. A save whose
// three numbers already disagree is not a ledger this code understands, and
// picking one of them to believe would be a guess; the synthetic fixtures are
// exactly that case, since they set munny and leave the pair at zero. So the
// rule is narrow on purpose: Patch keeps an identity that held from breaking,
// and never invents a value to repair one that was already broken. A document
// that names all three and contradicts itself is still rejected either way,
// because that is a mistake in the document rather than in the save.
const (
	munnyBalance = iota
	munnyEarned
	munnySpent
)

var munnyLedgerKeys = [3]string{"munny", "munny_earned", "munny_spent"}

func readMunnyLedger(p []byte) [3]int64 {
	return [3]int64{
		readField(p, 0x28, "u32"),
		readField(p, MunnyEarnedOff, "u32"),
		readField(p, MunnySpentOff, "u32"),
	}
}

func writeMunnyField(p []byte, i int, v int64) {
	switch i {
	case munnyBalance:
		writeField(p, 0x28, "u32", v)
	case munnyEarned:
		writeField(p, MunnyEarnedOff, "u32", v)
	default:
		writeField(p, MunnySpentOff, "u32", v)
	}
}

// reconcileMunny restores earned - spent == balance after the header loop has
// written whatever the document said, and appends the correction it made to
// changes so nobody is surprised by a field they did not name.
func reconcileMunny(p []byte, before [3]int64, changes *[]string) error {
	now := readMunnyLedger(p)
	var moved []int
	for i := range now {
		if now[i] != before[i] {
			moved = append(moved, i)
		}
	}
	if len(moved) == 0 {
		return nil
	}
	if len(moved) == 3 {
		if now[munnyEarned]-now[munnySpent] == now[munnyBalance] {
			return nil
		}
		return fmt.Errorf("header.munny %d, header.munny_earned %d and header.munny_spent %d "+
			"cannot all be right: earned minus spent has to be munny. Move at most two of "+
			"the three and the third is worked out",
			now[munnyBalance], now[munnyEarned], now[munnySpent])
	}
	if before[munnyEarned]-before[munnySpent] != before[munnyBalance] {
		return nil // never balanced; see above
	}

	// Solve for the field the document left alone. With two left alone, a
	// moved balance is absorbed by earned and a moved ledger side by balance.
	solve := munnyBalance
	if len(moved) == 1 && moved[0] == munnyBalance {
		solve = munnyEarned
	} else if len(moved) == 2 {
		for i := range now {
			if i != moved[0] && i != moved[1] {
				solve = i
			}
		}
	}

	want := now[munnyEarned] - now[munnySpent] // solve == munnyBalance
	switch solve {
	case munnyEarned:
		want = now[munnyBalance] + now[munnySpent]
	case munnySpent:
		want = now[munnyEarned] - now[munnyBalance]
	}
	if want < 0 || want > 0xFFFFFFFF {
		return fmt.Errorf("keeping the munny ledger consistent would put header.%s at %d, "+
			"which does not fit the field; set it yourself if that is really what you meant",
			munnyLedgerKeys[solve], want)
	}
	if want == now[solve] {
		return nil
	}
	writeMunnyField(p, solve, want)
	*changes = append(*changes, fmt.Sprintf("header.%s: %d -> %d (kept consistent with %s)",
		munnyLedgerKeys[solve], now[solve], want, munnyLedgerKeys[moved[0]]))
	return nil
}

// headerMirrors are the fields the format keeps a second copy of. Dump exposes
// only the first of each pair, so a document can move it and leave the copy
// behind -- which is the same silent-revert exposure the munny ledger has, and
// gets the same narrow treatment: the copy is kept equal only when it already
// was.
//
// That rule is what makes it safe without knowing which of the two readings of
// 0x504 is right (see layout.go). If it is a mirror, the two always agree and
// are always kept agreeing. If it is really a per-world count, then a save past
// the first world has them differing already and this leaves it alone; and a
// save still in the first world has every kill in that world, so setting both
// to the same number is right there too.
var headerMirrors = []struct {
	Name       string
	Off        int
	Kind       string
	MirrorOff  int
	MirrorKind string
}{
	{"enemies_defeated", 0x70, "u32", EnemiesDefeatedMirrorOff, "u32"},
	{"save_icon", 0x60, "u8", SaveIconMirrorOff, "u32"},
}

func readMirrors(p []byte) [][2]int64 {
	out := make([][2]int64, len(headerMirrors))
	for i, m := range headerMirrors {
		out[i] = [2]int64{readField(p, m.Off, m.Kind), readField(p, m.MirrorOff, m.MirrorKind)}
	}
	return out
}

// reconcileMirrors carries a moved field into its second copy, and reports it.
func reconcileMirrors(p []byte, before [][2]int64, changes *[]string) {
	for i, m := range headerMirrors {
		if before[i][0] != before[i][1] {
			continue // never agreed; not ours to repair
		}
		now := readField(p, m.Off, m.Kind)
		was := readField(p, m.MirrorOff, m.MirrorKind)
		if now == was {
			continue
		}
		writeField(p, m.MirrorOff, m.MirrorKind, now)
		*changes = append(*changes, fmt.Sprintf(
			"header.%s second copy at %s: %d -> %d (kept consistent)",
			m.Name, hexOff(m.MirrorOff), was, now))
	}
}

// slotOnlySections are the parts of a document that only exist for a
// per-playthrough slot. Dump omits the first seven for the small system file
// and emits the last three empty, so an empty one has to stay acceptable or a
// system file could not be dumped and patched back.
var slotOnlySections = []string{
	"party", "shortcuts", "magic", "links", "story_flags", "keychain_upgrades",
	"records", "characters", "inventory", "materials",
}

// refuseSlotOnlyKeys draws the line Dump already draws: for the system file it
// stops at the difficulty byte. Patch did not, and a document naming a
// slot-only field did not fail, it *panicked* -- bonus_hp lives at 0xB49C and
// the system file is about 0x7B20 bytes, so writing it indexed past the buffer.
// An error naming the field is the whole fix; the document simply does not
// belong to this file.
func refuseSlotOnlyKeys(d map[string]any, byName map[string]headerField) error {
	if hdr, ok := d["header"].(map[string]any); ok {
		for _, key := range sortedKeys(hdr) {
			f, known := byName[key]
			_, isText := StringFieldByName(key)
			if (known && f.off > 0x14) || isText {
				return fmt.Errorf("header.%s is not in a system file", key)
			}
		}
	}
	for _, sec := range slotOnlySections {
		v, ok := d[sec]
		if !ok {
			continue
		}
		if m, isMap := v.(map[string]any); isMap && len(m) == 0 {
			continue
		}
		return fmt.Errorf("%s is not in a system file", sec)
	}
	return nil
}

// Patch applies a partial JSON document. Keys that are absent are left alone.
// headerByName indexes headerFields by the name a document uses. It is built
// once rather than per call: the fields are fixed at compile time.
var headerByName = func() map[string]headerField {
	m := make(map[string]headerField, len(headerFields))
	for _, f := range headerFields {
		m[f.name] = f
	}
	return m
}()

// patchHeader applies the header object. Four kinds of key can appear in it:
// a mapped field, one of the NUL-terminated strings, one of the values Dump
// derives from a number that is already here, and a typo.
//
// The derived ones are accepted and ignored, because rewriting them would
// fight the field they came from and "dump, change one number, patch the whole
// document back" has to work. A typo is an error: silently dropping a key
// nobody recognizes is how a document quietly does nothing, and nobody finds
// out until the save is loaded.
func patchHeader(out []byte, hdr map[string]any) ([]string, error) {
	var changes []string
	for _, key := range sortedKeys(hdr) {
		f, mapped := headerByName[key]
		if !mapped {
			if sf, isText := StringFieldByName(key); isText {
				sv, isStr := hdr[key].(string)
				if !isStr {
					return nil, fmt.Errorf("header.%s must be a string", key)
				}
				old := GetString(out, sf)
				if err := SetString(out, sf, sv); err != nil {
					return nil, fmt.Errorf("header.%s: %w", key, err)
				}
				if old != sv {
					changes = append(changes, fmt.Sprintf("header.%s: %q -> %q", key, old, sv))
				}
				continue
			}
			if derivedHeader[key] {
				continue
			}
			return nil, fmt.Errorf("unknown key header.%s", key)
		}
		nv, err := asInt(hdr[key])
		if err != nil {
			return nil, fmt.Errorf("header.%s: %w", key, err)
		}
		// A read-only field is only an error when the document actually asks
		// to move it. Rejecting one that still holds its dumped value would
		// make the obvious workflow -- dump, change one number, patch the whole
		// document back -- fail on a field nobody touched.
		if ReadonlyHeader[key] {
			if readField(out, f.off, f.kind) != nv {
				return nil, fmt.Errorf("header.%s is read-only", key)
			}
			continue
		}
		if err := fits(f.kind, "header."+key, nv); err != nil {
			return nil, err
		}
		old := readField(out, f.off, f.kind)
		writeField(out, f.off, f.kind, nv)
		if old != nv {
			changes = append(changes, fmt.Sprintf("header.%s: %d -> %d", key, old, nv))
		}
	}
	return changes, nil
}

// patchCharacters applies the per-character section: stats, the current weapon
// slot, the ability array, the AI settings and the four equipment arrays.
func patchCharacters(out []byte, chars map[string]any) ([]string, error) {
	var changes []string
	for _, cname := range sortedKeys(chars) {
		ci := CharIndex(cname)
		if ci < 0 {
			return nil, fmt.Errorf("unknown character %q", cname)
		}
		entry, ok := chars[cname].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("characters.%s must be an object", cname)
		}
		for _, key := range sortedKeys(entry) {
			switch key {
			case "abilities":
				c, err := patchAbilities(out, ci, cname, entry[key])
				if err != nil {
					return nil, err
				}
				changes = append(changes, c...)
				continue
			case "current_weapon":
				c, err := patchCurrentWeapon(out, ci, cname, entry[key])
				if err != nil {
					return nil, err
				}
				changes = append(changes, c...)
				continue
			case "ai":
				c, err := patchAI(out, ci, cname, entry[key])
				if err != nil {
					return nil, err
				}
				changes = append(changes, c...)
				continue
			case "equipment":
				c, err := patchEquipment(out, ci, cname, entry[key])
				if err != nil {
					return nil, err
				}
				changes = append(changes, c...)
				continue
			}
			st, ok := findStat(key)
			if !ok {
				return nil, fmt.Errorf("unknown key characters.%s.%s", cname, key)
			}
			nv, err := asInt(entry[key])
			if err != nil {
				return nil, fmt.Errorf("characters.%s.%s: %w", cname, key, err)
			}
			if err := fits(st.kind, fmt.Sprintf("characters.%s.%s", cname, key), nv); err != nil {
				return nil, err
			}
			off := charOffset(ci) + st.off
			old := readField(out, off, st.kind)
			writeField(out, off, st.kind, nv)
			if old != nv {
				changes = append(changes, fmt.Sprintf("%s.%s: %d -> %d", cname, key, old, nv))
			}
		}
	}
	return changes, nil
}

// patchAbilities writes the 512-entry ability array of one character. The keys
// are ability ids, which a dump writes in hex, so they are parsed with a base
// of zero rather than assumed decimal.
func patchAbilities(out []byte, ci int, cname string, raw any) ([]string, error) {
	abil, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("characters.%s.abilities must be an object", cname)
	}
	var changes []string
	for _, aidS := range sortedKeys(abil) {
		aid64, err := strconv.ParseInt(aidS, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("bad ability id %q", aidS)
		}
		word, err := abilityWord(abil[aidS])
		if err != nil {
			return nil, fmt.Errorf("ability %s: %w", aidS, err)
		}
		aid := int(aid64)
		old := GetAbility(out, ci, aid)
		SetAbility(out, ci, aid, word)
		if old != word {
			changes = append(changes, fmt.Sprintf(
				"%s.ability 0x%03X %s: 0x%08X -> 0x%08X",
				cname, aid, AbilityName(aid), old, word))
		}
	}
	return changes, nil
}

func patchCurrentWeapon(out []byte, ci int, cname string, raw any) ([]string, error) {
	nv, err := asInt(raw)
	if err != nil {
		return nil, fmt.Errorf("characters.%s.current_weapon: %w", cname, err)
	}
	if nv < 0 || nv >= WeaponSlots {
		return nil, fmt.Errorf("characters.%s.current_weapon: slot %d is outside 0-%d",
			cname, nv, WeaponSlots-1)
	}
	old := GetCurrentWeapon(out, ci)
	if old == int(nv) {
		return nil, nil
	}
	SetCurrentWeapon(out, ci, int(nv))
	return []string{fmt.Sprintf("%s.current_weapon: %d -> %d", cname, old, nv)}, nil
}

// patchInventory applies the 0x400-entry item array. An entry may be the whole
// dumped object or the bare count, and a count of zero clears the flags too,
// which is how an item is removed rather than left at zero held.
func patchInventory(out []byte, inv map[string]any) ([]string, error) {
	var changes []string
	for _, idS := range sortedKeys(inv) {
		id64, err := strconv.ParseInt(idS, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("bad item id %q", idS)
		}
		id := int(id64)
		if id < 0 || id >= InventoryCount {
			return nil, fmt.Errorf("item id %d out of range", id)
		}
		count, flags := int64(0), int64(ItemFresh)
		switch t := inv[idS].(type) {
		case map[string]any:
			if c, ok := t["count"]; ok {
				if count, err = asInt(c); err != nil {
					return nil, err
				}
			}
			if fv, ok := t["flags"]; ok {
				if flags, err = asInt(fv); err != nil {
					return nil, err
				}
			}
		default:
			if count, err = asInt(inv[idS]); err != nil {
				return nil, err
			}
		}
		where := fmt.Sprintf("inventory.%d", id)
		if err := fits("u8", where+".count", count); err != nil {
			return nil, err
		}
		if err := fits("u8", where+".flags", flags); err != nil {
			return nil, err
		}
		oldC, oldF := GetItem(out, id)
		if count == 0 {
			flags = 0
		}
		SetItem(out, id, int(count), byte(flags))
		newC, newF := GetItem(out, id)
		// Report what the save holds, not what the document asked for. These
		// two used to disagree: the comparison read the stored value and the
		// message printed the requested one, so a clamped count was announced
		// as though it had been written. materials and keychain_upgrades have
		// always reported the stored value.
		if oldC != newC || oldF != newF {
			changes = append(changes, fmt.Sprintf("inventory %d %s: x%d -> x%d",
				id, ItemName(id), oldC, newC))
		}
	}
	return changes, nil
}

// Patch applies a JSON document to a save and reports what it changed.
//
// The order below is the order of the changes it reports, and it is fixed so
// that a document touching several regions reads the same way every time. The
// header goes first because the munny ledger and the two mirrored fields are
// reconciled against what they held before any of it was written.
func Patch(plain, doc []byte) ([]byte, []string, error) {
	var d map[string]any
	if err := json.Unmarshal(doc, &d); err != nil {
		return nil, nil, err
	}
	if f, ok := d["_format"].(string); ok && f != DocFormat {
		return nil, nil, fmt.Errorf("unsupported document format %q", f)
	}
	out := make([]byte, len(plain))
	copy(out, plain)

	slot := IsSlot(out)
	var ledgerBefore [3]int64
	var mirrorsBefore [][2]int64
	if slot {
		ledgerBefore = readMunnyLedger(out)
		mirrorsBefore = readMirrors(out)
	} else if err := refuseSlotOnlyKeys(d, headerByName); err != nil {
		// Dump stops at the difficulty byte for the small system file, and
		// Patch has to draw the same line: bonus_hp is at 0xB49C and a system
		// file ends around 0x7B20, so writing one would index past the buffer.
		return nil, nil, err
	}

	var changes []string
	apply := func(key string, fn func([]byte, map[string]any) ([]string, error)) error {
		raw, ok := d[key]
		if !ok {
			return nil
		}
		m, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", key)
		}
		c, err := fn(out, m)
		changes = append(changes, c...)
		return err
	}

	if err := apply("header", patchHeader); err != nil {
		return nil, nil, err
	}
	if _, hasHeader := d["header"]; hasHeader && slot {
		if err := reconcileMunny(out, ledgerBefore, &changes); err != nil {
			return nil, nil, err
		}
		reconcileMirrors(out, mirrorsBefore, &changes)
	}
	if err := apply("characters", patchCharacters); err != nil {
		return nil, nil, err
	}
	if err := apply("inventory", patchInventory); err != nil {
		return nil, nil, err
	}
	for _, sec := range patchSections {
		if err := apply(sec.key, sec.apply); err != nil {
			return nil, nil, err
		}
	}
	return out, changes, nil
}

// scalar is the "id" or "count" style value a dumped entry carries. A patch
// may write the whole object back unchanged or just the bare number, because
// editing a dump by hand and deleting the noise around the number is the
// obvious thing to do and should work.
func scalar(v any, field string) (int64, error) {
	if m, ok := v.(map[string]any); ok {
		inner, ok := m[field]
		if !ok {
			return 0, fmt.Errorf("object needs a %q key", field)
		}
		return asInt(inner)
	}
	return asInt(v)
}

// indexKeys returns the numeric keys of a section, in order, bounded by count.
func indexKeys(m map[string]any, count int, what string) ([]int, error) {
	out := make([]int, 0, len(m))
	for _, k := range sortedKeys(m) {
		n, err := strconv.ParseInt(k, 0, 32)
		if err != nil {
			return nil, fmt.Errorf("bad %s index %q", what, k)
		}
		if n < 0 || n >= int64(count) {
			return nil, fmt.Errorf("%s index %d is outside 0-%d", what, n, count-1)
		}
		out = append(out, int(n))
	}
	return out, nil
}

// patchSections are the whole-document regions, each keyed by index. They run
// after the header and character sections and in a fixed order, so a document
// that touches several of them reports its changes the same way every time.
var patchSections = []struct {
	key   string
	apply func(out []byte, m map[string]any) ([]string, error)
}{
	{"party", func(out []byte, m map[string]any) ([]string, error) {
		idx, err := indexKeys(m, PartySlots, "party")
		if err != nil {
			return nil, err
		}
		var ch []string
		for _, slot := range idx {
			v, err := scalar(m[strconv.Itoa(slot)], "id")
			if err != nil {
				return nil, fmt.Errorf("party.%d: %w", slot, err)
			}
			old := GetParty(out)[slot]
			SetPartySlot(out, slot, int(v))
			if old != int(v) {
				ch = append(ch, fmt.Sprintf("party.%d: %s -> %s",
					slot, PartyName(old), PartyName(int(v))))
			}
		}
		return ch, nil
	}},
	{"shortcuts", func(out []byte, m map[string]any) ([]string, error) {
		idx, err := indexKeys(m, ShortcutPages, "shortcuts")
		if err != nil {
			return nil, err
		}
		var ch []string
		for _, page := range idx {
			g, ok := m[strconv.Itoa(page)].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("shortcuts.%d must be an object", page)
			}
			for b, bname := range ShortcutButtonNames {
				raw, ok := g[bname]
				if !ok {
					continue
				}
				v, err := scalar(raw, "id")
				if err != nil {
					return nil, fmt.Errorf("shortcuts.%d.%s: %w", page, bname, err)
				}
				old := GetShortcut(out, page, b)
				SetShortcut(out, page, b, int(v))
				if old != int(v) {
					ch = append(ch, fmt.Sprintf("shortcuts.%d.%s: %s -> %s",
						page, bname, CommandName(old), CommandName(int(v))))
				}
			}
		}
		return ch, nil
	}},
	{"magic", commandArrayPatch("magic", MagicCount, GetMagic, SetMagic)},
	{"links", commandArrayPatch("links", LinkCount, GetLink, SetLink)},
	{"story_flags", func(out []byte, m map[string]any) ([]string, error) {
		idx, err := indexKeys(m, StoryFlagCount, "story_flags")
		if err != nil {
			return nil, err
		}
		var ch []string
		for _, id := range idx {
			v, err := scalar(m[strconv.Itoa(id)], "value")
			if err != nil {
				return nil, fmt.Errorf("story_flags.%d: %w", id, err)
			}
			if err := fits("i32", fmt.Sprintf("story_flags.%d", id), v); err != nil {
				return nil, err
			}
			old := GetStoryFlag(out, id)
			SetStoryFlag(out, id, int32(v))
			if old != int32(v) {
				ch = append(ch, fmt.Sprintf("story_flags.%d %s: %d -> %d",
					id, StoryFlagName(id), old, v))
			}
		}
		return ch, nil
	}},
	{"materials", func(out []byte, m map[string]any) ([]string, error) {
		idx, err := indexKeys(m, MaterialCount, "materials")
		if err != nil {
			return nil, err
		}
		var ch []string
		for _, id := range idx {
			v, err := scalar(m[strconv.Itoa(id)], "count")
			if err != nil {
				return nil, fmt.Errorf("materials.%d: %w", id, err)
			}
			if err := fits("u16", fmt.Sprintf("materials.%d", id), v); err != nil {
				return nil, err
			}
			old := GetMaterial(out, id)
			SetMaterial(out, id, int(v))
			if now := GetMaterial(out, id); old != now {
				ch = append(ch, fmt.Sprintf("materials %d %s: x%d -> x%d",
					id, MaterialName(id), old, now))
			}
		}
		return ch, nil
	}},
	{"keychain_upgrades", func(out []byte, m map[string]any) ([]string, error) {
		idx, err := indexKeys(m, KeychainUpgradeCount, "keychain_upgrades")
		if err != nil {
			return nil, err
		}
		var ch []string
		for _, i := range idx {
			v, err := scalar(m[strconv.Itoa(i)], "value")
			if err != nil {
				return nil, fmt.Errorf("keychain_upgrades.%d: %w", i, err)
			}
			if err := fits("u8", fmt.Sprintf("keychain_upgrades.%d", i), v); err != nil {
				return nil, err
			}
			old := GetKeychainUpgrade(out, i)
			SetKeychainUpgrade(out, i, int(v))
			if now := GetKeychainUpgrade(out, i); old != now {
				ch = append(ch, fmt.Sprintf("keychain_upgrades.%d: %d -> %d", i, old, now))
			}
		}
		return ch, nil
	}},
	{"records", patchRecords},
}

func commandArrayPatch(name string, count int, get func([]byte, int) int,
	set func([]byte, int, int)) func([]byte, map[string]any) ([]string, error) {
	return func(out []byte, m map[string]any) ([]string, error) {
		idx, err := indexKeys(m, count, name)
		if err != nil {
			return nil, err
		}
		var ch []string
		for _, i := range idx {
			v, err := scalar(m[strconv.Itoa(i)], "id")
			if err != nil {
				return nil, fmt.Errorf("%s.%d: %w", name, i, err)
			}
			old := get(out, i)
			set(out, i, int(v))
			if old != int(v) {
				ch = append(ch, fmt.Sprintf("%s.%d: %s -> %s",
					name, i, CommandName(old), CommandName(int(v))))
			}
		}
		return ch, nil
	}
}

func patchAI(out []byte, ci int, cname string, raw any) ([]string, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("characters.%s.ai must be an object", cname)
	}
	a := GetAI(out, ci)
	before := a
	var ch []string
	for _, f := range aiFields {
		v, ok := m[f.name]
		if !ok {
			continue
		}
		n, err := scalar(v, "id")
		if err != nil {
			return nil, fmt.Errorf("characters.%s.ai.%s: %w", cname, f.name, err)
		}
		if _, known := f.table[int(n)]; !known {
			return nil, fmt.Errorf("characters.%s.ai.%s: %d is not a known setting",
				cname, f.name, n)
		}
		f.set(&a, int(n))
	}
	if v, ok := m["recovery_targets"]; ok {
		where := fmt.Sprintf("characters.%s.ai.recovery_targets", cname)
		n, err := asInt(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		if err := fits("u8", where, n); err != nil {
			return nil, err
		}
		a.RecoveryTargets = int(n)
	}
	SetAI(out, ci, a)
	for _, f := range aiFields {
		if f.get(before) != f.get(a) {
			ch = append(ch, fmt.Sprintf("%s.ai.%s: %s -> %s", cname, f.name,
				lookup(f.table, f.get(before)), lookup(f.table, f.get(a))))
		}
	}
	if before.RecoveryTargets != a.RecoveryTargets {
		ch = append(ch, fmt.Sprintf("%s.ai.recovery_targets: %d -> %d",
			cname, before.RecoveryTargets, a.RecoveryTargets))
	}
	return ch, nil
}

func patchEquipment(out []byte, ci int, cname string, raw any) ([]string, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("characters.%s.equipment must be an object", cname)
	}
	byName := map[string]int{}
	for i, k := range EquipKinds {
		byName[k.Name] = i
	}
	var ch []string
	for _, kname := range sortedKeys(m) {
		ki, ok := byName[kname]
		if !ok {
			return nil, fmt.Errorf("characters.%s.equipment.%s is not an equipment array",
				cname, kname)
		}
		k := EquipKinds[ki]
		slots, ok := m[kname].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("characters.%s.equipment.%s must be an object", cname, kname)
		}
		idx, err := indexKeys(slots, k.Slots, "characters."+cname+".equipment."+kname)
		if err != nil {
			return nil, err
		}
		for _, s := range idx {
			where := fmt.Sprintf("characters.%s.equipment.%s.%d", cname, kname, s)
			e, err := readEquip(slots[strconv.Itoa(s)], where)
			if err != nil {
				return nil, err
			}
			old := GetEquip(out, ci, k.Off, s)
			SetEquip(out, ci, k.Off, s, e)
			if old != e {
				ch = append(ch, fmt.Sprintf("%s.%s.%d: %s -> %s",
					cname, kname, s, equipLabel(old), equipLabel(e)))
			}
		}
	}
	return ch, nil
}

func equipLabel(e Equip) string {
	if e.Empty() {
		return "(empty)"
	}
	return e.Name()
}

// readEquip decodes one slot. Writing null clears it, which is how a slot is
// emptied given the dump omits empty slots entirely.
func readEquip(v any, where string) (Equip, error) {
	if v == nil {
		return Equip{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return Equip{}, fmt.Errorf("%s must be an object or null", where)
	}
	e := Equip{Enabled: true}
	id, err := scalar(m, "id")
	if err != nil {
		return Equip{}, fmt.Errorf("%s: %w", where, err)
	}
	t, ok := m["type"]
	if !ok {
		return Equip{}, fmt.Errorf("%s needs a \"type\" key: an id means nothing without it", where)
	}
	tn, err := asInt(t)
	if err != nil {
		return Equip{}, fmt.Errorf("%s.type: %w", where, err)
	}
	if id < 0 || id > 0xFF {
		return Equip{}, fmt.Errorf("%s.id %d is outside 0-255", where, id)
	}
	if _, known := ItemTypes[int(tn)]; !known {
		return Equip{}, fmt.Errorf("%s.type %d is not a known item type", where, tn)
	}
	e.ID, e.ItemType = int(id), int(tn)
	if en, ok := m["enabled"]; ok {
		n, err := asInt(en)
		if err != nil {
			return Equip{}, fmt.Errorf("%s.enabled: %w", where, err)
		}
		e.Enabled = n != 0
	}
	return e, nil
}

func abilityWord(v any) (uint32, error) {
	if m, ok := v.(map[string]any); ok {
		w, ok := m["word"]
		if !ok {
			return 0, fmt.Errorf("ability object needs a \"word\" key")
		}
		n, err := asInt(w)
		return uint32(n), err
	}
	n, err := asInt(v)
	return uint32(n), err
}

func findStat(name string) (struct {
	name string
	off  int
	kind string
}, bool) {
	for _, st := range charStats {
		if st.name == name {
			return st, true
		}
	}
	return charStats[0], false
}

// CharIndex is the position of a character in CharNames, or -1. It is exported
// because the CLI needs the same lookup for -character and was carrying its own
// copy of this loop.
func CharIndex(name string) int {
	for i, n := range CharNames {
		if n == name {
			return i
		}
	}
	return -1
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// patchRecords applies the use counters, which every save carries, and the
// bests, which live past the point a short save reaches. Asking for a best on
// a save that stops before the block is an error naming the reason, not a
// write into whatever happens to be at that address.
func patchRecords(out []byte, m map[string]any) ([]string, error) {
	var ch []string
	full := HasRecords(out)

	need := func(what string) error {
		if full {
			return nil
		}
		return fmt.Errorf("records.%s needs the record block, and this save is only %d bytes: "+
			"it stops before %s", what, len(out), hexOff(RecordsOff))
	}

	for _, r := range []struct {
		key     string
		off     int
		count   int
		names   map[int]string
		getUse  func([]byte, int) int
		setUse  func([]byte, int, int)
		getBest func([]byte, int) int
		setBest func([]byte, int, int)
		// bestKind is the width the best is stored at, and the two rows differ:
		// an attraction best is an i32 and a shotlock best an i16.
		bestKind string
	}{
		{"attractions", AttractionUseOff, AttractionUseCount, RecordAttractions,
			GetAttractionUse, SetAttractionUse,
			func(p []byte, i int) int { return int(GetAttractionHigh(p, i)) },
			func(p []byte, i, v int) { SetAttractionHigh(p, i, int32(v)) }, "i32"},
		{"shotlocks", ShotlockUseOff, ShotlockUseCount, RecordShotlocks,
			GetShotlockUse, SetShotlockUse, GetShotlockHigh, SetShotlockHigh, "i16"},
	} {
		raw, ok := m[r.key]
		if !ok {
			continue
		}
		sub, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("records.%s must be an object", r.key)
		}
		idx, err := indexKeys(sub, r.count, "records."+r.key)
		if err != nil {
			return nil, err
		}
		for _, id := range idx {
			entry, isObj := sub[strconv.Itoa(id)].(map[string]any)
			where := fmt.Sprintf("records.%s.%d", r.key, id)
			// A bare number is the use count, the same way a bare number is
			// the count everywhere else a dump writes one.
			if !isObj {
				entry = map[string]any{"uses": sub[strconv.Itoa(id)]}
			}
			if v, ok := entry["uses"]; ok {
				n, err := asInt(v)
				if err != nil {
					return nil, fmt.Errorf("%s.uses: %w", where, err)
				}
				if err := fits("u16", where+".uses", n); err != nil {
					return nil, err
				}
				old := r.getUse(out, id)
				r.setUse(out, id, int(n))
				if now := r.getUse(out, id); old != now {
					ch = append(ch, fmt.Sprintf("records.%s %d %s: %d -> %d uses",
						r.key, id, lookup(r.names, id), old, now))
				}
			}
			if v, ok := entry["high_score"]; ok {
				if err := need(r.key); err != nil {
					return nil, err
				}
				n, err := asInt(v)
				if err != nil {
					return nil, fmt.Errorf("%s.high_score: %w", where, err)
				}
				if err := fits(r.bestKind, where+".high_score", n); err != nil {
					return nil, err
				}
				old := r.getBest(out, id)
				r.setBest(out, id, int(n))
				if now := r.getBest(out, id); old != now {
					ch = append(ch, fmt.Sprintf("records.%s %d %s: best %d -> %d",
						r.key, id, lookup(r.names, id), old, now))
				}
			}
		}
	}

	if raw, ok := m["minigames"]; ok {
		if err := need("minigames"); err != nil {
			return nil, err
		}
		sub, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("records.minigames must be an object")
		}
		for _, key := range sortedKeys(sub) {
			i := indexOfString(RecordScores, key)
			if i < 0 {
				return nil, fmt.Errorf("unknown key records.minigames.%s", key)
			}
			n, err := scalar(sub[key], "value")
			if err != nil {
				return nil, fmt.Errorf("records.minigames.%s: %w", key, err)
			}
			if err := fits("i32", "records.minigames."+key, n); err != nil {
				return nil, err
			}
			old := GetRecordScore(out, i)
			SetRecordScore(out, i, int32(n))
			if old != int32(n) {
				ch = append(ch, fmt.Sprintf("records.minigames.%s: %d -> %d", key, old, n))
			}
		}
	}

	if raw, ok := m["flans"]; ok {
		if err := need("flans"); err != nil {
			return nil, err
		}
		sub, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("records.flans must be an object")
		}
		for _, key := range sortedKeys(sub) {
			i := indexOfString(FlanNames, key)
			if i < 0 {
				return nil, fmt.Errorf("unknown flan %q", key)
			}
			e, ok := sub[key].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("records.flans.%s must be an object", key)
			}
			old := GetFlan(out, i)
			f := old
			for _, fld := range []struct {
				key string
				set func(*Flan, int32)
			}{
				{"high_score", func(x *Flan, v int32) { x.HighScore = v }},
				{"high_score_2", func(x *Flan, v int32) { x.HighScore2 = v }},
				{"attempts", func(x *Flan, v int32) { x.Attempts = v }},
			} {
				v, ok := e[fld.key]
				if !ok {
					continue
				}
				n, err := asInt(v)
				if err != nil {
					return nil, fmt.Errorf("records.flans.%s.%s: %w", key, fld.key, err)
				}
				if err := fits("i32", fmt.Sprintf("records.flans.%s.%s", key, fld.key), n); err != nil {
					return nil, err
				}
				fld.set(&f, int32(n))
			}
			SetFlan(out, i, f)
			if old != f {
				ch = append(ch, fmt.Sprintf("records.flans.%s: %d/%d/%d -> %d/%d/%d", key,
					old.HighScore, old.HighScore2, old.Attempts,
					f.HighScore, f.HighScore2, f.Attempts))
			}
		}
	}

	if raw, ok := m["album"]; ok {
		if err := need("album"); err != nil {
			return nil, err
		}
		sub, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("records.album must be an object")
		}
		for _, key := range sortedKeys(sub) {
			if key != "photo_max_count" {
				return nil, fmt.Errorf("unknown key records.album.%s", key)
			}
			n, err := asInt(sub[key])
			if err != nil {
				return nil, fmt.Errorf("records.album.%s: %w", key, err)
			}
			if err := fits("i32", "records.album."+key, n); err != nil {
				return nil, err
			}
			old := GetPhotoMaxCount(out)
			SetPhotoMaxCount(out, int32(n))
			if old != int32(n) {
				ch = append(ch, fmt.Sprintf("records.album.photo_max_count: %d -> %d", old, n))
			}
		}
	}
	return ch, nil
}

func indexOfString(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
