package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// Double-clicking the executable runs it with no arguments at all. That has to
// open the interface: a usage screen printed into a console window that closes
// again is not something a person can act on.
func TestNoArgumentsOpensTheInterface(t *testing.T) {
	for _, argv := range [][]string{
		{"kh3save"},
		{`C:\Users\Player\Downloads\kh3save.exe`},
		{}, // defensive: argv is never empty in practice, but must not panic
	} {
		command, rest := parseCommand(argv)
		if command != "gui" {
			t.Errorf("parseCommand(%q) = %q, want the interface", argv, command)
		}
		if len(rest) != 0 {
			t.Errorf("parseCommand(%q) passed arguments %q", argv, rest)
		}
	}
}

func TestSubcommandsAndTheirArgumentsSurvive(t *testing.T) {
	cases := []struct {
		argv    []string
		command string
		rest    []string
	}{
		{[]string{"kh3save", "info", "save.bin"}, "info", []string{"save.bin"}},
		{[]string{"kh3save", "swap", "save.bin", "-d", "Proud"}, "swap",
			[]string{"save.bin", "-d", "Proud"}},
		{[]string{"kh3save", "gui", "-no-browser"}, "gui", []string{"-no-browser"}},
		{[]string{"kh3save", "gui"}, "gui", []string{}},
		{[]string{"kh3save", "--version"}, "--version", []string{}},
	}
	for _, c := range cases {
		command, rest := parseCommand(c.argv)
		if command != c.command {
			t.Errorf("parseCommand(%q) command = %q, want %q", c.argv, command, c.command)
		}
		if len(rest) != len(c.rest) || (len(rest) > 0 && !reflect.DeepEqual(rest, c.rest)) {
			t.Errorf("parseCommand(%q) rest = %q, want %q", c.argv, rest, c.rest)
		}
	}
}

// The usage screen is the only place the default is written down, so it has to
// keep saying so.
func TestUsageDocumentsTheNoArgumentDefault(t *testing.T) {
	first := strings.SplitN(usage, "\n", 4)[2]
	if !strings.Contains(first, "kh3save ") || !strings.Contains(strings.ToLower(first), "no arguments") {
		t.Errorf("the first listed command is %q; it should be the no-argument default", first)
	}
}

// The schema subcommand prints the same description the browser builds its
// editor from, so "what can this edit?" has an answer that does not involve
// opening a browser. It has to name every section and stay readable.
func TestSchemaCommandListsEverySection(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdSchema(nil); err != nil {
			t.Fatal(err)
		}
	})
	for _, sec := range kh3.Sections() {
		if !strings.Contains(out, sec.Key) {
			t.Errorf("schema output does not mention the %q section", sec.Key)
		}
	}
	if !strings.Contains(out, kh3.DocFormat) {
		t.Error("schema output does not say which document format it describes")
	}
	// A section the schema marks as spoiling is covered in the browser until
	// the reader asks for it, and that is part of what this description says
	// about a region rather than a detail of one front end. Somebody writing
	// their own has to be able to see it here.
	//
	// A long warning is wrapped across comment lines, so both sides are
	// flattened before comparing: the continuation gutter is joined back up
	// and runs of whitespace collapsed. Comparing the raw strings passes only
	// for warnings short enough to fit one line, which is a check that quietly
	// asserts nothing about the others.
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	joined := flat(strings.ReplaceAll(out, "\n    # ", " "))

	marked := 0
	for _, sec := range kh3.Sections() {
		if sec.Spoils == "" {
			continue
		}
		marked++
		if !strings.Contains(joined, flat("spoilers: "+sec.Spoils)) {
			t.Errorf("schema output does not carry the spoiler warning on %q", sec.Key)
		}
	}
	if marked == 0 {
		t.Error("no section is marked as spoiling, so this check is asserting nothing")
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line has trailing whitespace: %q", line)
			break
		}
	}
}

func TestSchemaCommandJSONMatchesDescribe(t *testing.T) {
	out := captureStdout(t, func() {
		if err := cmdSchema([]string{"-json"}); err != nil {
			t.Fatal(err)
		}
	})
	var got kh3.Schema
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the -json output does not parse: %v", err)
	}
	if got.DocFormat != kh3.DocFormat || len(got.Sections) != len(kh3.Sections()) {
		t.Error("the -json output is not the description Describe returns")
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()
	run()
	w.Close()
	os.Stdout = old
	return <-done
}

// convert -to plain is the bridge to a console, so its output has to be the
// length a console slot actually is. A save out of a Steam container carries
// eight bytes of AES alignment past 0x10+filesize, and the console-side tools
// (hzhreal/HTOS for the PS4 title ids, bucanero's Apollo patch) rebuild the
// CRC to end-of-file, so those eight bytes become a checksum the game
// rejects.
func TestConvertToPlainWritesTheConsoleLength(t *testing.T) {
	dir := t.TempDir()
	key, err := kh3.DeriveKey(fixture.Account)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := kh3.Wrap(fixture.BuildFull(fixture.Default()), key)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "KHIII_slot0.bin")
	if err := os.WriteFile(src, blob, 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "plain")
	captureStdout(t, func() {
		if err := cmdConvert([]string{src, "-to", "plain", "-account", fixture.Account, "-o", out}); err != nil {
			t.Fatal(err)
		}
	})

	got, err := os.ReadFile(filepath.Join(out, "KHIII_slot0.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if want := 0x10 + fixture.FullFileSize; len(got) != want {
		t.Fatalf("converted to %d bytes, want 0x10+filesize = %d", len(got), want)
	}
	stored := binary.LittleEndian.Uint32(got[0x0C:])
	if crc := crc32.ChecksumIEEE(got[0x10:]); crc != stored {
		t.Errorf("CRC to end-of-file is 0x%08X, stored is 0x%08X", crc, stored)
	}

	// And the trip back: a console save is not block aligned, so converting it
	// to the PC form has to pad it before encrypting or the last partial block
	// is dropped.
	back := filepath.Join(dir, "pc")
	captureStdout(t, func() {
		if err := cmdConvert([]string{filepath.Join(out, "KHIII_slot0.bin"),
			"-to", "pc", "-account", fixture.Account, "-o", back}); err != nil {
			t.Fatal(err)
		}
	})
	rewrapped, err := os.ReadFile(filepath.Join(back, "KHIII_slot0.bin"))
	if err != nil {
		t.Fatal(err)
	}
	plain, form, err := kh3.Open(rewrapped, key)
	if err != nil {
		t.Fatalf("the re-wrapped save does not open: %v", err)
	}
	if form != kh3.FormatSteam {
		t.Errorf("re-wrapped save read back as %s", form)
	}
	if !kh3.IsSlot(plain) {
		t.Error("the round trip did not survive as a slot")
	}
}
