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

// candidatesFromPath returns numeric directory names on the way to path,
// innermost first. On Steam the save lives under a SteamID64 directory, and
// that id is the key.
func candidatesFromPath(path string) []string {
	abs, _ := filepath.Abs(path)
	// Split on both separators and on the archive marker. Zip members always
	// use "/", so on Windows splitting on "\" alone left the whole member path
	// as one component and the account directory inside a backup zip was never
	// seen.
	parts := strings.FieldsFunc(abs, func(r rune) bool {
		return r == '/' || r == filepath.Separator || r == rune(ArchiveSep[0])
	})
	seen := map[string]bool{}
	var out []string
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if p == "" || seen[p] {
			continue
		}
		if _, err := strconv.ParseUint(p, 10, 64); err == nil {
			seen[p] = true
			out = append(out, p)
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
			"  pass it with -account <SteamID64>, or set KH3_ACCOUNT",
		MaskPath(path), strings.Join(masked, ", "))
}
