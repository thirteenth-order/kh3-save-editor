package kh3

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"strings"
	"testing"
)

const testAccount = "76561190000000000"

// buildPlain makes a structurally valid synthetic save. No real save is ever
// committed: they embed a SteamID64 and a playthrough.
func buildPlain(difficulty byte) []byte {
	const plainLen = 0x20000
	fileSize := uint32(plainLen - 24)

	p := make([]byte, plainLen)
	copy(p[0:4], Magic)
	binary.LittleEndian.PutUint32(p[0x04:], fileSize)
	binary.LittleEndian.PutUint16(p[0x08:], 5)
	binary.LittleEndian.PutUint16(p[0x0A:], 2)
	p[0x14] = difficulty
	p[0x2C] = 6

	for ci := range CharNames {
		for aid := 0; aid < AbilityCount; aid++ {
			SetAbility(p, ci, aid, AbilityAbsent)
		}
		SetAbility(p, ci, 0x003, AbilityInnateEquipped)
		SetStat(p, ci, StatHP, 145)
		SetStat(p, ci, StatMP, 115)
	}
	binary.LittleEndian.PutUint32(p[0x0C:], crc32.ChecksumIEEE(p[0x10:0x10+fileSize]))
	return p
}

func key(t *testing.T) []byte {
	t.Helper()
	k, err := DeriveKey(testAccount)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestDeriveKeyIs32PrintableASCII(t *testing.T) {
	k := key(t)
	if len(k) != 32 {
		t.Fatalf("key length %d, want 32", len(k))
	}
	for i, b := range k {
		if b <= 0x20 || b >= 0x7F {
			t.Errorf("byte %d = 0x%02x is not printable ASCII", i, b)
		}
	}
}

func TestDeriveKeyRejectsEmptyAccount(t *testing.T) {
	if _, err := DeriveKey(""); err == nil {
		t.Fatal("expected an error for an empty account id")
	}
}

func TestRoundTripIsByteExact(t *testing.T) {
	k, p := key(t), buildPlain(1)
	blob, err := Wrap(p, k)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Unwrap(blob, k)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, p) {
		t.Fatal("round trip is not byte exact")
	}
}

func TestContainerShape(t *testing.T) {
	k, p := key(t), buildPlain(1)
	blob, _ := Wrap(p, k)
	if len(blob) != len(p)+TrailerLen {
		t.Errorf("length %d, want %d", len(blob), len(p)+TrailerLen)
	}
	if blob[len(blob)-TrailerLen] != TrailerSep {
		t.Error("trailer separator is not 0x08")
	}
	if (len(blob)-TrailerLen)%BlockSize != 0 {
		t.Error("ciphertext is not block aligned")
	}
}

func TestECBRepeatsIdenticalBlocks(t *testing.T) {
	k, p := key(t), buildPlain(1)
	blob, _ := Wrap(p, k)
	body := blob[:len(blob)-TrailerLen]
	seen := map[string]bool{}
	for i := 0; i+BlockSize <= len(body); i += BlockSize {
		seen[string(body[i:i+BlockSize])] = true
	}
	if total := len(body) / BlockSize; len(seen) >= total/10 {
		t.Errorf("%d unique blocks of %d; ECB should collapse the zero padding", len(seen), total)
	}
}

func TestStaleCRCIsRepaired(t *testing.T) {
	k, p := key(t), buildPlain(1)
	tampered := append([]byte(nil), p...)
	binary.LittleEndian.PutUint32(tampered[0x0C:], 0xDEADBEEF)
	tampered[0x14] = 3
	blob, err := Wrap(tampered, k)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Unwrap(blob, k)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(back[0x0C:]) == 0xDEADBEEF {
		t.Error("CRC was not recomputed")
	}
	if back[0x14] != 3 {
		t.Error("edit did not survive")
	}
}

func TestUnwrapRejectsBadInput(t *testing.T) {
	k, p := key(t), buildPlain(1)
	good, _ := Wrap(p, k)

	cases := map[string]func() []byte{
		"wrong key":       func() []byte { return good },
		"corrupt body":    func() []byte { b := append([]byte(nil), good...); b[1000] ^= 0xFF; return b },
		"bad md5":         func() []byte { b := append([]byte(nil), good...); b[len(b)-1] ^= 0xFF; return b },
		"bad separator":   func() []byte { b := append([]byte(nil), good...); b[len(b)-TrailerLen] = 9; return b },
		"truncated":       func() []byte { return good[:100] },
		"misaligned body": func() []byte { return good[:len(good)-1] },
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			useKey := k
			if name == "wrong key" {
				useKey, _ = DeriveKey("76561190000000001")
			}
			if _, err := Unwrap(mk(), useKey); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestSwapWritesDifficulty(t *testing.T) {
	for target := byte(0); target <= 3; target++ {
		out, _, err := SwapDifficulty(buildPlain(1), target, DefaultSwapOptions())
		if err != nil {
			t.Fatal(err)
		}
		if got := GetDifficulty(out); got != target {
			t.Errorf("difficulty %d, want %d", got, target)
		}
	}
}

func TestSwapRejectsBadDifficulty(t *testing.T) {
	if _, _, err := SwapDifficulty(buildPlain(1), 9, DefaultSwapOptions()); err == nil {
		t.Fatal("expected an error for difficulty 9")
	}
}

func TestLateralSwapTouchesOnlyTheFlag(t *testing.T) {
	p := buildPlain(1)
	out, _, _ := SwapDifficulty(p, 2, DefaultSwapOptions())
	for i := range p {
		if p[i] != out[i] && i != DifficultyOffset {
			t.Fatalf("unexpected change at 0x%X", i)
		}
	}
}

func TestCriticalAbilitiesGrantedAndRemoved(t *testing.T) {
	crit, _, _ := SwapDifficulty(buildPlain(1), Critical, DefaultSwapOptions())
	for _, aid := range CriticalAbilities {
		if GetAbility(crit, Sora, aid) != AbilityInnateEquipped {
			t.Errorf("ability 0x%03X was not granted", aid)
		}
	}
	back, _, _ := SwapDifficulty(crit, 1, DefaultSwapOptions())
	for _, aid := range CriticalAbilities {
		if GetAbility(back, Sora, aid) != AbilityAbsent {
			t.Errorf("ability 0x%03X was not removed", aid)
		}
	}
}

func TestUnequippedCriticalAbilityIsStillRemoved(t *testing.T) {
	p := buildPlain(3)
	SetAbility(p, Sora, CriticalAbilities[0], 0x00000449) // owned, innate, unequipped
	out, _, _ := SwapDifficulty(p, 1, DefaultSwapOptions())
	if GetAbility(out, Sora, CriticalAbilities[0]) != AbilityAbsent {
		t.Error("an unequipped Critical ability survived the downgrade")
	}
}

func TestEquipmentGrantedAbilityIsLeftAlone(t *testing.T) {
	p := buildPlain(3)
	SetAbility(p, Sora, CriticalAbilities[0], 0x00008443) // source bits set
	out, _, _ := SwapDifficulty(p, 1, DefaultSwapOptions())
	if GetAbility(out, Sora, CriticalAbilities[0]) != 0x00008443 {
		t.Error("an equipment-granted ability was clobbered")
	}
}

func TestPartyMembersNeverGetCriticalAbilities(t *testing.T) {
	out, _, _ := SwapDifficulty(buildPlain(1), Critical, DefaultSwapOptions())
	for _, ci := range []int{1, 2} {
		for _, aid := range CriticalAbilities {
			if GetAbility(out, ci, aid) != AbilityAbsent {
				t.Errorf("%s was given ability 0x%03X", CharNames[ci], aid)
			}
		}
	}
}

func TestCriticalScalesTheWholeParty(t *testing.T) {
	out, _, _ := SwapDifficulty(buildPlain(1), Critical, DefaultSwapOptions())
	for ci := range CharNames {
		if got := GetStat(out, ci, StatHP); got != 145/2 {
			t.Errorf("%s HP %d, want %d", CharNames[ci], got, 145/2)
		}
	}
}

func TestHPScalingCanBeDisabled(t *testing.T) {
	opt := DefaultSwapOptions()
	opt.ScaleHP = false
	out, _, _ := SwapDifficulty(buildPlain(1), Critical, opt)
	if GetStat(out, Sora, StatHP) != 145 {
		t.Error("HP was scaled despite ScaleHP being off")
	}
}

func TestHPNeverReachesZero(t *testing.T) {
	p := buildPlain(1)
	SetStat(p, Sora, StatHP, 1)
	out, _, _ := SwapDifficulty(p, Critical, DefaultSwapOptions())
	if got := GetStat(out, Sora, StatHP); got != 1 {
		t.Errorf("HP %d, want 1", got)
	}
}

func TestStartItemGrantAndRevoke(t *testing.T) {
	const earring = 256
	opt := DefaultSwapOptions()
	opt.GrantStartItems = true
	crit, _, _ := SwapDifficulty(buildPlain(1), Critical, opt)
	if c, _ := GetItem(crit, earring); c != 1 {
		t.Fatalf("earring count %d, want 1", c)
	}

	// Kept by default on the way back down.
	back, _, _ := SwapDifficulty(crit, 1, DefaultSwapOptions())
	if c, _ := GetItem(back, earring); c != 1 {
		t.Error("earring was removed without being asked")
	}

	opt2 := DefaultSwapOptions()
	opt2.RevokeStartItems = true
	back2, _, _ := SwapDifficulty(crit, 1, opt2)
	if c, _ := GetItem(back2, earring); c != 0 {
		t.Error("earring was not revoked on request")
	}
}

func TestEquippedEarringIsNeverRevoked(t *testing.T) {
	const earring = 256
	p := buildPlain(3)
	SetItem(p, earring, 1, ItemFresh)
	off := charOffset(Sora) + AccessorySlotOff
	p[off] = byte(ItemToAccessory[earring])
	p[off+1] = ItemTypeAccessory

	opt := DefaultSwapOptions()
	opt.RevokeStartItems = true
	out, _, _ := SwapDifficulty(p, 1, opt)
	if c, _ := GetItem(out, earring); c != 1 {
		t.Error("an equipped earring was taken")
	}
}

func TestExtraEarringsAreNeverRevoked(t *testing.T) {
	const earring = 256
	p := buildPlain(3)
	SetItem(p, earring, 3, ItemFresh)
	opt := DefaultSwapOptions()
	opt.RevokeStartItems = true
	out, _, _ := SwapDifficulty(p, 1, opt)
	if c, _ := GetItem(out, earring); c != 3 {
		t.Error("a stack of earrings was disturbed")
	}
}

func TestTablesAreLoaded(t *testing.T) {
	if Abilities[0x06A] != "Critical Converter" {
		t.Errorf("ability 0x06A = %q", Abilities[0x06A])
	}
	if Items[256] != "Soldier's Earring" {
		t.Errorf("item 256 = %q", Items[256])
	}
	if Accessories[17] != "Magic Ring" {
		t.Errorf("accessory 17 = %q", Accessories[17])
	}
	if ItemToAccessory[256] != 32 {
		t.Errorf("earring maps to accessory %d, want 32", ItemToAccessory[256])
	}
}

// --- JSON ------------------------------------------------------------------

func TestDumpIsValidJSON(t *testing.T) {
	data, err := Dump(buildPlain(3), "123", 3)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["_format"] != DocFormat {
		t.Errorf("_format = %v", doc["_format"])
	}
	hdr := doc["header"].(map[string]any)
	if hdr["difficulty"].(float64) != 3 {
		t.Errorf("difficulty = %v", hdr["difficulty"])
	}
	if _, ok := doc["characters"].(map[string]any)["Sora"]; !ok {
		t.Error("Sora missing from characters")
	}
}

func TestDumpOmitsAbsentAbilities(t *testing.T) {
	data, _ := Dump(buildPlain(1), "", 3)
	var doc map[string]any
	json.Unmarshal(data, &doc)
	abil := doc["characters"].(map[string]any)["Sora"].(map[string]any)["abilities"].(map[string]any)
	if _, ok := abil["0x003"]; !ok {
		t.Error("the innate ability the fixture sets is missing")
	}
	if _, ok := abil["0x068"]; ok {
		t.Error("an absent ability was rendered")
	}
}

func TestEmptyPatchChangesNothing(t *testing.T) {
	p := buildPlain(1)
	out, changes, err := Patch(p, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, p) || len(changes) != 0 {
		t.Error("an empty patch was not a no-op")
	}
}

func TestPatchOnlyTouchesNamedKeys(t *testing.T) {
	p := buildPlain(1)
	out, _, err := Patch(p, []byte(`{"header":{"difficulty":3}}`))
	if err != nil {
		t.Fatal(err)
	}
	for i := range p {
		if p[i] != out[i] && i != DifficultyOffset {
			t.Fatalf("unexpected change at 0x%X", i)
		}
	}
}

func TestPatchAcceptsHexStrings(t *testing.T) {
	out, _, err := Patch(buildPlain(1), []byte(`{"header":{"munny":"0xFF"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(out[0x28:]); got != 255 {
		t.Errorf("munny = %d, want 255", got)
	}
}

func TestPatchRejectsBadDocuments(t *testing.T) {
	cases := map[string]string{
		"read-only field":   `{"header":{"filesize":123}}`,
		"unknown character": `{"characters":{"Nobody":{"hp":1}}}`,
		"unknown char key":  `{"characters":{"Sora":{"nope":1}}}`,
		"bad format":        `{"_format":"something/else"}`,
		"item out of range": `{"inventory":{"99999":1}}`,
		"malformed json":    `{`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Patch(buildPlain(1), []byte(doc)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestPatchInventoryScalarAndObjectForms(t *testing.T) {
	a, _, _ := Patch(buildPlain(1), []byte(`{"inventory":{"256":2}}`))
	b, _, _ := Patch(buildPlain(1), []byte(`{"inventory":{"256":{"count":2}}}`))
	ca, _ := GetItem(a, 256)
	cb, _ := GetItem(b, 256)
	if ca != 2 || cb != 2 {
		t.Errorf("counts %d and %d, want 2 and 2", ca, cb)
	}
}

func TestPatchZeroCountClearsFlags(t *testing.T) {
	p, _, _ := Patch(buildPlain(1), []byte(`{"inventory":{"256":1}}`))
	out, _, _ := Patch(p, []byte(`{"inventory":{"256":0}}`))
	if c, f := GetItem(out, 256); c != 0 || f != 0 {
		t.Errorf("count %d flags 0x%02X, want 0 and 0", c, f)
	}
}

func TestUnknownNamesRenderAsQuestionMark(t *testing.T) {
	if ItemName(999999) != "?" || AbilityName(999999) != "?" {
		t.Error("unmapped ids should render as ?")
	}
}

// earringStates covers every inventory arrangement the start-item switches
// have to reason about.
func earringStates() []struct {
	name  string
	setup func([]byte)
} {
	const earring = 256
	return []struct {
		name  string
		setup func([]byte)
	}{
		{"none held", func(p []byte) {}},
		{"exactly the starting one, unequipped", func(p []byte) {
			SetItem(p, earring, 1, ItemFresh)
		}},
		{"the starting one, equipped by Sora", func(p []byte) {
			SetItem(p, earring, 1, ItemFresh)
			off := charOffset(Sora) + AccessorySlotOff
			p[off] = byte(ItemToAccessory[earring])
			p[off+1] = ItemTypeAccessory
		}},
		{"a stack the player collected", func(p []byte) {
			SetItem(p, earring, 3, ItemFresh)
		}},
	}
}

// StartItemState drives which switches the interface offers, so it has to
// agree with what a swap would actually do. Anything else offers a switch that
// silently does nothing, or hides one that would have worked.
func TestStartItemStateAgreesWithSwap(t *testing.T) {
	const earring = 256
	for _, c := range earringStates() {
		t.Run(c.name, func(t *testing.T) {
			// Grantable: does entering Critical actually add one?
			plain := buildPlain(1)
			c.setup(plain)
			grantable, _ := StartItemState(plain)
			before, _ := GetItem(plain, earring)
			opt := DefaultSwapOptions()
			opt.GrantStartItems = true
			after, _, err := SwapDifficulty(plain, Critical, opt)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := GetItem(after, earring)
			if changed := got != before; changed != grantable {
				t.Errorf("StartItemState says grantable=%v, but a grant %s the count (%d -> %d)",
					grantable, map[bool]string{true: "changed", false: "left"}[changed], before, got)
			}

			// Revocable: does leaving Critical actually take one back?
			plain = buildPlain(Critical)
			c.setup(plain)
			_, revocable := StartItemState(plain)
			before, _ = GetItem(plain, earring)
			opt = DefaultSwapOptions()
			opt.RevokeStartItems = true
			after, _, err = SwapDifficulty(plain, 1, opt)
			if err != nil {
				t.Fatal(err)
			}
			got, _ = GetItem(after, earring)
			if changed := got != before; changed != revocable {
				t.Errorf("StartItemState says revocable=%v, but a revoke %s the count (%d -> %d)",
					revocable, map[bool]string{true: "changed", false: "left"}[changed], before, got)
			}
		})
	}
}

// A save that is already Critical can still be missing the earring it should
// have started with, so a grant has to work without a difficulty change. The
// early return used to skip the items along with everything else.
func TestGrantWorksWithoutADifficultyChange(t *testing.T) {
	const earring = 256
	plain := buildPlain(Critical)
	opt := DefaultSwapOptions()
	opt.GrantStartItems = true

	out, changes, err := SwapDifficulty(plain, Critical, opt)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := GetItem(out, earring); c != 1 {
		t.Fatalf("earring count %d, want 1", c)
	}
	if len(changes) == 0 {
		t.Error("the change was not reported")
	}
	// The difficulty itself must be untouched, and so must everything else.
	if GetDifficulty(out) != Critical {
		t.Error("difficulty moved")
	}
}

// The same call on a save that already holds one must do nothing at all, so a
// no-op never reaches disk as a write plus a backup.
func TestSameDifficultyGrantIsANoOpWhenAlreadyHeld(t *testing.T) {
	const earring = 256
	plain := buildPlain(Critical)
	SetItem(plain, earring, 1, ItemFresh)
	opt := DefaultSwapOptions()
	opt.GrantStartItems = true

	out, changes, err := SwapDifficulty(plain, Critical, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Error("the save was modified")
	}
	for _, c := range changes {
		if strings.Contains(c, "added") {
			t.Errorf("reported %q on a save that already held one", c)
		}
	}
}

// Nothing about the start-item switches may move the difficulty on its own.
func TestSameDifficultyWithNoOptionsChangesNothing(t *testing.T) {
	plain := buildPlain(Critical)
	out, changes, err := SwapDifficulty(plain, Critical, DefaultSwapOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) || len(changes) != 0 {
		t.Errorf("a no-op swap produced %d changes", len(changes))
	}
}
