// Command genfixture writes synthetic Kingdom Hearts III saves for testing.
//
// It replaces the Python generator that produced testdata/golden.json, so
// nothing here needs Python to build a save tree. internal/fixture is a
// byte-identical port, and TestGoldenFixturesStillMatch keeps it that way.
//
// No real save is ever used: one embeds its owner's SteamID64 and their whole
// playthrough.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

func main() {
	layout := flag.Bool("layout", false, "build the full auto-detectable Steam directory tree")
	// A full-size save is the only kind that reaches the record block at the
	// tail, so it is the only kind that exercises the whole editor. It costs
	// 9.3 MB a slot on disk and is not what the golden vectors were built on,
	// which is why it is opt-in and the short save stays the default.
	full := flag.Bool("full", false, "write full-size saves, which carry the tail regions too")
	account := flag.String("account", fixture.Account, "synthetic account id to key the saves to")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: genfixture [-layout] [-full] [-account ID] <outdir>")
		os.Exit(2)
	}

	root := flag.Arg(0)
	if *layout {
		root = filepath.Join(root, "Documents", "KINGDOM HEARTS III", "Steam",
			*account, "SaveGames", "kh3sv2", "data")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		fail(err)
	}
	key, err := kh3.DeriveKey(*account)
	if err != nil {
		fail(err)
	}

	slots := fixture.Slots()
	if *full {
		slots = fixture.FullSlots()
	}
	for slot, plain := range slots {
		blob, err := kh3.Wrap(kh3.PadToBlock(plain), key)
		if err != nil {
			fail(err)
		}
		path := filepath.Join(root, fmt.Sprintf("KHIII_slot%d.bin", slot))
		if err := os.WriteFile(path, blob, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("%s  difficulty %d  %d bytes\n", path, plain[0x14], len(blob))
	}

	if *layout {
		vdf := filepath.Join(root, "..", "..", "..", "steam_autocloud.vdf")
		body := "\"steam_autocloud.vdf\"\n{\n\t\"accountid\"\t\t\"1\"\n}\n"
		if err := os.WriteFile(filepath.Clean(vdf), []byte(body), 0o644); err != nil {
			fail(err)
		}
	}
	fmt.Println("account", *account)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
