package kh3_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// agreeingMirrors builds a save whose two duplicated fields hold the same
// value as their originals, which is what a real save looks like and what the
// plain fixture does not: it sets enemies_defeated and leaves the copy at zero.
func agreeingMirrors(t *testing.T) []byte {
	t.Helper()
	p := fixture.Build(fixture.Default())
	binary.LittleEndian.PutUint32(p[kh3.EnemiesDefeatedMirrorOff:],
		binary.LittleEndian.Uint32(p[0x70:]))
	binary.LittleEndian.PutUint32(p[kh3.SaveIconMirrorOff:], uint32(p[0x60]))
	return p
}

func mirrorOf(p []byte, off int) uint32 { return binary.LittleEndian.Uint32(p[off:]) }

// A field with a second copy carries into it, so a document cannot move one
// and leave the other behind.
func TestPatchCarriesAFieldIntoItsSecondCopy(t *testing.T) {
	for _, c := range []struct {
		name, doc string
		mirror    int
		want      uint32
	}{
		{"enemies_defeated", `{"header":{"enemies_defeated":4242}}`,
			kh3.EnemiesDefeatedMirrorOff, 4242},
		{"save_icon", `{"header":{"save_icon":9}}`, kh3.SaveIconMirrorOff, 9},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, changes, err := kh3.Patch(agreeingMirrors(t), []byte(c.doc))
			if err != nil {
				t.Fatal(err)
			}
			if got := mirrorOf(out, c.mirror); got != c.want {
				t.Errorf("second copy is %d, want %d (changes: %v)", got, c.want, changes)
			}
			if !strings.Contains(strings.Join(changes, "\n"), "kept consistent") {
				t.Errorf("the correction is not reported: %v", changes)
			}
		})
	}
}

// The narrow half, and the reason it is narrow: 0x504 might be a per-world
// count rather than a mirror, and the five samples cannot tell. A save where
// the two already disagree is left exactly as it is. The plain fixture is that
// save, and the golden vectors were frozen on it.
func TestPatchLeavesASecondCopyThatNeverAgreedAlone(t *testing.T) {
	p := fixture.Build(fixture.Default())
	if mirrorOf(p, kh3.EnemiesDefeatedMirrorOff) == binary.LittleEndian.Uint32(p[0x70:]) {
		t.Skip("the fixture's copies now agree; this test needs one where they do not")
	}
	out, changes, err := kh3.Patch(p, []byte(`{"header":{"enemies_defeated":4242}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := mirrorOf(out, kh3.EnemiesDefeatedMirrorOff); got != 0 {
		t.Errorf("second copy moved to %d, want it left at 0", got)
	}
	if len(changes) != 1 {
		t.Errorf("changes = %v, want only the enemies_defeated write", changes)
	}
}

// And a whole dump patched straight back stays byte-identical either way.
func TestTheSecondCopiesSurviveADumpAndPatchBack(t *testing.T) {
	for _, p := range [][]byte{agreeingMirrors(t), fixture.Build(fixture.Default())} {
		doc, err := kh3.Dump(p, "", kh3.CharCount)
		if err != nil {
			t.Fatal(err)
		}
		out, changes, err := kh3.Patch(p, doc)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 0 {
			t.Errorf("round trip reported %v", changes)
		}
		if !bytes.Equal(out, p) {
			t.Error("round trip changed the save")
		}
	}
}
