package kh3

import "encoding/binary"

// The record block: minigame high scores, the Flantastic Seven, and the
// per-shotlock and per-attraction bests. It sits past the point where this
// game build stops matching KHSave.Lib3's SaveKh3u109, so its offset is not
// upstream's and was not taken on faith.
//
// Upstream declares the block at 0x83AF8 for a save whose filesize field is
// 0x94F2F8. This build's filesize field is 0x94F4D8, exactly 0x1E0 larger, and
// the block sits exactly 0x1E0 later. Three independent facts agree on that
// shift, which is why it is mapped here rather than left out:
//
//  1. the file-size fields differ by 0x1E0, so everything past the insertion
//     point moves by that much;
//  2. the five attraction bests land on 0x83D94 and end at 0x83DA8, which is
//     the exact byte where the last live data in the region stops, and the
//     three that read non-zero are the same three whose *use* counters at
//     0x696 are non-zero, across five sample saves, including one with no
//     attraction use at all, where all five bests read zero;
//  3. upstream's PhotoMaxCount, shifted the same 0x1E0, lands on a field
//     reading 200 in every sample save, which is the album limit the game
//     advertises.
//
// Landmarks 2 and 3 sit 0x1284 bytes apart and agree on the same constant, so
// this is a measured offset and not a guess. Only the attraction bests are
// individually confirmed against gameplay: every other field in the block
// reads zero in all five samples, which is what an hour and a half into
// Olympus should look like, since nothing else here is reachable that early.
const (
	// RecordShift is how far this build's tail sits past upstream's.
	RecordShift = 0x1E0

	RecordsOff = 0x83AF8 + RecordShift // 0x83CD8

	// PhotoMaxCountOff is the album limit, and reads 200 everywhere. The album
	// itself is not mapped: it is 98.6% of the file and holds image blobs.
	PhotoMaxCountOff = 0x84E48 + RecordShift // 0x85028
)

// The block's own layout, all offsets relative to RecordsOff.
const (
	recScoresOff  = 0x00 // 11 i32
	recFlanOff    = 0x2C // 7 x 3 i32
	recShotlockHi = 0x80 // 30 i16
	recAttractHi  = 0xBC // 5 i32

	// RecordsSize is where the block ends, which is also where the live data
	// in this region stops.
	RecordsSize = 0xD0

	FlanCount           = 7
	ShotlockHighCount   = 30
	AttractionHighCount = 5
)

// RecordScores names the eleven scalar minigame records, in save order.
var RecordScores = []string{
	"verum_rex_high_score",
	"verum_rex_timer",
	"flash_tracer_1_high_score",
	"flash_tracer_2_high_score",
	"flash_tracer_1_timer",
	"flash_tracer_2_timer",
	"frozen_slider_high_score",
	"frozen_slider_timer",
	"frozen_slider_medals",
	"festival_dance_high_score",
	"festival_dance_longest_chain",
}

// FlanNames are the Flantastic Seven, in save order.
var FlanNames = []string{
	"cherry", "strawberry", "banana", "honeydew", "grape", "watermelon", "orange",
}

// HasRecords reports whether a buffer actually reaches the record block. The
// synthetic saves the tests build are 0x20000 bytes, which is a valid save
// structure and stops long before this, so every caller has to ask.
func HasRecords(p []byte) bool { return len(p) >= RecordsOff+RecordsSize }

// HasPhotoAlbum reports whether a buffer reaches the album header.
func HasPhotoAlbum(p []byte) bool { return len(p) >= PhotoMaxCountOff+4 }

func GetRecordScore(p []byte, i int) int32 {
	return GetI32Array(p, RecordsOff+recScoresOff, len(RecordScores), i)
}

func SetRecordScore(p []byte, i int, v int32) {
	SetI32Array(p, RecordsOff+recScoresOff, len(RecordScores), i, v)
}

// Flan is one Flantastic Seven record. Upstream names two score fields and an
// attempt count; both score fields read zero in every sample, so which is
// which is reported as stored.
type Flan struct {
	HighScore  int32
	HighScore2 int32
	Attempts   int32
}

func GetFlan(p []byte, i int) Flan {
	if i < 0 || i >= FlanCount {
		return Flan{}
	}
	off := RecordsOff + recFlanOff + i*12
	return Flan{
		HighScore:  int32(le32(p[off:])),
		HighScore2: int32(le32(p[off+4:])),
		Attempts:   int32(le32(p[off+8:])),
	}
}

func SetFlan(p []byte, i int, f Flan) {
	if i < 0 || i >= FlanCount {
		return
	}
	off := RecordsOff + recFlanOff + i*12
	putLe32(p[off:], uint32(f.HighScore))
	putLe32(p[off+4:], uint32(f.HighScore2))
	putLe32(p[off+8:], uint32(f.Attempts))
}

// Shotlock bests are signed 16-bit, unlike the use counters at 0x6D0.
func GetShotlockHigh(p []byte, i int) int {
	if i < 0 || i >= ShotlockHighCount {
		return 0
	}
	return int(int16(binary.LittleEndian.Uint16(p[RecordsOff+recShotlockHi+i*2:])))
}

func SetShotlockHigh(p []byte, i, v int) {
	if i < 0 || i >= ShotlockHighCount {
		return
	}
	binary.LittleEndian.PutUint16(p[RecordsOff+recShotlockHi+i*2:], uint16(int16(v)))
}

func GetAttractionHigh(p []byte, i int) int32 {
	return GetI32Array(p, RecordsOff+recAttractHi, AttractionHighCount, i)
}

func SetAttractionHigh(p []byte, i int, v int32) {
	SetI32Array(p, RecordsOff+recAttractHi, AttractionHighCount, i, v)
}

func GetPhotoMaxCount(p []byte) int32 { return int32(le32(p[PhotoMaxCountOff:])) }

func SetPhotoMaxCount(p []byte, v int32) { putLe32(p[PhotoMaxCountOff:], uint32(v)) }

// SetAttractionUse and SetShotlockUse complete the pair whose getters already
// lived in layout.go; the record block makes both halves editable.
func SetAttractionUse(p []byte, id, v int) {
	SetU16Array(p, AttractionUseOff, AttractionUseCount, id, v)
}

func SetShotlockUse(p []byte, id, v int) {
	SetU16Array(p, ShotlockUseOff, ShotlockUseCount, id, v)
}
