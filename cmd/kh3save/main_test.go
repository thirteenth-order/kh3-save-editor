package main

import (
	"reflect"
	"strings"
	"testing"
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
