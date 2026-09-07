package kh3_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// The two int32s KHSave.Lib3 calls Unknown00058 and Unknown0005C are one
// int64: a UE4 FDateTime, 100-nanosecond ticks since 0001-01-01. The vectors
// here are synthetic: a real save's timestamp says when its owner was
// playing, which is not something this repo carries.
func TestSavedAtDecodesFDateTimeTicks(t *testing.T) {
	for _, c := range []struct {
		ticks int64
		want  string
	}{
		{0, ""}, // never written by the game
		{636843168000000000, "2019-01-29T00:00:00.000Z"}, // KH3's release day
		{637135310456780000, "2020-01-02T03:04:05.678Z"},
	} {
		p := fixture.Build(fixture.Default())
		binary.LittleEndian.PutUint64(p[kh3.SavedAtOff:], uint64(c.ticks))
		if got := kh3.ReadHeader(p).SavedAtString(); got != c.want {
			t.Errorf("ticks %d read back %q, want %q", c.ticks, got, c.want)
		}
	}
}

// saved_at is reported and never written, because the tick count is larger
// than a double holds exactly and the document round-trips through a browser's
// JSON. Patch has to accept the key and leave the bytes alone; if it ever
// started writing it, a dump-and-patch-back would move the timestamp.
func TestSavedAtIsReportedAndIgnoredOnPatch(t *testing.T) {
	p := fixture.Build(fixture.Default())
	const ticks = 637135310456780000
	binary.LittleEndian.PutUint64(p[kh3.SavedAtOff:], uint64(ticks))

	// A wall-clock write time says when somebody was playing, so it rides the
	// same explicit opt-in as the account id: absent from the dump people
	// share, present in the one they asked to identify.
	shared, err := kh3.Dump(p, "", kh3.CharCount)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Header map[string]any `json:"header"`
	}
	if err := json.Unmarshal(shared, &s); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Header["saved_at"]; ok {
		t.Errorf("a dump with no account id carried saved_at %v", got)
	}
	// The whole body, not just the key: the point is that the timestamp is not
	// in what gets pasted, wherever it might have been rendered.
	if bytes.Contains(shared, []byte("2020-01-02")) {
		t.Error("a dump with no account id still spells out the write time")
	}

	doc, err := kh3.Dump(p, "76561190000000000", kh3.CharCount)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Header map[string]any `json:"header"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		t.Fatal(err)
	}
	if got := d.Header["saved_at"]; got != "2020-01-02T03:04:05.678Z" {
		t.Fatalf("dump reported saved_at %v", got)
	}

	out, changes, err := kh3.Patch(p, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("patching a dump back reported changes: %v", changes)
	}
	if !bytes.Equal(out, p) {
		t.Error("patching a dump back changed the save")
	}
}
