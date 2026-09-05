package kh3

import (
	"regexp"
	"strings"
)

// A SteamID64 identifies a Steam account, and the save encryption key derives
// from it alone, so anywhere the tool shows one it is publishing an identifier.
// Output gets pasted into bug reports and the interface gets screenshotted, so
// the id is masked everywhere it appears for a human to read, and only shown in
// full when explicitly asked for.

// MaskAccount keeps enough of an account id to tell two apart without
// publishing either. Values too short to mask meaningfully pass through.
func MaskAccount(id string) string {
	if len(id) < 10 {
		return id
	}
	return id[:6] + strings.Repeat("*", len(id)-10) + id[len(id)-4:]
}

// steamIDInPath matches a SteamID64 sitting as its own path segment, which is
// how Steam names the directory a save lives in.
var steamIDInPath = regexp.MustCompile(`\b7656\d{13}\b`)

// MaskPath masks any account id embedded in a filesystem path.
//
// Masking the id in the interface accomplishes nothing while the path printed
// beside it still spells the id out in full, which is exactly how Steam names
// the directory: .../Steam/76561190000000000/SaveGames/...
func MaskPath(p string) string {
	return steamIDInPath.ReplaceAllStringFunc(p, MaskAccount)
}
