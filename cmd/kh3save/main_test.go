package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

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
