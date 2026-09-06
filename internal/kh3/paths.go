package kh3

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// SaveDir is one discovered save folder, or one backup zip standing in for a
// save folder.
type SaveDir struct {
	Path     string `json:"path"`
	Platform string `json:"platform"` // Steam / Epic Games Store
	// AccountID is the numeric directory: a SteamID64 on Steam, the constant
	// EpicAccount on Epic, and empty for a layout that has no such directory.
	AccountID string `json:"accountId"`
	Cloud     bool   `json:"cloud"`   // a steam_autocloud.vdf sits alongside
	Archive   bool   `json:"archive"` // a .zip, not a directory on disk
}

// saveRoots returns the directories that can contain a
// "KINGDOM HEARTS III" folder on this OS.
func saveRoots() []string {
	var roots []string
	home, err := os.UserHomeDir()
	if err != nil {
		return roots
	}
	switch runtime.GOOS {
	case "windows":
		// Documents is frequently redirected into OneDrive, which is the
		// single most common reason a save folder "isn't there".
		roots = append(roots,
			filepath.Join(home, "Documents"),
			filepath.Join(home, "OneDrive", "Documents"),
			filepath.Join(home, "OneDrive - Personal", "Documents"),
		)
		if od := os.Getenv("OneDrive"); od != "" {
			roots = append(roots, filepath.Join(od, "Documents"))
		}
	default:
		// KH3 is Windows-only on PC, so on Linux and macOS the save lives
		// inside a Proton or Wine prefix.
		roots = append(roots,
			filepath.Join(home, "Documents"),
			filepath.Join(home, "Games", "kingdom-hearts-iii", "drive_c", "users", "steamuser", "Documents"),
		)
		for _, steam := range []string{
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".local", "share", "Steam"),
			filepath.Join(home, "Library", "Application Support", "Steam"),
		} {
			matches, _ := filepath.Glob(filepath.Join(steam, "steamapps", "compatdata", "*", "pfx",
				"drive_c", "users", "steamuser", "Documents"))
			roots = append(roots, matches...)
		}
	}
	return roots
}

// FindSaveDirs locates every KH3 save folder on this machine.
func FindSaveDirs() []SaveDir { return findSaveDirsIn(saveRoots()) }

func findSaveDirsIn(roots []string) []SaveDir {
	var out []SaveDir
	seen := map[string]bool{}
	for _, root := range roots {
		base := filepath.Join(root, "KINGDOM HEARTS III")
		for _, layout := range []struct {
			glob       string
			hasAccount bool
		}{
			// Steam, and Epic as the game writes it: a directory named after
			// the account id. Epic's is the constant EpicAccount rather than a
			// per-user value, but it sits in the same place.
			{filepath.Join(base, "*", "*", "SaveGames", "kh3sv2", "data"), true},
			// No account level. Shared Epic saves unpack to this shape, so
			// someone who dropped one into Documents has it on disk whether or
			// not the game itself ever writes it that way.
			{filepath.Join(base, "*", "SaveGames", "kh3sv2", "data"), false},
		} {
			matches, _ := filepath.Glob(layout.glob)
			for _, m := range matches {
				abs, err := filepath.Abs(m)
				if err != nil || seen[abs] {
					continue
				}
				if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
					continue
				}
				seen[abs] = true

				// .../<owner>/SaveGames/kh3sv2/data. owner is the account
				// directory when the layout has one and the platform when it
				// does not. steam_autocloud.vdf sits beside SaveGames either
				// way, so it is always beside owner.
				owner := filepath.Dir(filepath.Dir(filepath.Dir(abs)))
				account, platform := "", filepath.Base(owner)
				if layout.hasAccount {
					account, platform = platform, filepath.Base(filepath.Dir(owner))
				}
				_, cloudErr := os.Stat(filepath.Join(owner, "steam_autocloud.vdf"))

				out = append(out, SaveDir{
					Path: abs, Platform: platform, AccountID: account, Cloud: cloudErr == nil,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// isSaveName reports whether a bare file name is one of the game's saves.
// Shared with the archive reader so a member inside a zip is recognised by
// exactly the same rule as a file on disk.
func isSaveName(name string) bool {
	return strings.HasPrefix(name, "KHIII_") && strings.HasSuffix(name, ".bin")
}

// ListSaveFiles returns the KHIII_*.bin files in a directory.
func ListSaveFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && isSaveName(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// GameIsRunning is a best-effort check. KH3 rewrites the whole slot when it
// saves, so an edit made while it is running will simply be overwritten.
// A false result never guarantees the game is closed.
func GameIsRunning() bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("tasklist", "/FO", "CSV", "/NH")
	default:
		// comm= is the executable name only. Using command= would include
		// arguments, so anything holding a save path in its argv (a file
		// manager, a backup job, this tool's own CLI) looked like the game.
		cmd = exec.Command("ps", "ax", "-o", "comm=")
	}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// Match the game, not ourselves. "kh3" was the original needle and it
	// matched this very binary (kh3-save-editor), so every user saw a permanent
	// "the game is running" warning.
	self := strings.ToLower(filepath.Base(os.Args[0]))
	for _, line := range strings.Split(strings.ToLower(string(out)), "\n") {
		if self != "" && strings.Contains(line, self) {
			continue
		}
		for _, needle := range gameProcessNames {
			if strings.Contains(line, needle) {
				return true
			}
		}
	}
	return false
}

// gameProcessNames are deliberately specific: anything shorter produces
// false positives against unrelated processes.
// Linux truncates comm to 15 characters, so "KINGDOM HEARTS III.exe" arrives
// as "KINGDOM HEARTS ". Match that prefix rather than the full title.
var gameProcessNames = []string{
	"kingdom hearts",
	"kingdomhearts",
}
