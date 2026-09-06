package kh3_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// The record block's offset is derived, not copied from upstream. These are
// the two landmarks the derivation rests on, frozen so that a later edit
// cannot quietly move the block without saying so.
func TestRecordBlockSitsWhereTheEvidencePutsIt(t *testing.T) {
	// Upstream's build declares 0x94F2F8; this one declares 0x94F4D8. The
	// shift is that difference and nothing else.
	if got := fixture.FullFileSize - 0x94F2F8; got != kh3.RecordShift {
		t.Errorf("RecordShift is %#x, but this build's filesize is %#x past upstream's",
			kh3.RecordShift, got)
	}
	// Landmark one: the block ends exactly where the live data in the region
	// stops in every sample save.
	if end := kh3.RecordsOff + kh3.RecordsSize; end != 0x83DA8 {
		t.Errorf("record block ends at %#x, and the sample saves stop at 0x83DA8", end)
	}
	// Landmark two, 0x1284 bytes further on and agreeing on the same constant.
	if kh3.PhotoMaxCountOff != 0x85028 {
		t.Errorf("PhotoMaxCountOff is %#x, and the album limit reads 200 at 0x85028",
			kh3.PhotoMaxCountOff)
	}
}

func TestRecordBlockRoundTrips(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	if !kh3.HasRecords(p) {
		t.Fatal("the full fixture should reach the record block")
	}

	for i := range kh3.RecordScores {
		kh3.SetRecordScore(p, i, int32(1000+i))
	}
	for i := 0; i < kh3.FlanCount; i++ {
		kh3.SetFlan(p, i, kh3.Flan{HighScore: int32(i + 1), HighScore2: int32(i + 2), Attempts: int32(i + 3)})
	}
	for i := 0; i < kh3.ShotlockHighCount; i++ {
		kh3.SetShotlockHigh(p, i, -(i + 1))
	}
	for i := 0; i < kh3.AttractionHighCount; i++ {
		kh3.SetAttractionHigh(p, i, int32(9000+i))
	}
	kh3.SetPhotoMaxCount(p, 123)

	for i := range kh3.RecordScores {
		if got := kh3.GetRecordScore(p, i); got != int32(1000+i) {
			t.Errorf("score %d read back %d", i, got)
		}
	}
	for i := 0; i < kh3.FlanCount; i++ {
		want := kh3.Flan{HighScore: int32(i + 1), HighScore2: int32(i + 2), Attempts: int32(i + 3)}
		if got := kh3.GetFlan(p, i); got != want {
			t.Errorf("flan %d read back %+v, want %+v", i, got, want)
		}
	}
	for i := 0; i < kh3.ShotlockHighCount; i++ {
		if got := kh3.GetShotlockHigh(p, i); got != -(i + 1) {
			t.Errorf("shotlock best %d read back %d", i, got)
		}
	}
	for i := 0; i < kh3.AttractionHighCount; i++ {
		if got := kh3.GetAttractionHigh(p, i); got != int32(9000+i) {
			t.Errorf("attraction best %d read back %d", i, got)
		}
	}
	if got := kh3.GetPhotoMaxCount(p); got != 123 {
		t.Errorf("photo max read back %d", got)
	}
}

// The whole point of the fixed-width array accessors: an off-by-one in any of
// them writes into the neighbouring structure and only shows up as a corrupt
// save. Fill every entry of every array with a distinct value and read them
// all back, which is the only shape of test that catches it.
func TestRecordArraysDoNotOverlap(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	for i := range kh3.RecordScores {
		kh3.SetRecordScore(p, i, int32(10000+i))
	}
	for i := 0; i < kh3.FlanCount; i++ {
		kh3.SetFlan(p, i, kh3.Flan{HighScore: int32(20000 + i*3), HighScore2: int32(20001 + i*3), Attempts: int32(20002 + i*3)})
	}
	for i := 0; i < kh3.ShotlockHighCount; i++ {
		kh3.SetShotlockHigh(p, i, 300+i)
	}
	for i := 0; i < kh3.AttractionHighCount; i++ {
		kh3.SetAttractionHigh(p, i, int32(40000+i))
	}
	for i := range kh3.RecordScores {
		if got := kh3.GetRecordScore(p, i); got != int32(10000+i) {
			t.Fatalf("score %d was overwritten: %d", i, got)
		}
	}
	for i := 0; i < kh3.FlanCount; i++ {
		if got := kh3.GetFlan(p, i).HighScore; got != int32(20000+i*3) {
			t.Fatalf("flan %d was overwritten: %d", i, got)
		}
	}
	for i := 0; i < kh3.ShotlockHighCount; i++ {
		if got := kh3.GetShotlockHigh(p, i); got != 300+i {
			t.Fatalf("shotlock best %d was overwritten: %d", i, got)
		}
	}
	// The byte after the last attraction best is where the block ends. It must
	// still be whatever it was, and never something an array setter reached.
	if p[kh3.RecordsOff+kh3.RecordsSize] != 0 {
		t.Fatal("something wrote past the end of the record block")
	}
}

func TestOutOfRangeRecordIndexesAreIgnored(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	before := append([]byte(nil), p...)
	kh3.SetRecordScore(p, -1, 5)
	kh3.SetRecordScore(p, len(kh3.RecordScores), 5)
	kh3.SetFlan(p, -1, kh3.Flan{HighScore: 5})
	kh3.SetFlan(p, kh3.FlanCount, kh3.Flan{HighScore: 5})
	kh3.SetShotlockHigh(p, -1, 5)
	kh3.SetShotlockHigh(p, kh3.ShotlockHighCount, 5)
	kh3.SetAttractionHigh(p, -1, 5)
	kh3.SetAttractionHigh(p, kh3.AttractionHighCount, 5)
	if string(p) != string(before) {
		t.Fatal("an out-of-range index changed the save")
	}
}

// A dump of a full save patched straight back has to be a no-op, the same as
// it already is for the short one. This is what catches anything Dump can say
// that Patch cannot read, and every new field goes through it.
func TestFullDumpPatchRoundTripIsByteIdentical(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	doc, err := kh3.Dump(p, "", kh3.CharCount)
	if err != nil {
		t.Fatal(err)
	}
	out, changes, err := kh3.Patch(p, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("patching a dump back reported %d change(s): %v", len(changes), changes)
	}
	if string(out) != string(p) {
		t.Error("patching a dump back changed the save")
	}
}

func TestRecordsPatchWritesEveryPart(t *testing.T) {
	p := fixture.BuildFull(fixture.Default())
	doc := `{"records":{
	  "attractions":{"0":{"uses":9,"high_score":777}},
	  "shotlocks":{"4":{"uses":1,"high_score":-5}},
	  "minigames":{"verum_rex_high_score":31337,"frozen_slider_medals":3},
	  "flans":{"grape":{"high_score":88,"attempts":2}},
	  "album":{"photo_max_count":150}}}`
	out, changes, err := kh3.Patch(p, []byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Fatal("nothing reported as changed")
	}
	if got := kh3.GetAttractionUse(out, 0); got != 9 {
		t.Errorf("attraction uses %d", got)
	}
	if got := kh3.GetAttractionHigh(out, 0); got != 777 {
		t.Errorf("attraction best %d", got)
	}
	if got := kh3.GetShotlockHigh(out, 4); got != -5 {
		t.Errorf("shotlock best %d", got)
	}
	if got := kh3.GetRecordScore(out, 0); got != 31337 {
		t.Errorf("verum rex %d", got)
	}
	if got := kh3.GetFlan(out, 4); got.HighScore != 88 || got.Attempts != 2 {
		t.Errorf("grape flan %+v", got)
	}
	// A field the document did not mention keeps its value.
	if got := kh3.GetFlan(out, 1).HighScore2; got != 12 {
		t.Errorf("an untouched flan field moved: %d", got)
	}
	if got := kh3.GetPhotoMaxCount(out); got != 150 {
		t.Errorf("photo max %d", got)
	}
}

// A save that stops before the block gets an error naming the reason, not a
// write into whatever happens to sit at that address.
func TestRecordFieldsRefuseASaveThatDoesNotReachThem(t *testing.T) {
	short := fixture.Build(fixture.Default())
	if kh3.HasRecords(short) {
		t.Fatal("the short fixture should not reach the record block")
	}
	for _, doc := range []string{
		`{"records":{"minigames":{"verum_rex_high_score":1}}}`,
		`{"records":{"flans":{"grape":{"attempts":1}}}}`,
		`{"records":{"album":{"photo_max_count":1}}}`,
		`{"records":{"attractions":{"0":{"high_score":1}}}}`,
	} {
		_, _, err := kh3.Patch(short, []byte(doc))
		if err == nil {
			t.Errorf("%s was accepted on a save that stops before the block", doc)
			continue
		}
		if !strings.Contains(err.Error(), "record block") {
			t.Errorf("%s failed with %q, which does not say why", doc, err)
		}
	}
	// The use counters live near the front and still work on a short save.
	if _, _, err := kh3.Patch(short, []byte(`{"records":{"attractions":{"0":{"uses":4}}}}`)); err != nil {
		t.Errorf("a use counter should still be writable on a short save: %v", err)
	}
	// And a short save's dump simply has no bests in it.
	d, err := kh3.Dump(short, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(d, &m); err != nil {
		t.Fatal(err)
	}
	recs := m["records"].(map[string]any)
	if _, ok := recs["minigames"]; ok {
		t.Error("a short save's dump should not claim to have minigame records")
	}
	att := recs["attractions"].(map[string]any)["0"].(map[string]any)
	if _, ok := att["high_score"]; ok {
		t.Error("a short save's dump should not claim to have an attraction best")
	}
}
