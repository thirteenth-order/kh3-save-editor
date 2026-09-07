package kh3

import (
	"fmt"
	"time"
)

// Save is a save file that has been read off disk and opened: the plaintext
// structure, plus everything needed to put it back the way it came.
//
// There used to be one of these in each front end (the CLI's `loaded` and
// the GUI's `openedSave`) with a matching pair of read and write functions
// either side. They agreed, but only because somebody kept them agreeing: the
// self-check below was written out twice, byte for byte, and it is the check
// standing between an edit and a corrupt save. Two copies of the invariant
// this program is most careful about is the one duplication worth removing
// even if nothing else here were shared.
//
// The rule the whole program rests on: every write re-reads its own output the
// way the game will, and refuses to write if the round trip does not hold.
// OpenFile and Commit are the only places that happens now, so no caller has
// to remember it and no caller can skip a step.
type Save struct {
	// Path is where it was read from, which may name a member inside a zip.
	Path string
	// Account is the id the key was derived from, empty for a save with no
	// Steam wrapper. It is key material: mask it on the way out, never in
	// place, or the masked value derives the wrong key.
	Account string
	Key     []byte
	Plain   []byte
	Format  Format
}

// OpenFile reads a save and opens whichever container form it is stored in.
//
// A save with no Steam wrapper carries no account id and needs no key, so none
// of the account plumbing runs for one. account overrides the id that would
// otherwise be discovered from the path; an empty string means auto-detect.
func OpenFile(path, account string) (*Save, error) {
	blob, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &Save{Path: path, Format: DetectFormat(blob)}
	if s.Format.NeedsKey() {
		if s.Account, s.Key, err = ResolveAccount(path, blob, account); err != nil {
			return nil, err
		}
	}
	if s.Plain, _, err = Open(blob, s.Key); err != nil {
		return nil, err
	}
	return s, nil
}

// SealChecked puts newPlain back into the form this save came in and reads its
// own output back the way the game will, returning the bytes only if the round
// trip holds.
//
// Nothing may write a save without going through here. Both integrity fields
// are rebuilt by Seal, and getting either wrong is what corrupts a save, so
// the output is re-opened, which revalidates the CRC and, for a Steam save,
// the MD5 trailer, then compared back against what was asked for.
func (s *Save) SealChecked(newPlain []byte) ([]byte, error) {
	blob, err := Seal(newPlain, s.Format, s.Key)
	if err != nil {
		return nil, err
	}
	check, _, err := Open(blob, s.Key)
	if err != nil {
		return nil, fmt.Errorf("self-check failed: %w", err)
	}
	// Seal rewrites the CRC at 0x0C, so compare around it.
	if string(check[:0x0C]) != string(newPlain[:0x0C]) ||
		string(check[0x10:]) != string(newPlain[0x10:]) {
		return nil, fmt.Errorf("self-check failed: the save did not read back as it was written")
	}
	return blob, nil
}

// Commit seals newPlain, checks it, and writes it to dst.
//
// Writing over the file this save came from backs it up first and returns
// where the backup went; writing anywhere else does not, because nothing is
// being replaced. For a save inside a zip the backup is of the whole archive,
// since replacing one member rewrites all of it.
func (s *Save) Commit(newPlain []byte, dst string) (backup string, err error) {
	blob, err := s.SealChecked(newPlain)
	if err != nil {
		return "", err
	}
	if dst == s.Path {
		if backup, err = BackupOf(s.Path, time.Now()); err != nil {
			return "", fmt.Errorf("could not write a backup, so nothing was changed: %w", err)
		}
	}
	if err := WriteFile(dst, blob, 0o644); err != nil {
		return backup, err
	}
	return backup, nil
}
