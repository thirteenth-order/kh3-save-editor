package kh3

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	plain := buildPlain(1)
	if got := DetectFormat(plain); got != FormatPlain {
		t.Errorf("unwrapped save detected as %v, want plain", got)
	}
	blob, err := Wrap(plain, key(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := DetectFormat(blob); got != FormatSteam {
		t.Errorf("wrapped save detected as %v, want steam", got)
	}
	// A short or empty file must not panic, and must not be called plain.
	for _, b := range [][]byte{nil, {}, {'S'}, {'S', '@', 'v'}} {
		if got := DetectFormat(b); got != FormatSteam {
			t.Errorf("%q detected as %v", b, got)
		}
	}
}

// TestOpenSealRoundTripInBothForms: reading and writing must be lossless in
// each form, and Seal must rebuild whichever integrity fields that form has.
func TestOpenSealRoundTripInBothForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		form Format
		key  func(*testing.T) []byte
	}{
		{"steam", FormatSteam, key},
		{"plain", FormatPlain, func(*testing.T) []byte { return nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := tc.key(t)
			plain := buildPlain(3)
			blob, err := Seal(plain, tc.form, k)
			if err != nil {
				t.Fatal(err)
			}
			if got := DetectFormat(blob); got != tc.form {
				t.Fatalf("sealed as %v, detected as %v", tc.form, got)
			}
			back, form, err := Open(blob, k)
			if err != nil {
				t.Fatal(err)
			}
			if form != tc.form {
				t.Errorf("Open reported %v, want %v", form, tc.form)
			}
			if !equalBytes(back, plain) {
				t.Error("round trip changed the save")
			}
		})
	}
}

// TestConvertBetweenFormsIsLossless is the console-to-PC bridge: strip the
// Steam wrapper, put it back on, and land on the same bytes.
func TestConvertBetweenFormsIsLossless(t *testing.T) {
	k := key(t)
	original, err := Wrap(buildPlain(3), k)
	if err != nil {
		t.Fatal(err)
	}

	bare, _, err := Open(original, k)
	if err != nil {
		t.Fatal(err)
	}
	stripped, err := Seal(bare, FormatPlain, nil)
	if err != nil {
		t.Fatal(err)
	}

	reopened, form, err := Open(stripped, nil)
	if err != nil {
		t.Fatalf("reading the stripped save without a key: %v", err)
	}
	if form != FormatPlain {
		t.Fatalf("stripped save reads as %v", form)
	}
	rewrapped, err := Seal(PadToBlock(reopened), FormatSteam, k)
	if err != nil {
		t.Fatal(err)
	}
	if !equalBytes(rewrapped, original) {
		t.Error("steam -> plain -> steam did not return the original bytes")
	}
}

// TestSealPlainRebuildsTheCRC: the CRC at 0x0C is the only integrity field an
// unwrapped save has, so an edit that does not rebuild it produces a file the
// game rejects.
func TestSealPlainRebuildsTheCRC(t *testing.T) {
	plain := buildPlain(1)
	plain[0x28] = 0xFF // munny, inside the CRC'd body
	binary.LittleEndian.PutUint32(plain[0x0C:], 0)

	out, err := Seal(plain, FormatPlain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(out[0x0C:]) == 0 {
		t.Error("Seal left the CRC at zero")
	}
	if _, _, err := Open(out, nil); err != nil {
		t.Errorf("sealed save does not reopen: %v", err)
	}
}

func TestOpenPlainRejectsDamage(t *testing.T) {
	base, err := Seal(buildPlain(1), FormatPlain, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]byte) []byte{
		"body byte flipped": func(b []byte) []byte {
			b[0x1000] ^= 0xFF
			return b
		},
		"crc field changed": func(b []byte) []byte {
			b[0x0C] ^= 0xFF
			return b
		},
		"truncated": func(b []byte) []byte { return b[:0x800] },
		"filesize field absurd": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[0x04:], 0xFFFFFFF0)
			return b
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			b := damage(append([]byte(nil), base...))
			if _, _, err := Open(b, nil); err == nil {
				t.Error("damaged save was accepted")
			}
		})
	}
}

// TestOpenSteamStillNeedsTheRightKey: a wrapped save must not fall through to
// the keyless path just because the key is wrong.
func TestOpenSteamStillNeedsTheRightKey(t *testing.T) {
	blob, err := Wrap(buildPlain(1), key(t))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := DeriveKey("76561190000000001")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(blob, wrong); err == nil {
		t.Fatal("the wrong key opened the save")
	}
	if _, _, err := Open(blob, nil); err == nil {
		t.Fatal("a nil key opened an encrypted save")
	}
}

func TestPadToBlock(t *testing.T) {
	aligned := make([]byte, 64)
	if got := PadToBlock(aligned); len(got) != 64 {
		t.Errorf("padded an already aligned buffer to %d", len(got))
	}
	for _, n := range []int{1, 15, 17, 63} {
		got := PadToBlock(make([]byte, n))
		if len(got)%BlockSize != 0 || len(got) < n || len(got)-n >= BlockSize {
			t.Errorf("PadToBlock(%d) = %d bytes", n, len(got))
		}
	}
}

// TestPaddingDoesNotDisturbEitherIntegrityField: both are computed over a
// prefix bounded by the filesize field, so growing the tail must not move them.
func TestPaddingDoesNotDisturbEitherIntegrityField(t *testing.T) {
	k := key(t)
	plain := buildPlain(1)
	short := plain[:len(plain)-5]

	blob, err := Seal(PadToBlock(short), FormatSteam, k)
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := Open(blob, k)
	if err != nil {
		t.Fatalf("a padded save does not reopen: %v", err)
	}
	if !equalBytes(back[:len(short)], short) {
		t.Error("padding changed the save body")
	}
}

func TestFormatString(t *testing.T) {
	for form, want := range map[Format]string{FormatSteam: "steam", FormatPlain: "plain"} {
		if got := form.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", form, got, want)
		}
	}
	if !FormatSteam.NeedsKey() || FormatPlain.NeedsKey() {
		t.Error("NeedsKey is backwards")
	}
}

// TestPlainErrorsNameTheProblem: someone handed a corrupt file has to be told
// which field disagreed, not just that something is wrong.
func TestPlainErrorsNameTheProblem(t *testing.T) {
	b, err := Seal(buildPlain(1), FormatPlain, nil)
	if err != nil {
		t.Fatal(err)
	}
	b[0x1000] ^= 0xFF
	_, _, err = Open(b, nil)
	if err == nil || !strings.Contains(err.Error(), "CRC32") {
		t.Errorf("error = %v, want it to name the CRC", err)
	}
}
