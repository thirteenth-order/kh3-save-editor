package kh3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

// A Kingdom Hearts III save is the same structure however it is stored. Only
// the wrapper around it changes: the PC release puts it inside AES-256-ECB
// keyed to the owner's SteamID64 and appends an MD5 trailer, and everything
// else (a save a console tool has already decrypted, the output of this
// tool's own decrypt command, a file someone is inspecting by hand) is the
// structure on its own with nothing around it.
//
// Every command works on both. That is the whole point of naming the wrapper
// separately from the save: a save with no Steam wrapper needs no account id,
// so none of the account plumbing runs, and the commands stop refusing files
// they can read perfectly well.
type Format int

const (
	// FormatSteam is the PC on-disk form: [ciphertext][0x08][md5].
	FormatSteam Format = iota
	// FormatPlain is the save structure with no wrapper. This is what a
	// console save tool hands back once it has opened its own container, and
	// what decrypt writes.
	FormatPlain
)

func (c Format) String() string {
	if c == FormatPlain {
		return "plain"
	}
	return "steam"
}

// NeedsKey reports whether reading or writing this form involves an account
// id at all.
func (c Format) NeedsKey() bool { return c == FormatSteam }

// DetectFormat tells the two forms apart. The magic is only readable
// without the key when there is no key, so finding it at offset 0 is both
// necessary and sufficient: ciphertext matching "S@vE" would mean the file was
// never encrypted.
func DetectFormat(blob []byte) Format {
	if len(blob) >= 4 && bytes.Equal(blob[:4], Magic) {
		return FormatPlain
	}
	return FormatSteam
}

// checkPlain validates the structure and its CRC. The Steam path gets this for
// free from the MD5 trailer; a plain save has only the CRC at 0x0C, so that is
// the one integrity field standing between a truncated download and a save the
// game refuses to load.
func checkPlain(plain []byte) error {
	if len(plain) < 0x20 {
		return fmt.Errorf("file too short (%d bytes)", len(plain))
	}
	if !bytes.Equal(plain[:4], Magic) {
		return errors.New("file does not start with 'S@vE'")
	}
	fs := fileSize(plain)
	if fs == 0 || int(fs)+0x10 > len(plain) {
		return fmt.Errorf("implausible filesize field 0x%x for a %d-byte file", fs, len(plain))
	}
	want := binary.LittleEndian.Uint32(plain[0x0C:0x10])
	if got := crc32.ChecksumIEEE(plain[0x10 : 0x10+fs]); got != want {
		return fmt.Errorf("CRC32 mismatch: header says 0x%08X, body is 0x%08X", want, got)
	}
	return nil
}

// Open reads a save in either form. key may be nil for a plain save, and is
// ignored for one.
func Open(blob, key []byte) ([]byte, Format, error) {
	c := DetectFormat(blob)
	if c == FormatPlain {
		if err := checkPlain(blob); err != nil {
			return nil, c, err
		}
		out := make([]byte, len(blob))
		copy(out, blob)
		return out, c, nil
	}
	plain, err := Unwrap(blob, key)
	return plain, c, err
}

// Seal writes a save back in the form it was read in, rebuilding whichever
// integrity fields that form carries.
func Seal(plain []byte, c Format, key []byte) ([]byte, error) {
	if c == FormatPlain {
		if !bytes.Equal(plain[:4], Magic) {
			return nil, errors.New("plaintext does not start with 'S@vE'")
		}
		out := make([]byte, len(plain))
		copy(out, plain)
		fs := fileSize(out)
		if fs == 0 || int(fs)+0x10 > len(out) {
			return nil, fmt.Errorf("implausible filesize field 0x%x", fs)
		}
		binary.LittleEndian.PutUint32(out[0x0C:0x10], crc32.ChecksumIEEE(out[0x10:0x10+fs]))
		return out, nil
	}
	return Wrap(plain, key)
}

// PadToBlock returns plain grown to an AES block boundary, which encrypting it
// requires. Both integrity fields are computed over a prefix bounded by the
// filesize field, so bytes added past the end change neither one, and the game
// never reads them. A save that came out of a Steam container is already
// aligned and is returned untouched.
func PadToBlock(plain []byte) []byte {
	if len(plain)%BlockSize == 0 {
		return plain
	}
	out := make([]byte, len(plain)+BlockSize-len(plain)%BlockSize)
	copy(out, plain)
	return out
}

// TrimToFileSize returns plain cut back to 0x10 plus the filesize field, which
// is the length a console writes and the inverse of PadToBlock. A real slot is
// 0x94F4E8 bytes there; the same save out of a Steam container is 0x94F4F0,
// carrying eight bytes of AES alignment the game never reads.
//
// Those eight bytes matter on the way to a console. The console-side tools
// rebuild the CRC over 0x10 to end-of-file rather than to 0x10+filesize
// (hzhreal/HTOS does it for CUSA11060/12025/12031/15072, and bucanero's Apollo
// patch for CUSA12031 does the same), which agrees with the game only when
// there is no tail. Hand one of them a padded file and it writes a CRC the
// game rejects.
//
// A save that is already the console length, or one whose filesize field does
// not describe it, is returned untouched: this trims, it never grows and it
// never guesses.
func TrimToFileSize(plain []byte) []byte {
	if len(plain) < 0x20 {
		return plain
	}
	end := 0x10 + int(fileSize(plain))
	if end < 0x20 || end > len(plain) {
		return plain
	}
	return plain[:end]
}
