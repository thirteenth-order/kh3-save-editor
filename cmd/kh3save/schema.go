package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// cmdSchema prints the same field description the browser UI builds itself
// out of. It exists so that "what can this thing actually edit?" has an answer
// that does not involve opening a browser or reading the source, and so that
// the answer cannot differ between the two: both come from kh3.Describe.
func cmdSchema(args []string) error {
	f := flag.NewFlagSet("schema", flag.ExitOnError)
	asJSON := f.Bool("json", false, "print the whole description, tables included")
	tables := f.Bool("tables", false, "list the enum tables and how many ids each holds")
	if err := f.Parse(args); err != nil {
		return err
	}
	s := kh3.Describe()

	if *asJSON {
		out, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	if *tables {
		names := make([]string, 0, len(s.Tables))
		for name := range s.Tables {
			names = append(names, name)
		}
		sortStrings(names)
		for _, name := range names {
			fmt.Printf("  %-20s %4d ids\n", name, len(s.Tables[name]))
		}
		return nil
	}

	fmt.Printf("Document format %s. Every path below is a key in dump's output and\n"+
		"patch's input; a key left out of a document is left alone in the save.\n",
		s.DocFormat)
	for _, sec := range s.Sections {
		fmt.Println()
		printSection(sec, "")
	}
	return nil
}

func printSection(sec kh3.Section, prefix string) {
	at := prefix + sec.Key
	head := at
	switch sec.Shape {
	case "index":
		head = fmt.Sprintf("%s.<0-%d>", at, sec.Count-1)
		if sec.IndexTable != "" {
			head += "   (indexed by " + sec.IndexTable + ")"
		}
	case "names":
		head = at + ".<name>"
	}
	fmt.Printf("%s\n", head)
	if sec.Shape == "names" && len(sec.Keys) > 0 {
		for _, line := range wrap("one of: "+strings.Join(sec.Keys, ", "), 74) {
			fmt.Printf("    # %s\n", line)
		}
	}
	if len(sec.EntryKeys) > 0 {
		fmt.Printf("    # each entry is keyed by: %s\n", strings.Join(sec.EntryKeys, ", "))
	}
	if sec.Note != "" {
		for _, line := range wrap(sec.Note, 74) {
			fmt.Printf("    # %s\n", line)
		}
	}
	// Marked rather than folded into the notes above, because it is a
	// different kind of statement: a note says where an offset came from, this
	// says the interface covers this region until the reader asks for it. A
	// reader of this output who is writing their own front end needs to know
	// that, and greps for it.
	if sec.Spoils != "" {
		for _, line := range wrap("spoilers: "+sec.Spoils, 74) {
			fmt.Printf("    # %s\n", line)
		}
	}
	if sec.Requires == "records" {
		fmt.Println("    # only on a save long enough to reach the record block")
	}
	for _, fld := range sec.Fields {
		printField(fld, "  ")
	}
	for _, fld := range sec.Entry {
		printField(fld, "  ")
	}
	base := at + "."
	if sec.Shape == "index" {
		base = at + ".<i>."
	} else if sec.Shape == "names" {
		base = at + ".<name>."
	}
	for _, sub := range sec.Sections {
		printSection(sub, base)
	}
}

func printField(fld kh3.Field, indent string) {
	if fld.Kind == kh3.KindDerv {
		return
	}
	kind := fld.Kind
	if fld.Table != "" {
		kind += " " + fld.Table
	}
	rng := ""
	if fld.Min != nil && fld.Max != nil {
		rng = fmt.Sprintf("%d..%d", *fld.Min, *fld.Max)
	} else if fld.MaxLen > 0 {
		rng = fmt.Sprintf("up to %d bytes", fld.MaxLen)
	}
	flags := ""
	if fld.Readonly {
		flags = " [read-only]"
	}
	line := fmt.Sprintf("%s%-28s %-22s %-24s %-8s%s", indent, fld.Key, kind, rng, fld.Offset, flags)
	fmt.Println(strings.TrimRight(line, " "))
	if fld.SoftNote != "" {
		fmt.Printf("%s  # %s\n", indent, fld.SoftNote)
	}
	if fld.Note != "" {
		for _, line := range wrap(fld.Note, 70) {
			fmt.Printf("%s  # %s\n", indent, line)
		}
	}
}

// wrap breaks a note at a column so a long provenance comment does not run off
// the side of a terminal.
func wrap(s string, width int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len(line)+1+len(w) > width {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
