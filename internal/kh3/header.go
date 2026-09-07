package kh3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

// Header is the mapped part of a decrypted save. Offsets are for format
// version 5.2 and match KHSave.Lib3 (Xeeynamo/KingdomSaveEditor).
type Header struct {
	FileSize        uint32
	VersionMajor    uint16
	VersionMinor    uint16
	Checksum        uint32
	Difficulty      byte
	WorldLogo       byte
	PlaytimeSeconds uint32
	TotalExp        uint32
	Munny           uint32
	MunnyEarned     uint32
	MunnySpent      uint32
	Level           byte
	Location        byte
	SaveIcon        byte
	EnemiesDefeated uint32
	SavesCount      uint16
	SavedAtTicks    int64
	BonusHP         int32
	BonusMP         int32
	BonusStrength   int32
	BonusMagic      int32
	BonusDefense    int32
	MapPath         string
	MapSpawn        string
}

// SavedAtOff is the wall-clock time the game wrote the save: one int64 of
// 100-nanosecond ticks since 0001-01-01, which is UE4's FDateTime and .NET's
// DateTime on the same scale. KHSave.Lib3 calls these two int32s Unknown00058
// and Unknown0005C; they are the low and high halves of one value.
//
// Measured, not guessed: decoded this way all five sample saves land on the
// minute their .bin file was last written, to the millisecond, and every
// sample reads a whole number of milliseconds, which is the resolution
// FDateTime::Now has on Windows. A save that has never been written by the
// game reads 0, which SavedAt reports as the zero Time rather than year 1.
const SavedAtOff = 0x58

// SlotMinLen distinguishes a per-playthrough slot from the small system file.
const SlotMinLen = 0x10000

func IsSlot(p []byte) bool { return len(p) > SlotMinLen }

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func ReadHeader(p []byte) Header {
	h := Header{
		FileSize:     binary.LittleEndian.Uint32(p[0x04:]),
		VersionMajor: binary.LittleEndian.Uint16(p[0x08:]),
		VersionMinor: binary.LittleEndian.Uint16(p[0x0A:]),
		Checksum:     binary.LittleEndian.Uint32(p[0x0C:]),
		Difficulty:   p[0x14],
	}
	if !IsSlot(p) {
		return h
	}
	h.WorldLogo = p[0x18]
	h.PlaytimeSeconds = binary.LittleEndian.Uint32(p[0x20:])
	h.TotalExp = binary.LittleEndian.Uint32(p[0x24:])
	h.Munny = binary.LittleEndian.Uint32(p[0x28:])
	h.Level = p[0x2C]
	h.Location = p[0x54]
	h.SaveIcon = p[0x60]
	h.EnemiesDefeated = binary.LittleEndian.Uint32(p[0x70:])
	h.SavesCount = binary.LittleEndian.Uint16(p[0x5B8:])
	h.SavedAtTicks = int64(binary.LittleEndian.Uint64(p[SavedAtOff:]))
	h.MunnyEarned = binary.LittleEndian.Uint32(p[MunnyEarnedOff:])
	h.MunnySpent = binary.LittleEndian.Uint32(p[MunnySpentOff:])
	h.BonusHP = int32(binary.LittleEndian.Uint32(p[0xB49C:]))
	h.BonusMP = int32(binary.LittleEndian.Uint32(p[0xB4A0:]))
	h.BonusStrength = int32(binary.LittleEndian.Uint32(p[0xB4A4:]))
	h.BonusMagic = int32(binary.LittleEndian.Uint32(p[0xB4A8:]))
	h.BonusDefense = int32(binary.LittleEndian.Uint32(p[0xB4AC:]))
	h.MapPath = cstr(p[0xBBA0:0xBCA0])
	h.MapSpawn = cstr(p[0xBCA0:0xBCE0])
	return h
}

// ticksPerSecond is the FDateTime unit: one tick is 100 nanoseconds.
const ticksPerSecond = 10_000_000

// unixEpochTicks is 1970-01-01T00:00:00Z counted in FDateTime ticks. The
// conversion goes through the Unix epoch rather than adding a Duration to
// 0001-01-01, because two thousand years does not fit in a time.Duration and
// the obvious one-liner silently overflows.
const unixEpochTicks = 621355968000000000

// SavedAt is when the game wrote the save, in UTC. A save that carries no
// timestamp returns the zero Time, so callers can say "unknown" instead of
// printing the year 1.
func (h Header) SavedAt() time.Time {
	if h.SavedAtTicks <= 0 {
		return time.Time{}
	}
	t := h.SavedAtTicks - unixEpochTicks
	sec, frac := t/ticksPerSecond, t%ticksPerSecond
	if frac < 0 { // Go truncates toward zero; time.Unix wants 0 <= nsec < 1e9.
		sec, frac = sec-1, frac+ticksPerSecond
	}
	return time.Unix(sec, frac*100).UTC()
}

// SavedAtString renders SavedAt the way a dump reports it, and "" for a save
// that has none.
func (h Header) SavedAtString() string {
	t := h.SavedAt()
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05.000Z")
}

func (h Header) Playtime() string {
	d := time.Duration(h.PlaytimeSeconds) * time.Second
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}
