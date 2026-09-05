package kh3

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFindSaveDirs(t *testing.T) {
	root := t.TempDir()
	// The real shape: <root>/KINGDOM HEARTS III/<platform>/<account>/SaveGames/kh3sv2/data
	acct := filepath.Join(root, "KINGDOM HEARTS III", "Steam", "76561190000000000")
	data := filepath.Join(acct, "SaveGames", "kh3sv2", "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(acct, "steam_autocloud.vdf"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "KHIII_slot0.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A decoy that must be ignored.
	if err := os.WriteFile(filepath.Join(data, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirs := findSaveDirsIn([]string{root})
	if len(dirs) != 1 {
		t.Fatalf("found %d save dirs, want 1", len(dirs))
	}
	d := dirs[0]
	if d.Platform != "Steam" {
		t.Errorf("platform %q", d.Platform)
	}
	if d.AccountID != "76561190000000000" {
		t.Errorf("account %q", d.AccountID)
	}
	if !d.Cloud {
		t.Error("steam_autocloud.vdf was not detected")
	}

	files := ListSaveFiles(d.Path)
	if len(files) != 1 || filepath.Base(files[0]) != "KHIII_slot0.bin" {
		t.Errorf("ListSaveFiles = %v", files)
	}
}

func TestFindSaveDirsIgnoresJunk(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "KINGDOM HEARTS III", "Steam", "123"), 0o755)
	if got := findSaveDirsIn([]string{root}); len(got) != 0 {
		t.Errorf("found %v in a tree with no data dir", got)
	}
	if got := findSaveDirsIn([]string{filepath.Join(root, "nope")}); len(got) != 0 {
		t.Errorf("found %v under a missing root", got)
	}
}

func TestNoCloudFileMeansNoCloudFlag(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "KINGDOM HEARTS III", "Epic Games Store", "abc123", "SaveGames", "kh3sv2", "data")
	os.MkdirAll(data, 0o755)
	dirs := findSaveDirsIn([]string{root})
	if len(dirs) != 1 || dirs[0].Cloud {
		t.Errorf("unexpected result %+v", dirs)
	}
	if dirs[0].Platform != "Epic Games Store" {
		t.Errorf("platform %q", dirs[0].Platform)
	}
}

// Two false positives this check has already had:
//  1. the needle "kh3" matched this very binary, kh3-save-editor
//  2. scanning full command lines matched any process holding a save path
//     in its arguments: a file manager, a backup job, our own CLI
func TestGameDetectionDoesNotMatchItself(t *testing.T) {
	for _, name := range gameProcessNames {
		for _, own := range []string{"kh3save", "kh3save.exe", "kh3.test"} {
			if strings.Contains(own, name) {
				t.Errorf("needle %q matches our own binary name %q", name, own)
			}
		}
	}
	if GameIsRunning() {
		t.Error("GameIsRunning is true while only the test binary is running")
	}
}

// A process merely holding a save path in its arguments is not the game.
func TestGameDetectionIgnoresPathsInArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "KINGDOM HEARTS III", "Steam", "1", "SaveGames", "kh3sv2", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A helper whose command line contains the game's name, which is exactly
	// what used to trigger the false positive.
	probe := exec.Command("sleep", "5")
	probe.Args = []string{"sleep", filepath.Join(dir, "save.bin"), "5"}
	if err := probe.Start(); err != nil {
		t.Skip("cannot start a helper process:", err)
	}
	defer probe.Process.Kill()
	time.Sleep(150 * time.Millisecond)
	if GameIsRunning() {
		t.Error("a process with the game's path in its arguments was mistaken for the game")
	}
}
