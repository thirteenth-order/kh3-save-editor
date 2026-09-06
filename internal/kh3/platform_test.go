package kh3

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// epicBlob is a synthetic save wrapped the way the Epic Games Store build
// wraps one: the same Steam container, keyed to the constant every Epic
// install shares.
func epicBlob(t *testing.T) []byte {
	t.Helper()
	k, err := DeriveKey(EpicAccount)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := Wrap(buildPlain(1), k)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// TestEpicSavesResolveWithNothingOnThePath: an Epic save carries no per-user
// id, so a shared one unpacked anywhere at all still has to open. This is the
// whole reason the constant is a candidate rather than something the user has
// to know to type.
func TestEpicSavesResolveWithNothingOnThePath(t *testing.T) {
	t.Setenv("KH3_ACCOUNT", "")
	p := filepath.Join(t.TempDir(), "unpacked", "KHIII_slot0.bin")
	got, key, err := ResolveAccount(p, epicBlob(t), "")
	if err != nil {
		t.Fatalf("an Epic save off any account directory did not resolve: %v", err)
	}
	if got != EpicAccount {
		t.Errorf("resolved to %q, want the Epic constant %q", got, EpicAccount)
	}
	if !KeyMatches(key, epicBlob(t)) {
		t.Error("the returned key does not decrypt the save it was resolved for")
	}
}

// TestEpicSavesResolveInTheirOwnLayout: and in the layout the game writes,
// where the constant is also sitting on the path.
func TestEpicSavesResolveInTheirOwnLayout(t *testing.T) {
	t.Setenv("KH3_ACCOUNT", "")
	p := filepath.Join(t.TempDir(), "Documents", "KINGDOM HEARTS III",
		"Epic Games Store", EpicAccount, "SaveGames", "kh3sv2", "data", "KHIII_slot0.bin")
	got, _, err := ResolveAccount(p, epicBlob(t), "")
	if err != nil {
		t.Fatalf("an Epic save in the Epic layout did not resolve: %v", err)
	}
	if got != EpicAccount {
		t.Errorf("resolved to %q, want %q", got, EpicAccount)
	}
}

// TestTheEpicFallbackDoesNotShadowSteam: the constant is tried last and only
// ever accepted when it decrypts, so adding it cannot make a Steam save report
// the wrong account.
func TestTheEpicFallbackDoesNotShadowSteam(t *testing.T) {
	t.Setenv("KH3_ACCOUNT", "")
	blob, err := Wrap(buildPlain(1), key(t))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "KINGDOM HEARTS III", "Steam", testAccount,
		"SaveGames", "kh3sv2", "data", "KHIII_slot0.bin")
	got, _, err := ResolveAccount(p, blob, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != testAccount {
		t.Errorf("resolved to %q, want the SteamID64 %q", got, MaskAccount(testAccount))
	}
}

// TestCandidatesFromPathTakesTheDirectoryAboveSaveGames: the numeric rule was
// written for Steam, and Epic happens to satisfy it too. Neither is a promise
// about how some other release names the directory, so whatever sits directly
// above SaveGames is offered as well.
func TestCandidatesFromPathTakesTheDirectoryAboveSaveGames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "KINGDOM HEARTS III", "Epic Games Store",
		"ec588173027141ca830c671ff0914555", "SaveGames", "kh3sv2", "data", "KHIII_slot0.bin")
	got := candidatesFromPath(p)
	if len(got) == 0 || got[0] != "ec588173027141ca830c671ff0914555" {
		t.Errorf("candidatesFromPath(%q) = %q; the directory above SaveGames should come first", p, got)
	}
}

// TestTrimToFileSizeIsTheConsoleLength: a console slot is exactly
// 0x10+filesize and the console-side tools rebuild the CRC to end-of-file, so
// the trimmed file has to satisfy that reading of the rule as well as the
// game's.
func TestTrimToFileSizeIsTheConsoleLength(t *testing.T) {
	padded := buildPlain(1)
	fs := int(binary.LittleEndian.Uint32(padded[0x04:]))
	if len(padded) == 0x10+fs {
		t.Fatal("the fixture has no alignment tail, so this test proves nothing")
	}

	trimmed := TrimToFileSize(padded)
	if len(trimmed) != 0x10+fs {
		t.Fatalf("trimmed to %d bytes, want 0x10+filesize = %d", len(trimmed), 0x10+fs)
	}
	stored := binary.LittleEndian.Uint32(trimmed[0x0C:])
	if got := crc32.ChecksumIEEE(trimmed[0x10:]); got != stored {
		t.Errorf("CRC to end-of-file is 0x%08X, stored is 0x%08X: a console tool would rewrite it wrong", got, stored)
	}
	if got := crc32.ChecksumIEEE(trimmed[0x10 : 0x10+fs]); got != stored {
		t.Errorf("CRC by the game's rule is 0x%08X, stored is 0x%08X", got, stored)
	}
	if _, _, err := Open(trimmed, nil); err != nil {
		t.Errorf("the trimmed save no longer opens: %v", err)
	}
	if again := TrimToFileSize(trimmed); len(again) != len(trimmed) {
		t.Errorf("trimming twice changed the length: %d then %d", len(trimmed), len(again))
	}
}

// TestTrimToFileSizeNeverGrowsOrGuesses: it is only ever safe to cut off what
// the filesize field says is past the end. Anything it cannot read that from
// comes back exactly as it went in.
func TestTrimToFileSizeNeverGrowsOrGuesses(t *testing.T) {
	short := []byte("S@vE")
	if got := TrimToFileSize(short); len(got) != len(short) {
		t.Errorf("a %d-byte input came back %d bytes", len(short), len(got))
	}
	lying := buildPlain(1)
	binary.LittleEndian.PutUint32(lying[0x04:], 0xFFFFFFF0)
	if got := TrimToFileSize(lying); len(got) != len(lying) {
		t.Errorf("a filesize field past the end of the file changed the length to %d", len(got))
	}
}

// TestFindSaveDirsFindsBothEpicLayouts: the game writes an account directory
// and a shared save routinely arrives without one. Both have to show up in a
// scan, and the one with no account directory has to report no account rather
// than the platform name, which is what sits in that position.
func TestFindSaveDirsFindsBothEpicLayouts(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "KINGDOM HEARTS III")
	withAcct := filepath.Join(base, "Epic Games Store", EpicAccount, "SaveGames", "kh3sv2", "data")
	without := filepath.Join(base, "Epic Games Store 2", "SaveGames", "kh3sv2", "data")
	for _, d := range []string{withAcct, without} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	dirs := findSaveDirsIn([]string{root})
	if len(dirs) != 2 {
		t.Fatalf("found %d save dirs, want 2: %+v", len(dirs), dirs)
	}
	got := map[string]SaveDir{}
	for _, d := range dirs {
		got[d.Path] = d
	}
	if d := got[withAcct]; d.Platform != "Epic Games Store" || d.AccountID != EpicAccount {
		t.Errorf("account layout read as platform %q account %q", d.Platform, d.AccountID)
	}
	if d := got[without]; d.Platform != "Epic Games Store 2" || d.AccountID != "" {
		t.Errorf("account-less layout read as platform %q account %q, want an empty account",
			d.Platform, d.AccountID)
	}
}

func TestIsAccountID(t *testing.T) {
	for _, s := range []string{testAccount, EpicAccount, "0"} {
		if !IsAccountID(s) {
			t.Errorf("IsAccountID(%q) = false", s)
		}
	}
	for _, s := range []string{"", "Epic Games Store", "kh3sv2", "76561190000000000x", "-1"} {
		if IsAccountID(s) {
			t.Errorf("IsAccountID(%q) = true", s)
		}
	}
}
