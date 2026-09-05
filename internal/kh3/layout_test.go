package kh3

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestEquipmentArraysDoNotOverlap is the test that matters most here. The four
// equipment arrays are contiguous from 0x80 to 0x158 with no gap, so an off-by-
// one in any offset or slot count silently writes into the neighboring array
// and the mistake only shows up as a corrupted save. Filling every slot of
// every array for every character with a distinct value and reading them all
// back catches that, and nothing weaker does.
func TestEquipmentArraysDoNotOverlap(t *testing.T) {
	p := buildPlain(1)

	want := map[[3]int]Equip{}
	n := 0
	for ci := range CharNames {
		for ki, k := range EquipKinds {
			for s := 0; s < k.Slots; s++ {
				n++
				e := Equip{ID: n % 251, ItemType: n % 11, Enabled: n%2 == 0}
				want[[3]int{ci, ki, s}] = e
				SetEquip(p, ci, k.Off, s, e)
			}
		}
	}

	for key, e := range want {
		got := GetEquip(p, key[0], EquipKinds[key[1]].Off, key[2])
		if got != e {
			t.Errorf("%s %s slot %d = %+v, want %+v",
				CharNames[key[0]], EquipKinds[key[1]].Name, key[2], got, e)
		}
	}

	// The arrays must also not have reached into the AI bytes just past them.
	for ci := range CharNames {
		if got := GetAI(p, ci); got != (AI{}) {
			t.Errorf("%s AI = %+v after equipment writes, want zero", CharNames[ci], got)
		}
	}
}

func TestEquipmentDoesNotOverlapNeighboringCharacters(t *testing.T) {
	p := buildPlain(1)
	last := len(CharNames) - 1
	for s := 0; s < ItemSlots; s++ {
		SetEquip(p, 0, ItemSlotOff, s, Equip{ID: 9, ItemType: ItemTypeConsumable, Enabled: true})
	}
	for _, ci := range []int{1, last} {
		for _, k := range EquipKinds {
			for s := 0; s < k.Slots; s++ {
				if got := GetEquip(p, ci, k.Off, s); !got.Empty() {
					t.Fatalf("%s %s slot %d = %+v after writing only Sora",
						CharNames[ci], k.Name, s, got)
				}
			}
		}
	}
}

func TestAIRoundTrip(t *testing.T) {
	p := buildPlain(1)
	want := AI{CombatStyle: 3, AbilityUse: 2, RecoveryUse: 1, RecoveryTargets: 15}
	SetAI(p, Sora, want)
	if got := GetAI(p, Sora); got != want {
		t.Errorf("AI = %+v, want %+v", got, want)
	}
	if got := GetAI(p, 1); got != (AI{}) {
		t.Errorf("Donald AI = %+v, want zero", got)
	}
	// The AI bytes sit right after the item array and right before abilities.
	if got := GetAbility(p, Sora, 0); got != AbilityAbsent {
		t.Errorf("ability 0 = 0x%08X after an AI write, want 0x%08X", got, AbilityAbsent)
	}
}

// TestEquipNameUsesTheTypeByte is the "keyblade that reads as a snack" guard:
// the same id means a different thing in each index space, so the type byte is
// what has to pick the table.
func TestEquipNameUsesTheTypeByte(t *testing.T) {
	const id = 3
	seen := map[string]int{}
	for _, tp := range []int{ItemTypeWeapon, ItemTypeArmor, ItemTypeAccessory, ItemTypeSynthesis} {
		name := EquipName(id, tp)
		if name == "?" {
			t.Errorf("id %d type %d has no name", id, tp)
		}
		seen[name]++
	}
	if len(seen) < 3 {
		t.Errorf("id %d reads as %v across four item types; the type byte is being ignored", id, seen)
	}
	if got := EquipName(id, 200); got != "?" {
		t.Errorf("unknown item type named %q, want %q", got, "?")
	}
}

func TestArrayAccessorsRefuseOutOfRangeIndexes(t *testing.T) {
	p := buildPlain(1)
	before := append([]byte(nil), p...)

	SetMaterial(p, MaterialCount, 99)
	SetMaterial(p, -1, 99)
	SetStoryFlag(p, StoryFlagCount, 99)
	SetStoryFlag(p, -1, 99)
	SetMagic(p, MagicCount, 99)
	SetLink(p, LinkCount, 99)
	SetShortcut(p, ShortcutPages, 0, 99)
	SetShortcut(p, 0, ShortcutButtons, 99)
	SetPartySlot(p, PartySlots, 99)

	if !equalBytes(p, before) {
		t.Error("an out-of-range index wrote to the save")
	}
	if got := GetMaterial(p, MaterialCount); got != 0 {
		t.Errorf("out-of-range material read %d, want 0", got)
	}
}

// TestMaterialCountSaturates keeps a big number from wrapping into a small one,
// which is what makes "give me 99999 of everything" quietly hand back 34.
func TestMaterialCountSaturates(t *testing.T) {
	p := buildPlain(1)
	SetMaterial(p, 0, 99999)
	if got := GetMaterial(p, 0); got != 0xFFFF {
		t.Errorf("material count = %d, want %d", got, 0xFFFF)
	}
	SetMaterial(p, 1, -5)
	if got := GetMaterial(p, 1); got != 0 {
		t.Errorf("negative material count = %d, want 0", got)
	}
}

func TestCharNamesMatchTheGeneratedTable(t *testing.T) {
	if len(CharNames) != CharCount {
		t.Fatalf("CharNames has %d entries, want %d", len(CharNames), CharCount)
	}
	for i, n := range CharNames {
		if n == "" {
			t.Errorf("character %d has no name", i)
		}
		if PlayableCharacters[i] != n {
			t.Errorf("character %d: CharNames %q, table %q", i, n, PlayableCharacters[i])
		}
	}
}

// TestDumpPatchRoundTripIsByteIdentical is the whole contract of the JSON
// surface: everything Dump writes, Patch has to be able to read back without
// moving a byte. Anything Dump can say but Patch cannot parse shows up here.
func TestDumpPatchRoundTripIsByteIdentical(t *testing.T) {
	p := buildPlain(3)
	// Give every new region a non-default value, so the round trip is
	// exercising real content rather than a save full of zeroes.
	SetPartySlot(p, 0, 11)
	SetPartySlot(p, 1, 12)
	SetShortcut(p, 0, 0, 29)
	SetShortcut(p, 2, 3, 41)
	SetMagic(p, 0, 29)
	SetLink(p, 1, 0x60)
	SetStoryFlag(p, 29, 2101)
	SetMaterial(p, 0, 5)
	SetMaterial(p, 44, 1234)
	SetCrabs(p, 12)
	SetU16Array(p, AttractionUseOff, AttractionUseCount, 2, 3)
	SetU16Array(p, ShotlockUseOff, ShotlockUseCount, 28, 1)
	SetCurrentWeapon(p, Sora, 2)
	SetAI(p, 1, AI{CombatStyle: 2, AbilityUse: 1, RecoveryUse: 2, RecoveryTargets: 15})
	SetEquip(p, Sora, WeaponSlotOff, 0, Equip{ID: 1, ItemType: ItemTypeWeapon, Enabled: true})
	SetEquip(p, Sora, AccessorySlotOff, 0, Equip{ID: 17, ItemType: ItemTypeAccessory, Enabled: true})
	SetEquip(p, 1, ItemSlotOff, 3, Equip{ID: 1, ItemType: ItemTypeConsumable, Enabled: false})

	doc, err := Dump(p, "", len(CharNames))
	if err != nil {
		t.Fatal(err)
	}
	out, changes, err := Patch(p, doc)
	if err != nil {
		t.Fatalf("patching a save with its own dump: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("round trip reported %d changes, want none:\n  %s",
			len(changes), strings.Join(changes, "\n  "))
	}
	if !equalBytes(out, p) {
		t.Error("round trip through Dump and Patch changed the save")
	}
}

// TestReadOnlyHeaderRejectsOnlyRealChanges: a full dump carries the checksum
// and filesize, so re-applying one must not fail on fields nobody touched.
func TestReadOnlyHeaderRejectsOnlyRealChanges(t *testing.T) {
	p := buildPlain(1)
	h := ReadHeader(p)

	same := fmt.Sprintf(`{"header":{"checksum_crc32":%d,"munny":7}}`, h.Checksum)
	out, changes, err := Patch(p, []byte(same))
	if err != nil {
		t.Fatalf("unchanged read-only field rejected: %v", err)
	}
	if len(changes) != 1 {
		t.Errorf("changes = %v, want only the munny write", changes)
	}
	if got := ReadHeader(out).Checksum; got != h.Checksum {
		t.Errorf("checksum moved to %d", got)
	}

	moved := fmt.Sprintf(`{"header":{"checksum_crc32":%d}}`, h.Checksum+1)
	if _, _, err := Patch(p, []byte(moved)); err == nil {
		t.Error("changing a read-only field was accepted")
	}
}

func TestPatchRejectsBadLayoutDocuments(t *testing.T) {
	cases := map[string]string{
		"party slot out of range":    `{"party":{"5":1}}`,
		"shortcut page out of range": `{"shortcuts":{"3":{"circle":1}}}`,
		"magic index out of range":   `{"magic":{"6":1}}`,
		"story flag out of range":    `{"story_flags":{"80":1}}`,
		"material out of range":      `{"materials":{"100":1}}`,
		"equip slot out of range":    `{"characters":{"Sora":{"equipment":{"weapons":{"3":{"id":1,"type":3}}}}}}`,
		"equip without a type":       `{"characters":{"Sora":{"equipment":{"weapons":{"0":{"id":1}}}}}}`,
		"equip unknown type":         `{"characters":{"Sora":{"equipment":{"weapons":{"0":{"id":1,"type":99}}}}}}`,
		"equip id out of range":      `{"characters":{"Sora":{"equipment":{"weapons":{"0":{"id":300,"type":3}}}}}}`,
		"unknown equipment array":    `{"characters":{"Sora":{"equipment":{"hats":{"0":{"id":1,"type":3}}}}}}`,
		"unknown ai setting":         `{"characters":{"Sora":{"ai":{"combat_style":99}}}}`,
		"current weapon slot":        `{"characters":{"Sora":{"current_weapon":3}}}`,
		"party not an object":        `{"party":[1,2]}`,
	}
	p := buildPlain(1)
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Patch(p, []byte(doc)); err == nil {
				t.Errorf("%s was accepted", doc)
			}
		})
	}
}

// TestPatchAcceptsBareNumbers: a dump writes {"id": 29, "name": "Fire"} and the
// obvious hand edit is to replace the whole object with 29.
func TestPatchAcceptsBareNumbers(t *testing.T) {
	p := buildPlain(1)
	doc := `{"magic":{"0":29},"materials":{"0":7},"party":{"0":11},
	         "story_flags":{"1":5},"shortcuts":{"0":{"circle":29}},
	         "records":{"attractions":{"0":4}}}`
	out, changes, err := Patch(p, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 6 {
		t.Errorf("changes = %v, want six", changes)
	}
	if got := GetMagic(out, 0); got != 29 {
		t.Errorf("magic 0 = %d, want 29", got)
	}
	if got := GetMaterial(out, 0); got != 7 {
		t.Errorf("material 0 = %d, want 7", got)
	}
	if got := GetAttractionUse(out, 0); got != 4 {
		t.Errorf("attraction 0 = %d, want 4", got)
	}
}

func TestPatchClearsAnEquipmentSlotWithNull(t *testing.T) {
	p := buildPlain(1)
	SetEquip(p, Sora, AccessorySlotOff, 0, Equip{ID: 17, ItemType: ItemTypeAccessory, Enabled: true})
	out, changes, err := Patch(p, []byte(`{"characters":{"Sora":{"equipment":{"accessories":{"0":null}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Errorf("changes = %v, want one", changes)
	}
	if got := GetEquip(out, Sora, AccessorySlotOff, 0); !got.Empty() {
		t.Errorf("slot = %+v after clearing, want empty", got)
	}
}

// TestDumpNamesTheRealSaveShapes checks the tables are wired to the right
// regions: a save at Olympus has to say Olympus, not an index.
func TestDumpNamesRegionsFromTheRightTable(t *testing.T) {
	p := buildPlain(1)
	p[0x18] = 4 // Olympus
	SetMagic(p, 0, 29)
	SetMaterial(p, 0, 1)
	SetPartySlot(p, 0, 11)

	doc, err := Dump(p, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Header struct {
			WorldLogoName string `json:"world_logo_name"`
		}
		Magic     map[string]struct{ Name string }
		Materials map[string]struct{ Name string }
		Party     map[string]struct{ Name string }
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ got, want, what string }{
		{d.Header.WorldLogoName, "Olympus", "world logo"},
		{d.Magic["0"].Name, "Fire", "magic 0"},
		{d.Materials["0"].Name, "Blazing Shard", "material 0"},
		{d.Party["0"].Name, "Donald", "party 0"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
