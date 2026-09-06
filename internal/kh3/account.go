package kh3

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// KeyMatches is a cheap oracle: does key decrypt the first block to "S@vE"?
// Fast enough to try a list of candidate account ids.
func KeyMatches(key, blob []byte) bool {
	if len(blob) < TrailerLen+BlockSize {
		return false
	}
	p, err := decryptHead(blob[:BlockSize], key)
	return err == nil && bytes.Equal(p[:4], Magic)
}

var accountIDRe = regexp.MustCompile(`"accountid"\s*"(\d+)"`)

// EpicAccount is the account id the Epic Games Store build keys its saves
// with. It is a constant, not a per-user id: every Epic install writes to
// "KINGDOM HEARTS III/Epic Games Store/1638/SaveGames/...", so every Epic save
// in the world is encrypted under the same key.
//
// Confirmed against Epic saves from two unrelated uploaders, which both
// decrypt and pass both integrity fields under it and fail under any other
// candidate. kikeprime/KH-Save-Editor hardcodes the same value as its default
// account, which is where the number was first written down.
//
// It is not masked anywhere, because unlike a SteamID64 it identifies nobody.
const EpicAccount = "1638"

// IsAccountID reports whether a path component looks like an id the key could
// derive from. Steam names the directory after the SteamID64 and Epic after
// the constant above, so in both cases it is all digits.
func IsAccountID(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// splitPath breaks a path into components. Split on both separators and on the
// archive marker: zip members always use "/", so on Windows splitting on "\"
// alone left the whole member path as one component and the account directory
// inside a backup zip was never seen.
func splitPath(path string) []string {
	abs, _ := filepath.Abs(path)
	return strings.FieldsFunc(abs, func(r rune) bool {
		return r == '/' || r == filepath.Separator || r == rune(ArchiveSep[0])
	})
}

// candidatesFromPath returns the directory names on the way to path that could
// be the account id.
//
// Two rules, because the id is not always shaped the way Steam shapes it. The
// structural one comes first: whatever directory the SaveGames folder sits in
// is the account directory, whatever it looks like. Then every numeric
// component innermost first, which is what finds a SteamID64 in a path that
// has been rearranged, and what finds Epic's 1638.
func candidatesFromPath(path string) []string {
	parts := splitPath(path)
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for i, p := range parts {
		if strings.EqualFold(p, "SaveGames") && i > 0 {
			add(parts[i-1])
		}
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if IsAccountID(parts[i]) {
			add(parts[i])
		}
	}
	return out
}

// candidatesFromVDF reads accountid out of a nearby steam_autocloud.vdf. That
// is the 32-bit form, so the SteamID64 it implies is offered too.
func candidatesFromVDF(path string) []string {
	dir, _ := filepath.Abs(filepath.Dir(path))
	var out []string
	for i := 0; i < 5; i++ {
		if data, err := os.ReadFile(filepath.Join(dir, "steam_autocloud.vdf")); err == nil {
			for _, m := range accountIDRe.FindAllStringSubmatch(string(data), -1) {
				out = append(out, m[1])
				if n, err := strconv.ParseUint(m[1], 10, 64); err == nil {
					out = append(out, strconv.FormatUint(n+76561197960265728, 10))
				}
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return out
}

// ResolveAccount works out which account id keys blob.
func ResolveAccount(path string, blob []byte, explicit string) (string, []byte, error) {
	if explicit != "" {
		key, err := DeriveKey(explicit)
		if err != nil {
			return "", nil, err
		}
		if !KeyMatches(key, blob) {
			// The id came from the caller so echoing it tells them nothing new, but
			// the path can carry a different account id in a directory name.
			return "", nil, fmt.Errorf("account id %q does not decrypt %s", explicit, MaskPath(path))
		}
		return explicit, key, nil
	}

	var tried []string
	pool := candidatesFromPath(path)
	if env := strings.TrimSpace(os.Getenv("KH3_ACCOUNT")); env != "" {
		pool = append([]string{env}, pool...)
	}
	pool = append(pool, candidatesFromVDF(path)...)
	// Last, because it is a guess about the release rather than a reading of
	// this file's surroundings, and because a shared Epic save is routinely
	// unpacked somewhere with no account directory left on the path at all.
	// Costing one AES block to find out is worth it.
	pool = append(pool, EpicAccount)

	seen := map[string]bool{}
	for _, cand := range pool {
		if seen[cand] {
			continue
		}
		seen[cand] = true
		tried = append(tried, cand)
		key, err := DeriveKey(cand)
		if err != nil {
			continue
		}
		if KeyMatches(key, blob) {
			return cand, key, nil
		}
	}
	// Masked, because this is the message a stuck user pastes into a bug
	// report. The ids are still distinguishable enough to see which candidates
	// were considered, which is all the message is for.
	masked := make([]string, len(tried))
	for i, t := range tried {
		masked[i] = MaskAccount(t)
	}
	return "", nil, fmt.Errorf(
		"could not determine the account id for %s\n  tried: %s\n"+
			"  pass it with -account <SteamID64>, or set KH3_ACCOUNT\n"+
			"  (an Epic Games Store save is keyed to %s, which was tried above)",
		MaskPath(path), strings.Join(masked, ", "), EpicAccount)
}
