package kh3

import "fmt"

// The four NUL-terminated fields the save carries. They are the level the game
// reloads into and the actor it spawns, so they are the difference between a
// save that resumes where you left off and one that drops you somewhere else.
// Every one is a fixed-width field padded with NULs, and writing one clears
// the whole field first so no tail of the old value survives.
type StringField struct {
	Name string
	Off  int
	Len  int
	Note string
}

var StringFields = []StringField{
	{"map_path", 0xBBA0, 0x100, "the level this save reloads into"},
	{"map_spawn", 0xBCA0, 0x40, "the spawn point inside that level"},
	{"player_script", 0xBCE0, 0x100, "reads empty in every sample save"},
	{"player_character", 0xBDE0, 0x100, "reads empty in every sample save"},
}

func StringFieldByName(name string) (StringField, bool) {
	for _, f := range StringFields {
		if f.Name == name {
			return f, true
		}
	}
	return StringField{}, false
}

func GetString(p []byte, f StringField) string { return cstr(p[f.Off : f.Off+f.Len]) }

// SetString writes a fixed-width field. One byte is always kept for the
// terminator, so a value that exactly fills the field is refused rather than
// silently written without one: the game reads until a NUL, and a field with
// no NUL runs into whatever follows it.
func SetString(p []byte, f StringField, s string) error {
	if len(s) >= f.Len {
		return fmt.Errorf("%s is %d bytes, and the field holds %d including its terminator",
			f.Name, len(s), f.Len)
	}
	for i := range p[f.Off : f.Off+f.Len] {
		p[f.Off+i] = 0
	}
	copy(p[f.Off:], s)
	return nil
}

// SetKeychainUpgrade completes the pair whose getter lives in layout.go.
// KeychainUpgradeOff is the one offset in this program with no confirmation,
// so this writes only what a document explicitly asks for and nothing guesses
// at it. See the note on the constant.
func SetKeychainUpgrade(p []byte, i, v int) {
	if i < 0 || i >= KeychainUpgradeCount {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 0xFF {
		v = 0xFF
	}
	p[KeychainUpgradeOff+i] = byte(v)
}
