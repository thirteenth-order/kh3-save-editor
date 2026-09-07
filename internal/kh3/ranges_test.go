package kh3_test

import (
	"encoding/json"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// The schema's Min/Max are the storage range of a field, and the browser
// validator refuses a document outside them before it is ever sent. Patch has
// to refuse exactly the same values, or the two disagree about what is legal
// and the disagreement is invisible: the setters truncate or clamp, so an
// out-of-range value is written as something else and reported as though it
// had been taken. It was 92 fields' worth of exactly that (header.level 300
// stored 44, inventory count 300 stored 255 and said 300) while jscheck's
// mirror check asserted in a comment that Patch refused what it refused.
//
// This walks the schema rather than listing fields, so a field added with a
// range and no check fails here on the day it is added.
//
// Both directions matter. Refusing more than the browser does is the same bug
// wearing the other hat: it would make the editor offer a value the CLI throws
// out, so the bounds themselves have to be accepted.
type rangeProbe struct {
	path string
	doc  map[string]any
	v    int64
}

// entryFor builds one entry object holding field f, plus the siblings the
// entry cannot be read without: an equipment slot means nothing until its type
// byte says which table its id is an id in.
func entryFor(sec kh3.Section, f kh3.Field, v int64) any {
	e := map[string]any{f.Key: v}
	for _, sib := range sec.Entry {
		if sib.Key == "type" && sib.Kind == kh3.KindEnum {
			e["type"] = 1
		}
	}
	return e
}

// collectRanges walks a section and every section under it, emitting one probe
// per bound of every field that declares one. wrap nests a value back up to
// the top of the document.
func collectRanges(sec kh3.Section, wrap func(any) map[string]any, path string,
	beyond bool, out *[]rangeProbe) {

	at := func(k string) string { return path + "." + k }
	push := func(p string, f kh3.Field, mk func(int64) any) {
		// A read-only field is refused for being read-only before its value is
		// ever looked at, so it says nothing about range checking either way.
		if f.Readonly {
			return
		}
		for _, bound := range []*int64{f.Min, f.Max} {
			if bound == nil {
				continue
			}
			v := *bound
			if beyond {
				if bound == f.Min {
					v--
				} else {
					v++
				}
			}
			*out = append(*out, rangeProbe{p, wrap(mk(v)), v})
		}
	}
	nest := func(sub kh3.Section, key string) func(any) map[string]any {
		return func(x any) map[string]any { return wrap(map[string]any{key: x}) }
	}

	switch sec.Shape {
	case "object":
		for _, f := range sec.Fields {
			f := f
			push(at(f.Key), f, func(v int64) any { return map[string]any{f.Key: v} })
		}
		for _, sub := range sec.Sections {
			collectRanges(sub, nest(sub, sub.Key), at(sub.Key), beyond, out)
		}
	case "group":
		for _, sub := range sec.Sections {
			collectRanges(sub, nest(sub, sub.Key), at(sub.Key), beyond, out)
		}
	case "index":
		for _, f := range sec.Entry {
			f := f
			push(at("0."+f.Key), f, func(v int64) any {
				inner := entryFor(sec, f, v)
				if len(sec.EntryKeys) > 0 {
					inner = map[string]any{sec.EntryKeys[0]: inner}
				}
				return map[string]any{"0": inner}
			})
		}
	case "names":
		if len(sec.Keys) == 0 {
			return
		}
		key := sec.Keys[0]
		for _, f := range sec.Entry {
			f := f
			push(at(key+"."+f.Key), f, func(v int64) any {
				return map[string]any{key: entryFor(sec, f, v)}
			})
		}
		for _, sub := range sec.Sections {
			sub := sub
			collectRanges(sub, func(x any) map[string]any {
				return wrap(map[string]any{key: map[string]any{sub.Key: x}})
			}, at(key+"."+sub.Key), beyond, out)
		}
	}
}

func rangeProbes(t *testing.T, beyond bool) []rangeProbe {
	t.Helper()
	var out []rangeProbe
	for _, sec := range kh3.Sections() {
		sec := sec
		collectRanges(sec, func(x any) map[string]any {
			return map[string]any{sec.Key: x}
		}, sec.Key, beyond, &out)
	}
	if len(out) == 0 {
		t.Fatal("the schema walk found no bounded field, so this test asserts nothing")
	}
	return out
}

func TestPatchRefusesWhatTheSchemaCallsOutOfRange(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	probes := rangeProbes(t, true)
	for _, pr := range probes {
		doc, err := json.Marshal(pr.doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := kh3.Patch(p, doc); err == nil {
			t.Errorf("Patch accepted %s = %d, which the schema puts outside the field "+
				"and the browser refuses outright", pr.path, pr.v)
		}
	}
	t.Logf("%d out-of-range values, all refused", len(probes))
}

// The other direction: a value exactly on the bound is legal, and Patch may
// not be stricter than the description the editor is built from.
func TestPatchAcceptsTheBoundsTheSchemaDeclares(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	probes := rangeProbes(t, false)
	for _, pr := range probes {
		doc, err := json.Marshal(pr.doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := kh3.Patch(p, doc); err != nil {
			// The munny ledger is an identity, not three free numbers, so a
			// document naming one of the three is answered arithmetically and
			// can refuse for that reason rather than for range.
			t.Errorf("Patch refused %s = %d, which is a bound the schema declares "+
				"and the editor will offer: %v", pr.path, pr.v, err)
		}
	}
	t.Logf("%d bounds, all accepted", len(probes))
}

// A section that is not an object is an error, not a no-op.
//
// Patch used to reach for header, characters and inventory with a checked type
// assertion and skip the section when it was anything else, so
// {"header": "level 42 please"} reported no changes and wrote nothing while
// looking like it had worked. The browser validator has always refused it, so
// this is the same mirror the ranges above are: what one rejects, the other
// rejects.
func TestPatchRefusesASectionThatIsNotAnObject(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	for _, sec := range kh3.Sections() {
		for _, notObject := range []string{`"a string"`, `42`, `[1,2,3]`, `null`} {
			doc := []byte(`{"` + sec.Key + `":` + notObject + `}`)
			_, changes, err := kh3.Patch(p, doc)
			if err == nil {
				t.Errorf("Patch accepted %s = %s and reported %d changes, rather than "+
					"saying it is not an object", sec.Key, notObject, len(changes))
			}
		}
	}
}
