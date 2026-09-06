package kh3_test

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// schemaPaths flattens the schema into the set of leaf paths a dump can carry.
// An index is collapsed to "*" because a dump only writes the entries a save
// actually uses; a names shape is expanded, because there the key set is part
// of what is being asserted.
func schemaPaths(sections []kh3.Section, prefix string, out map[string]bool) {
	for _, s := range sections {
		base := s.Key
		if prefix != "" {
			base = prefix + "." + s.Key
		}
		switch s.Shape {
		case "object":
			for _, f := range s.Fields {
				out[base+"."+f.Key] = true
			}
			schemaPaths(s.Sections, base, out)
		case "group":
			schemaPaths(s.Sections, base, out)
		case "index":
			entry := base + ".*"
			if len(s.EntryKeys) > 0 {
				for _, k := range s.EntryKeys {
					for _, f := range s.Entry {
						out[entry+"."+k+"."+f.Key] = true
					}
				}
			} else {
				for _, f := range s.Entry {
					out[entry+"."+f.Key] = true
				}
			}
			schemaPaths(s.Sections, entry, out)
		case "names":
			for _, k := range s.Keys {
				at := base + "." + k
				for _, f := range s.Entry {
					out[at+"."+f.Key] = true
				}
				schemaPaths(s.Sections, at, out)
			}
		default:
			panic("unknown section shape " + s.Shape)
		}
	}
}

// docPaths flattens a dump the same way. A key that parses as a number is an
// index and collapses to "*", which is what makes the two sets comparable.
func docPaths(v any, prefix string, out map[string]bool) {
	m, ok := v.(map[string]any)
	if !ok {
		out[prefix] = true
		return
	}
	if len(m) == 0 {
		return
	}
	for k, sub := range m {
		key := k
		if _, err := strconv.ParseInt(k, 0, 64); err == nil {
			key = "*"
		}
		at := key
		if prefix != "" {
			at = prefix + "." + key
		}
		docPaths(sub, at, out)
	}
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestSchemaCoversEveryDumpedKey is the reason the schema is worth having. A
// description that is only documentation goes stale the first time a field is
// added; this fails the build instead. It runs on the full-size fixture so the
// record block is in the document too.
func TestSchemaCoversEveryDumpedKey(t *testing.T) {
	doc, err := kh3.Dump(fixture.BuildFull(fixture.Default()), "", kh3.CharCount)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(doc, &d); err != nil {
		t.Fatal(err)
	}
	// The three keys that describe the document rather than the save.
	delete(d, "_format")
	delete(d, "_note")
	delete(d, "account")

	have := map[string]bool{}
	docPaths(d, "", have)
	want := map[string]bool{}
	schemaPaths(kh3.Sections(), "", want)

	var missing, extra []string
	for _, p := range sortedSet(have) {
		if !want[p] {
			missing = append(missing, p)
		}
	}
	for _, p := range sortedSet(want) {
		if !have[p] {
			extra = append(extra, p)
		}
	}
	if len(missing) > 0 {
		t.Errorf("Dump writes %d key(s) the schema does not describe:\n  %s\n"+
			"add them to Sections() in schema.go, or the editor will not know they exist",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("the schema describes %d key(s) Dump never writes:\n  %s",
			len(extra), strings.Join(extra, "\n  "))
	}
}

// Every enum a field points at has to exist, or the editor renders a picker
// with nothing in it and no error anywhere.
func TestEveryEnumTableReferencedExists(t *testing.T) {
	tables := kh3.Tables()
	var walk func([]kh3.Section, string)
	walk = func(secs []kh3.Section, prefix string) {
		for _, s := range secs {
			at := prefix + s.Key
			if s.IndexTable != "" {
				if _, ok := tables[s.IndexTable]; !ok {
					t.Errorf("%s names index table %q, which Tables() does not have", at, s.IndexTable)
				}
			}
			for _, f := range append(append([]kh3.Field{}, s.Fields...), s.Entry...) {
				if f.Table == "" {
					continue
				}
				if _, ok := tables[f.Table]; !ok {
					t.Errorf("%s.%s names table %q, which Tables() does not have", at, f.Key, f.Table)
				}
			}
			walk(s.Sections, at+".")
		}
	}
	walk(kh3.Sections(), "")
	for _, name := range kh3.EquipTables() {
		if _, ok := tables[name]; !ok {
			t.Errorf("EquipTables names %q, which Tables() does not have", name)
		}
	}
}

// A field that says it is bounded has to be bounded the right way round.
func TestSchemaBoundsAreSane(t *testing.T) {
	var walk func([]kh3.Section, string)
	walk = func(secs []kh3.Section, prefix string) {
		for _, s := range secs {
			at := prefix + s.Key
			for _, f := range append(append([]kh3.Field{}, s.Fields...), s.Entry...) {
				where := at + "." + f.Key
				if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
					t.Errorf("%s: min %d is above max %d", where, *f.Min, *f.Max)
				}
				if f.SoftMin != nil && f.SoftMax != nil && *f.SoftMin > *f.SoftMax {
					t.Errorf("%s: softMin %d is above softMax %d", where, *f.SoftMin, *f.SoftMax)
				}
				if (f.SoftMin != nil || f.SoftMax != nil) && f.SoftNote == "" {
					t.Errorf("%s: has a soft bound and no note saying why", where)
				}
				if f.Kind == kh3.KindEnum && f.Table == "" {
					t.Errorf("%s: is an enum with no table", where)
				}
			}
			walk(s.Sections, at+".")
		}
	}
	walk(kh3.Sections(), "")
}
