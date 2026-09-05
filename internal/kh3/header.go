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
	Level           byte
	Location        byte
	SaveIcon        byte
	EnemiesDefeated uint32
	SavesCount      uint16
	BonusHP         int32
	BonusMP         int32
	BonusStrength   int32
	BonusMagic      int32
	BonusDefense    int32
	MapPath         string
	MapSpawn        string
}

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
	h.BonusHP = int32(binary.LittleEndian.Uint32(p[0xB49C:]))
	h.BonusMP = int32(binary.LittleEndian.Uint32(p[0xB4A0:]))
	h.BonusStrength = int32(binary.LittleEndian.Uint32(p[0xB4A4:]))
	h.BonusMagic = int32(binary.LittleEndian.Uint32(p[0xB4A8:]))
	h.BonusDefense = int32(binary.LittleEndian.Uint32(p[0xB4AC:]))
	h.MapPath = cstr(p[0xBBA0:0xBCA0])
	h.MapSpawn = cstr(p[0xBCA0:0xBCE0])
	return h
}

func (h Header) Playtime() string {
	d := time.Duration(h.PlaytimeSeconds) * time.Second
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}
