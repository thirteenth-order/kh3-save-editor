package kh3_test

import (
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

func TestStringFieldsRoundTrip(t *testing.T) {
	p := fixture.Build(fixture.Default())
	for _, f := range kh3.StringFields {
		want := "/Game/Levels/" + f.Name
		if err := kh3.SetString(p, f, want); err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		if got := kh3.GetString(p, f); got != want {
			t.Errorf("%s read back %q", f.Name, got)
		}
	}
}

// Writing a shorter value over a longer one must not leave the tail of the old
// one behind: the game reads to the first NUL, but everything after it is
// still in the file and still lands in a dump.
func TestWritingAStringClearsTheWholeField(t *testing.T) {
	p := fixture.Build(fixture.Default())
	f, _ := kh3.StringFieldByName("map_path")
	if err := kh3.SetString(p, f, strings.Repeat("A", 200)); err != nil {
		t.Fatal(err)
	}
	if err := kh3.SetString(p, f, "short"); err != nil {
		t.Fatal(err)
	}
	for i, b := range p[f.Off+len("short") : f.Off+f.Len] {
		if b != 0 {
			t.Fatalf("byte %d of the field is %#x, not cleared", i, b)
		}
	}
}

// A value that exactly fills the field would leave no room for the terminator,
// and the game would read on into whatever follows.
func TestAStringThatFillsTheFieldIsRefused(t *testing.T) {
	p := fixture.Build(fixture.Default())
	f, _ := kh3.StringFieldByName("map_spawn")
	before := append([]byte(nil), p...)
	err := kh3.SetString(p, f, strings.Repeat("x", f.Len))
	if err == nil {
		t.Fatal("a value with no room for a terminator was accepted")
	}
	if !strings.Contains(err.Error(), "terminator") {
		t.Errorf("error does not say why: %v", err)
	}
	if string(p) != string(before) {
		t.Error("a refused write still changed the save")
	}
}

func TestPatchEditsTheMapPath(t *testing.T) {
	p := fixture.Build(fixture.Default())
	out, changes, err := kh3.Patch(p, []byte(
		`{"header":{"map_path":"/Game/Levels/ts/ts_01/ts_01","map_spawn":"ts_01_Lv_Save_01"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Errorf("expected two changes, got %v", changes)
	}
	f, _ := kh3.StringFieldByName("map_path")
	if got := kh3.GetString(out, f); got != "/Game/Levels/ts/ts_01/ts_01" {
		t.Errorf("map_path is %q", got)
	}
}

func TestPatchRefusesAnOverlongMapPath(t *testing.T) {
	p := fixture.Build(fixture.Default())
	_, _, err := kh3.Patch(p, []byte(
		`{"header":{"map_path":"`+strings.Repeat("z", 300)+`"}}`))
	if err == nil {
		t.Fatal("an overlong map_path was accepted")
	}
	if !strings.Contains(err.Error(), "map_path") {
		t.Errorf("error does not name the field: %v", err)
	}
}

func TestPatchRefusesANonStringForATextField(t *testing.T) {
	p := fixture.Build(fixture.Default())
	if _, _, err := kh3.Patch(p, []byte(`{"header":{"map_path":7}}`)); err == nil {
		t.Fatal("a number was accepted for a text field")
	}
}

// A key that is neither editable nor one of the values Dump derives is a typo,
// and used to be dropped in silence.
func TestUnknownHeaderKeyIsRejected(t *testing.T) {
	p := fixture.Build(fixture.Default())
	_, _, err := kh3.Patch(p, []byte(`{"header":{"munney":100}}`))
	if err == nil {
		t.Fatal("a misspelled header key was accepted")
	}
	if !strings.Contains(err.Error(), "munney") {
		t.Errorf("error does not name the key: %v", err)
	}
}

// The derived keys a dump carries are still accepted, or the obvious
// workflow (dump, change one number, patch the whole thing back) breaks.
func TestDerivedHeaderKeysAreStillAccepted(t *testing.T) {
	p := fixture.Build(fixture.Default())
	_, changes, err := kh3.Patch(p, []byte(
		`{"header":{"playtime":"9:99:99","world_logo_name":"Nowhere",`+
			`"location_name":"Nowhere","save_icon_name":"Nobody"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("derived keys should change nothing, got %v", changes)
	}
}

func TestKeychainUpgradesRoundTrip(t *testing.T) {
	p := fixture.Build(fixture.Default())
	out, changes, err := kh3.Patch(p, []byte(`{"keychain_upgrades":{"3":{"value":5},"7":9}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Errorf("expected two changes, got %v", changes)
	}
	if got := kh3.GetKeychainUpgrade(out, 3); got != 5 {
		t.Errorf("slot 3 is %d", got)
	}
	if got := kh3.GetKeychainUpgrade(out, 7); got != 9 {
		t.Errorf("slot 7 is %d", got)
	}
	if _, _, err := kh3.Patch(p, []byte(`{"keychain_upgrades":{"99":1}}`)); err == nil {
		t.Error("an out-of-range keychain index was accepted")
	}
}
