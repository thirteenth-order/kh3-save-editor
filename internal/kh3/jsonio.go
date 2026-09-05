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
	{0x2C, "u8", "level"},
	{0x30, "u8", "desire_choice"},
	{0x31, "u8", "power_choice"},
	{0x39, "u8", "save_clear"},
	{0x54, "u8", "location"},
	{0x60, "u8", "save_icon"},
	{0x70, "u32", "enemies_defeated"},
	{0x5B8, "u16", "saves_count"},
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
	doc.set("_note", "Absent keys are left unchanged on patch. The account id is "+
		"omitted unless explicitly requested, because a dump is often shared. "+
		"Read-only: "+strings.Join(ro, ", "))
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
		hdr := ReadHeader(plain)
		h.set("playtime", hdr.Playtime())
		h.set("map_path", hdr.MapPath)
		h.set("map_spawn", hdr.MapSpawn)
	}
	doc.set("header", h)

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

	return json.MarshalIndent(doc, "", "  ")
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

// Patch applies a partial JSON document. Keys that are absent are left alone.
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
	var changes []string

	byName := map[string]headerField{}
	for _, f := range headerFields {
		byName[f.name] = f
	}

	if hdr, ok := d["header"].(map[string]any); ok {
		for _, key := range sortedKeys(hdr) {
			if ReadonlyHeader[key] {
				return nil, nil, fmt.Errorf("header.%s is read-only", key)
			}
			f, ok := byName[key]
			if !ok {
				continue // display-only keys such as playtime / map_path
			}
			nv, err := asInt(hdr[key])
			if err != nil {
				return nil, nil, fmt.Errorf("header.%s: %w", key, err)
			}
			old := readField(out, f.off, f.kind)
			writeField(out, f.off, f.kind, nv)
			if old != nv {
				changes = append(changes, fmt.Sprintf("header.%s: %d -> %d", key, old, nv))
			}
		}
	}

	if chars, ok := d["characters"].(map[string]any); ok {
		for _, cname := range sortedKeys(chars) {
			ci := charIndex(cname)
			if ci < 0 {
				return nil, nil, fmt.Errorf("unknown character %q", cname)
			}
			entry, ok := chars[cname].(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("characters.%s must be an object", cname)
			}
			for _, key := range sortedKeys(entry) {
				if key == "abilities" {
					abil, ok := entry[key].(map[string]any)
					if !ok {
						return nil, nil, fmt.Errorf("characters.%s.abilities must be an object", cname)
					}
					for _, aidS := range sortedKeys(abil) {
						aid64, err := strconv.ParseInt(aidS, 0, 32)
						if err != nil {
							return nil, nil, fmt.Errorf("bad ability id %q", aidS)
						}
						word, err := abilityWord(abil[aidS])
						if err != nil {
							return nil, nil, fmt.Errorf("ability %s: %w", aidS, err)
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
					continue
				}
				st, ok := findStat(key)
				if !ok {
					return nil, nil, fmt.Errorf("unknown key characters.%s.%s", cname, key)
				}
				nv, err := asInt(entry[key])
				if err != nil {
					return nil, nil, fmt.Errorf("characters.%s.%s: %w", cname, key, err)
				}
				off := charOffset(ci) + st.off
				old := readField(out, off, st.kind)
				writeField(out, off, st.kind, nv)
				if old != nv {
					changes = append(changes, fmt.Sprintf("%s.%s: %d -> %d", cname, key, old, nv))
				}
			}
		}
	}

	if inv, ok := d["inventory"].(map[string]any); ok {
		for _, idS := range sortedKeys(inv) {
			id64, err := strconv.ParseInt(idS, 0, 32)
			if err != nil {
				return nil, nil, fmt.Errorf("bad item id %q", idS)
			}
			id := int(id64)
			if id < 0 || id >= InventoryCount {
				return nil, nil, fmt.Errorf("item id %d out of range", id)
			}
			count, flags := int64(0), int64(ItemFresh)
			switch t := inv[idS].(type) {
			case map[string]any:
				if c, ok := t["count"]; ok {
					if count, err = asInt(c); err != nil {
						return nil, nil, err
					}
				}
				if fv, ok := t["flags"]; ok {
					if flags, err = asInt(fv); err != nil {
						return nil, nil, err
					}
				}
			default:
				if count, err = asInt(inv[idS]); err != nil {
					return nil, nil, err
				}
			}
			oldC, oldF := GetItem(out, id)
			if count == 0 {
				flags = 0
			}
			SetItem(out, id, int(count), byte(flags))
			newC, newF := GetItem(out, id)
			if oldC != newC || oldF != newF {
				changes = append(changes, fmt.Sprintf("inventory %d %s: x%d -> x%d",
					id, ItemName(id), oldC, count))
			}
		}
	}
	return out, changes, nil
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

func charIndex(name string) int {
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
