package kh3

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeZip builds an archive whose members are exactly the given name/body
// pairs, in the given order.
func writeZip(t *testing.T, path string, members [][2]string) {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, m := range members {
		f, err := w.Create(m[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(m[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The layout inside a real backup, which is what the account id is read from.
const memberDir = "KINGDOM HEARTS III/Steam/76561190000000000/SaveGames/kh3sv2/data/"

func fixtureZip(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "backup.zip")
	writeZip(t, p, [][2]string{
		{"KINGDOM HEARTS III/", ""},
		{memberDir + "KHIII_slot0.bin", "slot zero"},
		{memberDir + "KHIII_slot1.bin", "slot one"},
		{memberDir + "KHIII_system.bin", "system"},
		{"KINGDOM HEARTS III/Steam/76561190000000000/Config/GameSettings.dat", "settings"},
	})
	return p
}

func TestSplitArchive(t *testing.T) {
	cases := []struct {
		in      string
		archive string
		member  string
		ok      bool
	}{
		{"/a/b.zip!m/x.bin", "/a/b.zip", "m/x.bin", true},
		{"/a/B.ZIP!m/x.bin", "/a/B.ZIP", "m/x.bin", true},
		// A "!" in a directory name is not an archive boundary.
		{"/wow!/save/KHIII_slot0.bin", "/wow!/save/KHIII_slot0.bin", "", false},
		// The boundary is the "!" whose left side is the archive.
		{"/wow!dir/b.zip!m.bin", "/wow!dir/b.zip", "m.bin", true},
		// A member whose own name contains "!" stays intact.
		{"/a/b.zip!m!n/x.bin", "/a/b.zip", "m!n/x.bin", true},
		{"/plain/KHIII_slot0.bin", "/plain/KHIII_slot0.bin", "", false},
	}
	for _, c := range cases {
		archive, member, ok := SplitArchive(c.in)
		if archive != c.archive || member != c.member || ok != c.ok {
			t.Errorf("SplitArchive(%q) = %q, %q, %v; want %q, %q, %v",
				c.in, archive, member, ok, c.archive, c.member, c.ok)
		}
	}
}

func TestListArchiveSavesFindsOnlySaves(t *testing.T) {
	got, err := ListArchiveSaves(fixtureZip(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("found %d saves, want 3 (the directory entry and the config file are not saves): %q", len(got), got)
	}
	for _, p := range got {
		if Base(p) == "GameSettings.dat" {
			t.Errorf("%s is not a save", p)
		}
	}
}

func TestReadFileReachesIntoAnArchive(t *testing.T) {
	zipPath := fixtureZip(t)
	data, err := ReadFile(JoinArchive(zipPath, memberDir+"KHIII_slot1.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "slot one" {
		t.Errorf("read %q, want %q", data, "slot one")
	}
	if _, err := ReadFile(JoinArchive(zipPath, memberDir+"KHIII_slot9.bin")); err == nil {
		t.Error("expected an error for a member that is not there")
	}
}

// The whole point of rewriting rather than repacking: one member changes and
// nothing else in the archive moves.
func TestWriteFileReplacesOneMemberAndLeavesTheRest(t *testing.T) {
	zipPath := fixtureZip(t)
	before, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range before.File {
		names = append(names, f.Name)
	}
	before.Close()

	target := JoinArchive(zipPath, memberDir+"KHIII_slot0.bin")
	if err := WriteFile(target, []byte("rewritten"), 0o644); err != nil {
		t.Fatal(err)
	}

	after, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("the archive is no longer readable: %v", err)
	}
	defer after.Close()
	if len(after.File) != len(names) {
		t.Fatalf("archive has %d members, want %d", len(after.File), len(names))
	}
	for i, f := range after.File {
		if f.Name != names[i] {
			t.Errorf("member %d is %q, want %q (order must be preserved)", i, f.Name, names[i])
		}
	}
	got, err := ReadFile(target)
	if err != nil || string(got) != "rewritten" {
		t.Errorf("read back %q, %v; want %q", got, err, "rewritten")
	}
	// Every other member must be untouched.
	other, err := ReadFile(JoinArchive(zipPath, memberDir+"KHIII_slot1.bin"))
	if err != nil || string(other) != "slot one" {
		t.Errorf("neighbouring member became %q, %v; want %q", other, err, "slot one")
	}
}

func TestWriteFileRefusesAMemberThatIsNotThere(t *testing.T) {
	zipPath := fixtureZip(t)
	stat, _ := os.Stat(zipPath)
	if err := WriteFile(JoinArchive(zipPath, "nope.bin"), []byte("x"), 0o644); err == nil {
		t.Fatal("expected an error writing a member that does not exist")
	}
	// A refused write must not have disturbed the archive.
	if now, _ := os.Stat(zipPath); now.Size() != stat.Size() {
		t.Error("the archive was modified by a write that should have failed")
	}
	if _, err := zip.OpenReader(zipPath); err != nil {
		t.Errorf("the archive is no longer readable: %v", err)
	}
}

// A failed rewrite must not leave the temporary archive lying beside the real
// one, where the next directory listing would show it as a stray file.
func TestFailedWriteLeavesNoTemporaryFile(t *testing.T) {
	zipPath := fixtureZip(t)
	dir := filepath.Dir(zipPath)
	before, _ := os.ReadDir(dir)
	_ = WriteFile(JoinArchive(zipPath, "nope.bin"), []byte("x"), 0o644)
	after, _ := os.ReadDir(dir)
	if len(after) != len(before) {
		var names []string
		for _, e := range after {
			names = append(names, e.Name())
		}
		t.Errorf("directory now holds %d entries, want %d: %q", len(after), len(before), names)
	}
}

// Backing up a member has to copy the archive, because replacing a member
// rewrites the archive. Backing up the member alone would leave nothing to
// restore from.
func TestBackupOfAMemberCopiesTheWholeArchive(t *testing.T) {
	zipPath := fixtureZip(t)
	original, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	bak, err := BackupOf(JoinArchive(zipPath, memberDir+"KHIII_slot0.bin"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(bak) != filepath.Dir(zipPath) {
		t.Errorf("backup landed in %s, want it beside the archive", filepath.Dir(bak))
	}
	// A backup nothing can open is not much of a backup: the extension has to
	// survive the timestamp, or IsArchive and every file manager reject it.
	if filepath.Ext(bak) != ".zip" {
		t.Errorf("backup is named %q, which is not openable as an archive", filepath.Base(bak))
	}
	if !IsArchive(bak) {
		t.Errorf("%q is not recognised as an archive", filepath.Base(bak))
	}
	if saves, err := ListArchiveSaves(bak); err != nil || len(saves) != 3 {
		t.Errorf("the backup does not read back as an archive: %d saves, %v", len(saves), err)
	}
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Error("the backup is not a copy of the archive")
	}
}

func TestBackupOfAPlainFileCopiesThatFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "KHIII_slot0.bin")
	if err := os.WriteFile(p, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	bak, err := BackupOf(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(bak)
	if string(got) != "plain" {
		t.Errorf("backup holds %q, want %q", got, "plain")
	}
}

// The account id lives in a directory name inside the archive, so it has to
// survive being read out of a member path on every platform.
func TestAccountIsFoundInsideAnArchivePath(t *testing.T) {
	p := JoinArchive("/backups/KH3.zip", memberDir+"KHIII_slot0.bin")
	got := candidatesFromPath(p)
	for _, c := range got {
		if c == "76561190000000000" {
			return
		}
	}
	t.Errorf("candidatesFromPath(%q) = %q, which does not include the account directory", p, got)
}

func TestDescribeArchiveReadsPlatformAndAccount(t *testing.T) {
	d := DescribeArchive(fixtureZip(t))
	if !d.Archive {
		t.Error("Archive should be set")
	}
	if d.Platform != "Steam" {
		t.Errorf("platform %q, want %q", d.Platform, "Steam")
	}
	if d.AccountID != "76561190000000000" {
		t.Errorf("account %q, want the directory inside the archive", d.AccountID)
	}
	if d.Cloud {
		t.Error("an archive is not a live Steam Cloud folder")
	}
}

func TestArchiveHelpersIgnorePlainPaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), "KHIII_slot0.bin")
	if err := os.WriteFile(p, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if InArchive(p) || IsArchive(p) {
		t.Error("a plain save is neither in an archive nor an archive")
	}
	if Container(p) != p {
		t.Errorf("Container(%q) = %q, want it unchanged", p, Container(p))
	}
	data, err := ReadFile(p)
	if err != nil || string(data) != "plain" {
		t.Errorf("ReadFile on a plain path returned %q, %v", data, err)
	}
	if err := WriteFile(p, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(p); string(again) != "second" {
		t.Errorf("WriteFile on a plain path wrote %q", again)
	}
}

// A directory that merely ends in .zip is not an archive, and must not be
// treated as one.
func TestDirectoryNamedZipIsNotAnArchive(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "saves.zip")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if IsArchive(dir) {
		t.Error("a directory is not an archive, whatever it is called")
	}
}

// A backup of a private save must not be created world-readable, must not
// silently overwrite an earlier backup made in the same second, and must not
// read an arbitrarily large file into memory.
func TestBackupOfPreservesModeAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "KHIII_slot0.bin")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 17, 4, 42, 0, time.UTC)

	first, err := BackupOf(src, now)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("backup mode %o, want 0600: a backup of a private save must stay private", got)
	}

	// Same timestamp: must not clobber the first.
	second, err := BackupOf(src, now)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("second backup reused the first name and destroyed it")
	}
	if _, err := os.Stat(first); err != nil {
		t.Errorf("first backup no longer exists: %v", err)
	}
}

func TestBackupOfRefusesAnEnormousFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "KHIII_slot0.bin")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse: costs no disk, but reports a size past the cap.
	if err := f.Truncate(maxContainerSize + 1); err != nil {
		f.Close()
		t.Skip("filesystem does not support sparse files:", err)
	}
	f.Close()

	if _, err := BackupOf(src, time.Now()); err == nil {
		t.Error("BackupOf read a file past the size cap instead of refusing")
	}
}
