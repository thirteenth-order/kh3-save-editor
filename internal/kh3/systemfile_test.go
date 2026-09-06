package kh3_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// systemSizedSave is a plaintext the size of KHIII_system.bin, which is the
// only shape of save that is not a slot. Nothing in internal/fixture builds
// one, which is exactly why the panic below went unnoticed: the whole non-slot
// path of Patch had no test at all.
func systemSizedSave() []byte {
	p := make([]byte, 0x7B20)
	copy(p, "S@vE")
	binary.LittleEndian.PutUint32(p[0x04:], 0x7B08)
	binary.LittleEndian.PutUint16(p[0x08:], 5)
	binary.LittleEndian.PutUint16(p[0x0A:], 2)
	p[0x14] = 1
	return p
}

// Dump stops at the difficulty byte for the small system file. Patch has to
// draw the same line: a document naming a field that only a slot has used to
// panic rather than fail, because bonus_hp lives at 0xB49C and a system file
// ends long before that.
func TestPatchRefusesSlotOnlyKeysOnASystemFile(t *testing.T) {
	sys := systemSizedSave()
	if kh3.IsSlot(sys) {
		t.Fatal("the system fixture is slot-sized; this test needs the small file")
	}
	for _, doc := range []string{
		`{"header":{"bonus_hp":1}}`,
		`{"header":{"munny_earned":1}}`,
		`{"header":{"munny":1}}`,
		`{"header":{"map_path":"/Game/x"}}`,
		`{"characters":{"Sora":{"hp":1}}}`,
		`{"records":{"minigames":{"verum_rex_high_score":1}}}`,
	} {
		_, _, err := kh3.Patch(sys, []byte(doc))
		if err == nil {
			t.Errorf("%s was accepted for a system file", doc)
		}
	}
	// The difficulty byte and everything before it is genuinely there.
	if _, _, err := kh3.Patch(sys, []byte(`{"header":{"difficulty":3}}`)); err != nil {
		t.Errorf("difficulty rejected for a system file: %v", err)
	}
}

// And a system file still has to survive being dumped and patched straight
// back, which is why an empty slot-only section stays acceptable.
func TestASystemFileStillRoundTrips(t *testing.T) {
	sys := systemSizedSave()
	doc, err := kh3.Dump(sys, "", kh3.CharCount)
	if err != nil {
		t.Fatal(err)
	}
	out, changes, err := kh3.Patch(sys, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("round trip reported %v", changes)
	}
	if !bytes.Equal(out, sys) {
		t.Error("round trip changed the system file")
	}
}
