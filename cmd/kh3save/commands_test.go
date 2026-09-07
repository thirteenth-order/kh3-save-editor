// The subcommands, driven the way a person drives them: build a save tree,
// run the command against it, read what it printed and what it wrote.
//
// This package sat at 19% with seventeen of its nineteen functions at zero,
// commit -- the write path -- among them. The cause was structural: every
// command was one function that parsed flags, walked paths, loaded, printed
// and wrote, so there was no seam to hold. savePaths and eachSave are that
// seam, and this is what they were for.

package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// saveTree writes n synthetic Steam saves into a fresh directory and returns
// it. Nothing here comes from a real save: the account is fixture.Account,
// which belongs to nobody.
func saveTree(t *testing.T, n int) (dir string, paths []string) {
	t.Helper()
	dir = t.TempDir()
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		o := fixture.Default()
		o.Level = byte(6 + i)
		blob, err := kh3.Wrap(fixture.Build(o), key)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "KHIII_slot"+string(rune('0'+i))+".bin")
		if err := os.WriteFile(p, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return dir, paths
}

func TestParseDifficultyTakesEveryFormPeopleType(t *testing.T) {
	for _, c := range []struct {
		in   string
		want byte
	}{
		{"0", 0}, {"3", 3},
		{"Critical", 3}, {"critical", 3}, {"  PROUD  ", 2},
		{"Standard", 1},
		// Upstream calls level 1 "Normal" and the game calls it "Standard".
		// Difficulties is hand-written to say Standard, so the other word has
		// to be taken too or anybody coming from upstream's naming is stuck.
		{"Normal", 1}, {"normal", 1},
	} {
		got, err := parseDifficulty(c.in)
		if err != nil {
			t.Errorf("parseDifficulty(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseDifficulty(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "4", "-1", "Hard", "Very Easy", "0x1"} {
		if got, err := parseDifficulty(bad); err == nil {
			t.Errorf("parseDifficulty(%q) = %d, want an error", bad, got)
		}
	}
}

// -n is a promise that nothing was written, and it is the flag people reach
// for when they are not sure. It has to be exact: no output file, no backup,
// and the save byte-identical afterwards.
func TestDryRunWritesNothingAtAll(t *testing.T) {
	dir, paths := saveTree(t, 1)
	before, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdSwap([]string{paths[0], "-d", "Critical", "-n",
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("swap -n does not say it wrote nothing:\n%s", out)
	}
	after, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("swap -n changed the save")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(paths) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("swap -n left files behind: %v", names)
	}
}

// Editing in place backs the save up first; -o writes elsewhere and leaves the
// original alone. Both go through commit, which had no test at all.
func TestSwapInPlaceBacksUpAndOutDirDoesNot(t *testing.T) {
	dir, paths := saveTree(t, 1)
	original, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}

	elsewhere := filepath.Join(dir, "out")
	captureStdout(t, func() {
		if err := cmdSwap([]string{paths[0], "-d", "Critical", "-o", elsewhere,
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if now, _ := os.ReadFile(paths[0]); string(now) != string(original) {
		t.Error("swap -o changed the save it was reading")
	}
	moved, err := kh3.OpenFile(filepath.Join(elsewhere, "KHIII_slot0.bin"), fixture.Account)
	if err != nil {
		t.Fatalf("swap -o did not write a save that opens: %v", err)
	}
	if got := kh3.GetDifficulty(moved.Plain); got != 3 {
		t.Errorf("the save swap -o wrote is on difficulty %d, want Critical", got)
	}

	// In place: the save changes and a backup of what was there appears.
	out := captureStdout(t, func() {
		if err := cmdSwap([]string{paths[0], "-d", "Critical", "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "backup ->") {
		t.Errorf("an in-place swap did not report a backup:\n%s", out)
	}
	edited, err := kh3.OpenFile(paths[0], fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	if got := kh3.GetDifficulty(edited.Plain); got != 3 {
		t.Errorf("the save is on difficulty %d after an in-place swap, want Critical", got)
	}
	var backups []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, found %v", backups)
	}
	saved, err := os.ReadFile(filepath.Join(dir, backups[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != string(original) {
		t.Error("the backup is not the save that was replaced")
	}
}

// Every path this program prints goes through show, because on Steam the save
// lives under a directory named after the account id and the key derives from
// that id alone. A command that prints an unmasked path publishes it, which is
// exactly what the -with-account flag exists to gate.
func TestCommandOutputMasksTheAccountInThePath(t *testing.T) {
	dir := t.TempDir()
	// The real Steam layout, so the id is a directory name on the way to the
	// save rather than something a command could avoid printing by accident.
	deep := filepath.Join(dir, "Steam", fixture.Account, "SaveGames", "kh3sv2", "data")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := kh3.Wrap(fixture.Build(fixture.Default()), key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(deep, "KHIII_slot0.bin")
	if err := os.WriteFile(p, blob, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"info", func() error { return cmdInfo([]string{p}) }},
		{"info -l", func() error { return cmdInfo([]string{p, "-l"}) }},
		{"verify", func() error { return cmdVerify([]string{p}) }},
		{"abilities", func() error { return cmdAbilities([]string{p}) }},
		{"swap -n", func() error { return cmdSwap([]string{p, "-d", "Critical", "-n"}) }},
	} {
		out := captureStdout(t, func() {
			if err := c.run(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		})
		if strings.Contains(out, fixture.Account) {
			t.Errorf("%s printed the account id in full", c.name)
		}
		if !strings.Contains(out, kh3.MaskPath(p)) {
			t.Errorf("%s did not print the masked path", c.name)
		}
	}

	// And the id is still the real one everywhere it is used: masking it in
	// place would derive a key from 765611*******0000 and nothing would open.
	out := captureStdout(t, func() {
		if err := cmdInfo([]string{p, "-with-account"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "account      "+fixture.Account) {
		t.Errorf("-with-account did not report the account id:\n%s", out)
	}
}

// verify keeps its own loop rather than using eachSave, because reporting
// which saves are broken is the job: stopping at the first one would answer a
// different question.
func TestVerifyReportsEveryFileAndFailsOnABadOne(t *testing.T) {
	dir, paths := saveTree(t, 2)
	broken := filepath.Join(dir, "KHIII_slot9.bin")
	if err := os.WriteFile(broken, []byte("not a save at all, but long enough"), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	out := captureStdout(t, func() {
		err = cmdVerify([]string{dir, "-account", fixture.Account})
	})
	if err == nil {
		t.Error("verify passed a directory holding a file that does not open")
	}
	if strings.Count(out, "OK    ") != len(paths) {
		t.Errorf("verify did not pass both good saves:\n%s", out)
	}
	if !strings.Contains(out, "FAIL  ") {
		t.Errorf("verify did not report the broken save:\n%s", out)
	}
	// The good save after the bad one still got checked, which is the point of
	// verify not stopping.
	if !strings.Contains(out, kh3.MaskPath(paths[1])) {
		t.Errorf("verify stopped before the last save:\n%s", out)
	}
}

// A save with no Steam wrapper carries no account id, so -account means
// nothing for one and saying so beats deriving a key nothing will use.
func TestAccountOnAPlainSaveIsRefusedWithSomethingToDo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "KHIII_slot0.bin")
	if err := os.WriteFile(p, fixture.Build(fixture.Default()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := load(p, fixture.Account)
	if err == nil {
		t.Fatal("-account was accepted for a save that has no account")
	}
	if !strings.Contains(err.Error(), "convert -to pc") {
		t.Errorf("the refusal does not say what to do instead: %v", err)
	}
	// Without it the same file opens fine, and reports no account.
	l, err := load(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Account != "" || l.Format != kh3.FormatPlain {
		t.Errorf("a plain save opened as %s with account %q", l.Format, l.Account)
	}
}

func TestExpandFindsSavesAndSaysSoWhenItFindsNone(t *testing.T) {
	dir, paths := saveTree(t, 3)
	// A directory yields its saves, sorted, and ignores anything else in it.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := expand([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(paths) {
		t.Fatalf("expand found %v, want the %d saves", got, len(paths))
	}
	for i := range got {
		if got[i] != paths[i] {
			t.Errorf("expand[%d] = %s, want %s (sorted)", i, got[i], paths[i])
		}
	}
	// A named file is taken as given, whatever it is called.
	if got, err := expand([]string{paths[1]}); err != nil || len(got) != 1 {
		t.Errorf("expand of one file = %v, %v", got, err)
	}
	// Nothing to work on is an error rather than a silent success, which is
	// what makes "kh3save info ~/Documents" say something useful.
	if _, err := expand([]string{t.TempDir()}); err == nil {
		t.Error("expand accepted a directory with no saves")
	}
	if !strings.Contains(mustErr(t, func() error {
		_, err := expand([]string{t.TempDir()})
		return err
	}), "KHIII_") {
		t.Error("the error does not say what it was looking for")
	}
}

// -ability is repeatable and takes hex, which is how the ability tables and
// this program's own output write ids.
func TestAbilityIDsAccumulateAndTakeHex(t *testing.T) {
	var ids intList
	for _, v := range []string{"0x068", "105", "0x06A"} {
		if err := ids.Set(v); err != nil {
			t.Fatalf("Set(%q): %v", v, err)
		}
	}
	want := []int{0x68, 105, 0x6A}
	if len(ids) != len(want) {
		t.Fatalf("collected %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids[%d] = %d, want %d", i, ids[i], want[i])
		}
	}
	if err := ids.Set("purple"); err == nil {
		t.Error("Set accepted a value that is not a number")
	}
}

func TestDiffRunsFindEveryStretchAndOnlyThat(t *testing.T) {
	for _, c := range []struct {
		name string
		a, b []byte
		want []run
	}{
		{"identical", []byte{1, 2, 3}, []byte{1, 2, 3}, nil},
		{"one byte", []byte{1, 2, 3}, []byte{1, 9, 3}, []run{{1, 2}}},
		{"a run", []byte{1, 2, 3, 4}, []byte{1, 9, 9, 4}, []run{{1, 3}}},
		{"two runs", []byte{1, 2, 3, 4, 5}, []byte{9, 2, 9, 9, 5}, []run{{0, 1}, {2, 4}}},
		{"at the very end", []byte{1, 2}, []byte{1, 9}, []run{{1, 2}}},
		// Comparing stops at the shorter one: the tail of a longer file is not
		// a difference, it is an absence, and the length note says so instead.
		{"different lengths", []byte{1, 2, 3, 4}, []byte{1, 2}, nil},
	} {
		got := diffRuns(c.a, c.b)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: run %d is %v, want %v", c.name, i, got[i], c.want[i])
			}
		}
	}

	// The little-endian reading is what makes a four-byte run readable as the
	// number it is, which is how the munny ledger was found.
	if got := leNumber([]byte{0x99, 0x04, 0x00, 0x00}); got != 1177 {
		t.Errorf("leNumber = %d, want 1177", got)
	}
}

// A real save pair: swap writes a known set of bytes, and diff has to find
// exactly those and no others. This is the check that the two halves of this
// program agree about what changed.
func TestDiffFindsWhatSwapWrote(t *testing.T) {
	_, paths := saveTree(t, 1)
	l, err := kh3.OpenFile(paths[0], fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	swapped, changes, err := kh3.SwapDifficulty(l.Plain, 3, kh3.SwapOptions{ScaleHP: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("the swap reported no changes, so there is nothing to find")
	}
	runs := diffRuns(l.Plain, swapped)
	if len(runs) == 0 {
		t.Fatal("diff found nothing between a save and its swapped self")
	}
	// The difficulty byte is the one change every swap makes, so it must be in
	// there, as a run of its own rather than swallowed by a neighbor.
	found := false
	for _, r := range runs {
		if r.start == kh3.DifficultyOffset && r.len() == 1 {
			found = true
		}
		if r.start < 0x10 {
			t.Errorf("swap wrote inside the header at 0x%X; only the CRC lives there "+
				"and Seal rebuilds that", r.start)
		}
	}
	if !found {
		t.Errorf("diff did not find the difficulty byte at 0x%X: %v",
			kh3.DifficultyOffset, runs)
	}
}

// mustErr runs fn, requires it to fail, and returns the message.
func mustErr(t *testing.T, fn func() error) string {
	t.Helper()
	err := fn()
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	return err.Error()
}

// decrypt writes the plaintext and encrypt puts it back, so the pair has to be
// a round trip: the save that comes out the far end must be the one that went
// in. This is the route somebody takes to inspect a save by hand.
func TestDecryptAndEncryptRoundTrip(t *testing.T) {
	dir, paths := saveTree(t, 1)
	original, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	plainDir := filepath.Join(dir, "plain")
	captureStdout(t, func() {
		if err := cmdDecrypt([]string{paths[0], "-o", plainDir, "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	plain, err := os.ReadFile(filepath.Join(plainDir, "KHIII_slot0.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain[:4]) != string(kh3.Magic) {
		t.Fatal("decrypt did not write a plaintext save")
	}

	backDir := filepath.Join(dir, "back")
	captureStdout(t, func() {
		if err := cmdEncrypt([]string{filepath.Join(plainDir, "KHIII_slot0.bin"),
			"-o", backDir, "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	back, err := os.ReadFile(filepath.Join(backDir, "KHIII_slot0.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(original) {
		t.Error("decrypt then encrypt did not give back the save it started with")
	}

	// encrypt has no save to read an id from, so it has to be told one.
	if err := cmdEncrypt([]string{filepath.Join(plainDir, "KHIII_slot0.bin"), "-o", backDir}); err == nil {
		t.Error("encrypt ran with no account; plaintext carries no id to find")
	}
}

// rekey moves a save to another account, which means the destination opens it
// and the source no longer can. Getting that backwards would hand somebody a
// save neither account can load.
func TestRekeyMakesTheSaveOpenUnderTheNewAccount(t *testing.T) {
	const other = "76561190000000001"
	dir, paths := saveTree(t, 1)
	out := filepath.Join(dir, "rekeyed")
	captureStdout(t, func() {
		if err := cmdRekey([]string{paths[0], "-to", other, "-o", out,
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	moved := filepath.Join(out, "KHIII_slot0.bin")
	if _, err := kh3.OpenFile(moved, other); err != nil {
		t.Errorf("the rekeyed save does not open under the new account: %v", err)
	}
	if _, err := kh3.OpenFile(moved, fixture.Account); err == nil {
		t.Error("the rekeyed save still opens under the old account")
	}
	// The original is untouched, because rekey writes to a directory.
	if _, err := kh3.OpenFile(paths[0], fixture.Account); err != nil {
		t.Errorf("rekey damaged the save it read: %v", err)
	}
	if err := cmdRekey([]string{paths[0], "-o", out}); err == nil {
		t.Error("rekey ran with no destination account")
	}
}

// grant-abilities writes ability words, and the interesting cases are the ones
// it declines: an ability already owned, and one an equipped item is granting,
// which it must not take away by writing over the equipment's bits.
func TestGrantAbilitiesGrantsRevokesAndDeclines(t *testing.T) {
	_, paths := saveTree(t, 1)
	out := captureStdout(t, func() {
		if err := cmdGrantAbilities([]string{paths[0], "-critical",
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "granted") {
		t.Fatalf("nothing was granted:\n%s", out)
	}
	l, err := kh3.OpenFile(paths[0], fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	for _, aid := range kh3.CriticalAbilities {
		if w := kh3.GetAbility(l.Plain, 0, aid); w == kh3.AbilityAbsent {
			t.Errorf("ability 0x%03X was not granted", aid)
		}
	}

	// Running it again declines rather than rewriting, and writes nothing.
	before, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := cmdGrantAbilities([]string{paths[0], "-critical",
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "already owned") {
		t.Errorf("granting twice did not say the abilities were already there:\n%s", out)
	}
	if after, _ := os.ReadFile(paths[0]); string(after) != string(before) {
		t.Error("granting an ability that was already owned rewrote the save")
	}

	// And back off again.
	captureStdout(t, func() {
		if err := cmdGrantAbilities([]string{paths[0], "-critical", "-revoke",
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	l, err = kh3.OpenFile(paths[0], fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	for _, aid := range kh3.CriticalAbilities {
		if w := kh3.GetAbility(l.Plain, 0, aid); w != kh3.AbilityAbsent {
			t.Errorf("ability 0x%03X survived a revoke as 0x%08X", aid, w)
		}
	}

	if err := cmdGrantAbilities([]string{paths[0]}); err == nil {
		t.Error("grant-abilities ran with nothing to grant")
	}
	if err := cmdGrantAbilities([]string{paths[0], "-critical", "-character", "Xemnas"}); err == nil {
		t.Error("grant-abilities accepted a character that is not in the game")
	}
}

// dump writes the document patch reads, so the pair round-trips: a whole dump
// applied back to the save it came from changes nothing and says so. The CLI
// half of TestDumpPatchRoundTripIsByteIdentical.
func TestDumpThenPatchChangesNothing(t *testing.T) {
	dir, paths := saveTree(t, 1)
	doc := filepath.Join(dir, "slot0.json")
	captureStdout(t, func() {
		if err := cmdDump([]string{paths[0], "-o", doc, "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	before, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdPatch([]string{paths[0], doc, "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "no changes") {
		t.Errorf("patching a save with its own dump reported changes:\n%s", out)
	}
	if after, _ := os.ReadFile(paths[0]); string(after) != string(before) {
		t.Error("patching a save with its own dump rewrote it")
	}

	// A dump never carries the account id unless it is asked for, because it
	// is the thing people paste into a bug report.
	data, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), fixture.Account) {
		t.Error("the dump carries the account id without -with-account")
	}

	if err := cmdPatch([]string{doc}); err == nil {
		t.Error("patch ran with no save to apply the document to")
	}
}

// patch is the editing surface, so an edit through it has to land, and one the
// format cannot hold has to be refused with the save untouched.
func TestPatchAppliesAnEditAndRefusesAnImpossibleOne(t *testing.T) {
	dir, paths := saveTree(t, 1)
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"header":{"level":42}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdPatch([]string{paths[0], good, "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "header.level") {
		t.Errorf("patch did not report the field it changed:\n%s", out)
	}
	l, err := kh3.OpenFile(paths[0], fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	if got := kh3.ReadHeader(l.Plain).Level; got != 42 {
		t.Errorf("level is %d after the patch, want 42", got)
	}

	before, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"header":{"level":900}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := cmdPatch([]string{paths[0], bad, "-account", fixture.Account}); err == nil {
			t.Error("patch accepted a level the byte cannot hold")
		}
	})
	if after, _ := os.ReadFile(paths[0]); string(after) != string(before) {
		t.Error("a refused patch still changed the save")
	}
}

// info -l reads the regions that are too bulky for the default output. The
// short fixture stops before the record block, and asking for a best on one is
// an error rather than a read past the end of the buffer -- so info -l has to
// print what is there and stay quiet about what is not.
func TestInfoLongPrintsWhatTheSaveHoldsAndNoMore(t *testing.T) {
	dir := t.TempDir()
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		plain []byte
		full  bool
	}{
		{"KHIII_slot0.bin", fixture.Build(fixture.Default()), false},
		{"KHIII_slot1.bin", fixture.BuildFull(fixture.Default()), true},
	} {
		blob, err := kh3.Wrap(kh3.PadToBlock(c.plain), key)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, c.name)
		if err := os.WriteFile(p, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if err := cmdInfo([]string{p, "-l", "-account", fixture.Account}); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		})
		for _, want := range []string{"magic ", "links ", "map "} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: info -l did not print %q:\n%s", c.name, want, out)
			}
		}
		// The record block only exists in the full save, and a save that stops
		// before it says nothing rather than a screen of zeros.
		hasAlbum := strings.Contains(out, "album ")
		if hasAlbum != c.full {
			t.Errorf("%s: album line present = %v, want %v", c.name, hasAlbum, c.full)
		}
	}
}

// lookupName and commandList are what keep an unnamed id readable. An id no
// table covers has to print as the id rather than as an empty string, or the
// output quietly loses a value that is really there.
func TestUnnamedIDsStillPrintAsSomething(t *testing.T) {
	if got := lookupName(map[int]string{1: "Blizzard Charge"}, 1); got != "Blizzard Charge" {
		t.Errorf("lookupName of a known id = %q", got)
	}
	if got := lookupName(map[int]string{}, 737); got != "#737" {
		t.Errorf("lookupName of an unknown id = %q, want #737", got)
	}
	// An empty array reads as "(none)" rather than a blank line, so the reader
	// can tell "nothing here" from "this command printed nothing".
	empty := func(p []byte, i int) int { return 0 }
	if got := commandList(nil, empty, 6); got != "(none)" {
		t.Errorf("commandList of an empty array = %q, want (none)", got)
	}
	one := func(p []byte, i int) int {
		if i == 0 {
			return 29
		}
		return 0
	}
	if got := commandList(nil, one, 6); got != kh3.CommandName(29) {
		t.Errorf("commandList = %q, want %q", got, kh3.CommandName(29))
	}
}

// The system file has no playthrough in it, so a command that edits one has
// nothing to do and should say which file it passed over rather than failing
// the whole run or writing nothing in silence.
func TestSystemFilesAreSkippedByName(t *testing.T) {
	dir := t.TempDir()
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	// The shape of KHIII_system.bin: the only save that is not a slot. Built
	// the way internal/kh3's own tests build one, because nothing in
	// internal/fixture makes a non-slot save.
	system := make([]byte, 0x7B20)
	copy(system, kh3.Magic)
	binary.LittleEndian.PutUint32(system[0x04:], 0x7B08)
	binary.LittleEndian.PutUint16(system[0x08:], 5)
	binary.LittleEndian.PutUint16(system[0x0A:], 2)
	system[kh3.DifficultyOffset] = 1
	blob, err := kh3.Wrap(kh3.PadToBlock(system), key)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "KHIII_system.bin")
	if err := os.WriteFile(p, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := kh3.OpenFile(p, fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	if kh3.IsSlot(l.Plain) {
		t.Fatal("the truncated save still reads as a slot, so this test proves nothing")
	}

	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"swap", func() error {
			return cmdSwap([]string{p, "-d", "Critical", "-account", fixture.Account})
		}},
		{"grant-abilities", func() error {
			return cmdGrantAbilities([]string{p, "-critical", "-account", fixture.Account})
		}},
	} {
		out := captureStdout(t, func() {
			if err := c.run(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		})
		if !strings.Contains(out, "skip") || !strings.Contains(out, "system file") {
			t.Errorf("%s did not say it passed over the system file:\n%s", c.name, out)
		}
		if after, _ := os.ReadFile(p); string(after) != string(before) {
			t.Errorf("%s wrote to the system file", c.name)
		}
	}
}

// diff is how every offset in this program was found, so its output has to
// name the offset it found, in hex, and read a short run as the number it is.
func TestDiffPrintsOffsetsAndFiltersLongRuns(t *testing.T) {
	dir, paths := saveTree(t, 1)
	l, err := kh3.OpenFile(paths[0], fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	edited := append([]byte(nil), l.Plain...)
	edited[kh3.DifficultyOffset] = 3
	other := filepath.Join(dir, "KHIII_slot1.bin")
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := kh3.Wrap(edited, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, blob, 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := cmdDiff([]string{paths[0], other, "-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "0x00000014") {
		t.Errorf("diff did not name the difficulty offset:\n%s", out)
	}
	if !strings.Contains(out, "1 -> 3") {
		t.Errorf("diff did not read the one-byte run as a number:\n%s", out)
	}
	if !strings.Contains(out, "differing runs") {
		t.Errorf("diff printed no summary:\n%s", out)
	}
	// -skip-known hides the header, where only the CRC moves, and says how
	// many it filtered rather than dropping them silently.
	out = captureStdout(t, func() {
		if err := cmdDiff([]string{paths[0], other, "-skip-known",
			"-account", fixture.Account}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "filtered") {
		t.Errorf("-skip-known hid runs without saying so:\n%s", out)
	}

	if err := cmdDiff([]string{paths[0]}); err == nil {
		t.Error("diff ran on one file")
	}
}

// schema -tables lists the enum tables. The list is sorted so that two runs
// print the same thing: it comes out of a map, whose order is deliberately
// random in Go, and a listing that reshuffles itself is not a listing.
func TestSchemaTablesAreListedInAStableOrder(t *testing.T) {
	var runs []string
	for i := 0; i < 3; i++ {
		runs = append(runs, captureStdout(t, func() {
			if err := cmdSchema([]string{"-tables"}); err != nil {
				t.Fatal(err)
			}
		}))
	}
	for i := 1; i < len(runs); i++ {
		if runs[i] != runs[0] {
			t.Fatalf("schema -tables printed a different order on run %d:\n%s\nvs\n%s",
				i, runs[0], runs[i])
		}
	}
	lines := strings.Split(strings.TrimSpace(runs[0]), "\n")
	if len(lines) < 2 {
		t.Fatalf("schema -tables listed nothing:\n%s", runs[0])
	}
	var names []string
	for _, l := range lines {
		names = append(names, strings.Fields(l)[0])
	}
	if !slices.IsSorted(names) {
		t.Errorf("the table names are not sorted: %v", names)
	}
	for name := range kh3.Describe().Tables {
		if !slices.Contains(names, name) {
			t.Errorf("schema -tables does not list %q", name)
		}
	}
}
