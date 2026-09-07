package kh3_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// wrapped writes a synthetic Steam save to dir and returns its path.
func wrapped(t *testing.T, dir, name string, o fixture.Options) string {
	t.Helper()
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := kh3.Wrap(fixture.Build(o), key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// The rule the whole program rests on: every write re-reads its own output the
// way the game will, and refuses to hand back anything that does not survive
// the trip. It was written out twice, once per front end, and neither copy was
// ever executed by a test -- the CLI's commit sat at 0% and the GUI reached
// its own only through a handler. There is one copy now, and this runs it.
//
// For a well-formed plaintext the read-back is a backstop rather than a filter:
// Seal rebuilds both integrity fields, so the trip holds unless Seal or Open
// has a bug, which is exactly what it is there to catch. What is reachable from
// here is the other half of the promise -- that a plaintext which would not
// produce a loadable save yields an error and no bytes, whichever of the two
// checks notices.
func TestSealCheckedReturnsOnlyASaveThatReadsBack(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		form string
		open func() *kh3.Save
	}{
		{"steam", func() *kh3.Save {
			s, err := kh3.OpenFile(wrapped(t, dir, "KHIII_slot0.bin", fixture.Default()), fixture.Account)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}},
		{"plain", func() *kh3.Save {
			p := filepath.Join(dir, "plain.bin")
			if err := os.WriteFile(p, fixture.Build(fixture.Default()), 0o644); err != nil {
				t.Fatal(err)
			}
			s, err := kh3.OpenFile(p, "")
			if err != nil {
				t.Fatal(err)
			}
			return s
		}},
	} {
		s := c.open()
		edited := append([]byte(nil), s.Plain...)
		edited[kh3.DifficultyOffset] = 3
		blob, err := s.SealChecked(edited)
		if err != nil {
			t.Fatalf("%s: an edited save does not seal: %v", c.form, err)
		}
		// The promise is not "it sealed" but "what it returns opens", so open
		// it here the way the game would rather than trusting the return.
		back, form, err := kh3.Open(blob, s.Key)
		if err != nil {
			t.Fatalf("%s: SealChecked returned a save that does not open: %v", c.form, err)
		}
		if form != s.Format {
			t.Errorf("%s: sealed into %s", c.form, form)
		}
		// Seal rewrites the CRC at 0x0C, so compare around it, which is the
		// comparison SealChecked itself makes.
		if string(back[:0x0C]) != string(edited[:0x0C]) || string(back[0x10:]) != string(edited[0x10:]) {
			t.Errorf("%s: what came back is not what was written", c.form)
		}

		// A plaintext the game would refuse gets no bytes at all.
		bad := []struct {
			why   string
			build func() []byte
		}{
			{"the magic is gone", func() []byte {
				b := append([]byte(nil), edited...)
				copy(b[:4], "junk")
				return b
			}},
		}
		if s.Format.NeedsKey() {
			// Only the encrypted form cares: AES works a block at a time and
			// would drop a partial one. A save that never went through AES
			// need not be aligned at all, which is why PadToBlock exists and
			// why this is not a case for the plain form.
			bad = append(bad, struct {
				why   string
				build func() []byte
			}{"it is not block aligned",
				func() []byte { return append(append([]byte(nil), edited...), 0) }})
		}
		for _, b := range bad {
			out, err := s.SealChecked(b.build())
			if err == nil {
				t.Errorf("%s: SealChecked accepted a plaintext where %s", c.form, b.why)
				continue
			}
			if out != nil {
				t.Errorf("%s: SealChecked returned bytes alongside an error", c.form)
			}
		}
	}
}

// Writing over the save backs it up first; writing somewhere else does not,
// because nothing is being replaced. Both front ends depend on that split:
// the CLI's -o and the interface's in-place edit are the same call.
func TestCommitBacksUpOnlyWhatItReplaces(t *testing.T) {
	dir := t.TempDir()
	p := wrapped(t, dir, "KHIII_slot0.bin", fixture.Default())
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := kh3.OpenFile(p, fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	edited := append([]byte(nil), s.Plain...)
	edited[kh3.DifficultyOffset] = 3

	// Somewhere else: no backup, and the original is untouched.
	elsewhere := filepath.Join(dir, "out", "KHIII_slot0.bin")
	if err := os.MkdirAll(filepath.Dir(elsewhere), 0o755); err != nil {
		t.Fatal(err)
	}
	bak, err := s.Commit(edited, elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if bak != "" {
		t.Errorf("writing to a new path made a backup at %s; there was nothing to replace", bak)
	}
	if now, _ := os.ReadFile(p); string(now) != string(before) {
		t.Error("writing elsewhere changed the file it was read from")
	}

	// In place: a backup, and it holds what was there before.
	bak, err = s.Commit(edited, p)
	if err != nil {
		t.Fatal(err)
	}
	if bak == "" {
		t.Fatal("writing over the save made no backup")
	}
	saved, err := os.ReadFile(bak)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != string(before) {
		t.Error("the backup is not the save that was replaced")
	}
	// And the new file is the edit, read back the way the game reads it.
	after, err := kh3.OpenFile(p, fixture.Account)
	if err != nil {
		t.Fatalf("the committed save does not open: %v", err)
	}
	if got := kh3.GetDifficulty(after.Plain); got != 3 {
		t.Errorf("the committed save is on difficulty %d, want 3", got)
	}
}

// A save with no Steam wrapper carries no account id, so none of the account
// plumbing may run for one. Reaching for Unwrap/Wrap instead of Open/Seal is
// what reintroduces the Steam-only assumption, and it is what made the
// interface report "could not determine the account id" for files it could
// read fine.
func TestOpenFileAndCommitNeedNoAccountForAPlainSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "KHIII_slot0.bin")
	if err := os.WriteFile(p, fixture.Build(fixture.Default()), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := kh3.OpenFile(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Format != kh3.FormatPlain {
		t.Fatalf("read back as %s, want plain", s.Format)
	}
	if s.Account != "" || s.Key != nil {
		t.Errorf("a plain save produced an account %q and a key", s.Account)
	}
	edited := append([]byte(nil), s.Plain...)
	edited[kh3.DifficultyOffset] = 3
	if _, err := s.Commit(edited, p); err != nil {
		t.Fatal(err)
	}
	after, err := kh3.OpenFile(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if kh3.GetDifficulty(after.Plain) != 3 || after.Format != kh3.FormatPlain {
		t.Error("the plain save did not survive a commit as a plain save")
	}
}
