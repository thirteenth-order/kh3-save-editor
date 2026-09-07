// Reading a save and printing what is in it: info and its long form, verify,
// and the per-character ability list. Nothing here writes.

package main

import (
	"fmt"
	"strings"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

func cmdInfo(args []string) error {
	var account string
	f := fs("info", &account)
	// The account id is a SteamID64 and the key is derived from it alone, so
	// either one identifies the Steam account. This output is what gets pasted
	// into bug reports, so both are opt-in.
	withAccount := f.Bool("with-account", false, "show the account id and the time the save was written")
	showKey := f.Bool("show-key", false, "show the derived AES key (identifies your account)")
	long := f.Bool("l", false, "also show party, equipment, magic, materials and story progress")
	paths, err := savePaths(f, args)
	if err != nil {
		return err
	}
	return eachSave(paths, account, func(l *kh3.Save) error {
		h := kh3.ReadHeader(l.Plain)
		fmt.Printf("\n%s\n", show(l.Path))
		if l.Format == kh3.FormatPlain {
			fmt.Println("  container    plain (no Steam wrapper, no account id)")
		}
		if *withAccount {
			fmt.Printf("  account      %s\n", l.Account)
		}
		if *showKey {
			fmt.Printf("  key          %q\n", l.Key)
		}
		fmt.Printf("  version      %d.%d   filesize 0x%X   plaintext %d bytes\n",
			h.VersionMajor, h.VersionMinor, h.FileSize, len(l.Plain))
		if !kh3.IsSlot(l.Plain) {
			fmt.Println("  (system/config file, no per-playthrough fields)")
			return nil
		}
		fmt.Printf("  difficulty   %d (%s)\n", h.Difficulty, kh3.Difficulties[h.Difficulty])
		fmt.Printf("  level        %d    playtime %s    munny %d    exp %d\n",
			h.Level, h.Playtime(), h.Munny, h.TotalExp)
		fmt.Printf("  munny ledger earned %d   spent %d\n", h.MunnyEarned, h.MunnySpent)
		fmt.Printf("  world        %s\n", kh3.WorldName(int(h.WorldLogo)))
		fmt.Printf("  location     %d (%s)\n", h.Location, kh3.LocationName(int(h.Location)))
		fmt.Printf("  saves %d   enemies defeated %d   crabs %d\n",
			h.SavesCount, h.EnemiesDefeated, kh3.GetCrabs(l.Plain))
		// Same opt-in as the account: a wall-clock write time says when
		// somebody was playing, and this output gets pasted into bug reports.
		if t := h.SavedAtString(); t != "" && *withAccount {
			fmt.Printf("  written      %s\n", t)
		}
		fmt.Printf("  bonuses      hp %d mp %d str %d mag %d def %d\n",
			h.BonusHP, h.BonusMP, h.BonusStrength, h.BonusMagic, h.BonusDefense)
		fmt.Printf("  map          %s  @ %s\n", h.MapPath, h.MapSpawn)
		if *long {
			printLong(l.Plain)
		}
		return nil
	})
}

// printLong renders the regions that are interesting to read but too bulky for
// the default output. Everything here is also in dump, in a form patch reads.
func printLong(p []byte) {
	var party []string
	for _, id := range kh3.GetParty(p) {
		if id != 0 {
			party = append(party, kh3.PartyName(id))
		}
	}
	if len(party) > 0 {
		fmt.Printf("  party        %s\n", strings.Join(party, ", "))
	}

	fmt.Printf("  magic        %s\n", commandList(p, kh3.GetMagic, kh3.MagicCount))
	fmt.Printf("  links        %s\n", commandList(p, kh3.GetLink, kh3.LinkCount))

	for page := 0; page < kh3.ShortcutPages; page++ {
		var set []string
		for b, bname := range kh3.ShortcutButtonNames {
			if cmd := kh3.GetShortcut(p, page, b); cmd != 0 {
				set = append(set, bname+" "+kh3.CommandName(cmd))
			}
		}
		if len(set) > 0 {
			fmt.Printf("  shortcuts %d  %s\n", page, strings.Join(set, ", "))
		}
	}

	var mats []string
	for id := 0; id < kh3.MaterialCount; id++ {
		if n := kh3.GetMaterial(p, id); n > 0 {
			mats = append(mats, fmt.Sprintf("%s x%d", kh3.MaterialName(id), n))
		}
	}
	if len(mats) > 0 {
		fmt.Printf("  materials    %s\n", strings.Join(mats, ", "))
	}

	var flags []string
	for id := 0; id < kh3.StoryFlagCount; id++ {
		if v := kh3.GetStoryFlag(p, id); v != 0 {
			flags = append(flags, fmt.Sprintf("%s %d", kh3.StoryFlagName(id), v))
		}
	}
	if len(flags) > 0 {
		fmt.Printf("  story        %s\n", strings.Join(flags, ", "))
	}

	printRecords(p)

	var chains []string
	for i := 0; i < kh3.KeychainUpgradeCount; i++ {
		if v := kh3.GetKeychainUpgrade(p, i); v != 0 {
			chains = append(chains, fmt.Sprintf("%d:%d", i, v))
		}
	}
	if len(chains) > 0 {
		fmt.Printf("  keychains    %s\n", strings.Join(chains, ", "))
	}

	// map_path and map_spawn are already in the default output; the other two
	// read empty in every sample save, so they only appear when they are not.
	for _, sf := range kh3.StringFields[2:] {
		if v := kh3.GetString(p, sf); v != "" {
			fmt.Printf("  %-12s %s\n", sf.Name, v)
		}
	}

	for ci := range kh3.CharNames {
		var worn []string
		for _, k := range kh3.EquipKinds {
			for s := 0; s < k.Slots; s++ {
				if e := kh3.GetEquip(p, ci, k.Off, s); !e.Empty() {
					worn = append(worn, e.Name())
				}
			}
		}
		if len(worn) == 0 {
			continue
		}
		fmt.Printf("  %-12s hp %d mp %d  %s\n", kh3.CharNames[ci],
			kh3.GetStat(p, ci, kh3.StatHP), kh3.GetStat(p, ci, kh3.StatMP),
			strings.Join(worn, ", "))
	}
}

// printRecords shows the use counters every save carries and, where the save
// is long enough to hold them, the bests that go with them. A save that stops
// before that block says nothing rather than printing a screen of zeros.
func printRecords(p []byte) {
	full := kh3.HasRecords(p)

	for _, r := range []struct {
		label string
		count int
		names map[int]string
		uses  func([]byte, int) int
		best  func([]byte, int) int
	}{
		{"attractions", kh3.AttractionUseCount, kh3.RecordAttractions, kh3.GetAttractionUse,
			func(p []byte, i int) int { return int(kh3.GetAttractionHigh(p, i)) }},
		{"shotlocks", kh3.ShotlockUseCount, kh3.RecordShotlocks, kh3.GetShotlockUse,
			kh3.GetShotlockHigh},
	} {
		var out []string
		for id := 0; id < r.count; id++ {
			n := r.uses(p, id)
			if n == 0 {
				continue
			}
			line := fmt.Sprintf("%s x%d", lookupName(r.names, id), n)
			if full {
				if best := r.best(p, id); best != 0 {
					line += fmt.Sprintf(" (best %d)", best)
				}
			}
			out = append(out, line)
		}
		if len(out) > 0 {
			fmt.Printf("  %-12s %s\n", r.label, strings.Join(out, ", "))
		}
	}

	if !full {
		return
	}
	var mini []string
	for i, name := range kh3.RecordScores {
		if v := kh3.GetRecordScore(p, i); v != 0 {
			mini = append(mini, fmt.Sprintf("%s %d", name, v))
		}
	}
	if len(mini) > 0 {
		fmt.Printf("  minigames    %s\n", strings.Join(mini, ", "))
	}
	var flans []string
	for i, name := range kh3.FlanNames {
		f := kh3.GetFlan(p, i)
		if f == (kh3.Flan{}) {
			continue
		}
		flans = append(flans, fmt.Sprintf("%s %d/%d in %d", name, f.HighScore, f.HighScore2, f.Attempts))
	}
	if len(flans) > 0 {
		fmt.Printf("  flans        %s\n", strings.Join(flans, ", "))
	}
	if n := kh3.GetPhotoMaxCount(p); n != 0 {
		fmt.Printf("  album        holds %d photos\n", n)
	}
}

func lookupName(t map[int]string, id int) string {
	if n, ok := t[id]; ok {
		return n
	}
	return fmt.Sprintf("#%d", id)
}

func commandList(p []byte, get func([]byte, int) int, count int) string {
	var out []string
	for i := 0; i < count; i++ {
		if cmd := get(p, i); cmd != 0 {
			out = append(out, kh3.CommandName(cmd))
		}
	}
	if len(out) == 0 {
		return "(none)"
	}
	return strings.Join(out, ", ")
}

func cmdVerify(args []string) error {
	var account string
	f := fs("verify", &account)
	paths, err := savePaths(f, args)
	if err != nil {
		return err
	}
	// This one keeps its own loop on purpose: eachSave stops at the first save
	// that will not open, and reporting which of them are broken is the whole
	// job here.
	bad := 0
	for _, p := range paths {
		if _, err := load(p, account); err != nil {
			bad++
			fmt.Printf("FAIL  %s\n        %v\n", show(p), err)
			continue
		}
		fmt.Printf("OK    %s\n", show(p))
	}
	if bad > 0 {
		return fmt.Errorf("%d file(s) failed", bad)
	}
	return nil
}

func cmdAbilities(args []string) error {
	var account string
	f := fs("abilities", &account)
	all := f.Bool("all-characters", false, "include every character, not just the party")
	paths, err := savePaths(f, args)
	if err != nil {
		return err
	}
	return eachSave(paths, account, func(l *kh3.Save) error {
		// A system file carries no characters, and saying so for each one
		// would bury the output when a whole folder is passed.
		if !kh3.IsSlot(l.Plain) {
			return nil
		}
		fmt.Printf("\n%s   difficulty %s\n", show(l.Path), kh3.Difficulties[kh3.GetDifficulty(l.Plain)])
		n := 3
		if *all {
			n = len(kh3.CharNames)
		}
		for ci := 0; ci < n; ci++ {
			var owned [][2]int
			for aid := 0; aid < kh3.AbilityCount; aid++ {
				if w := kh3.GetAbility(l.Plain, ci, aid); w != kh3.AbilityAbsent {
					owned = append(owned, [2]int{aid, int(w)})
				}
			}
			if len(owned) == 0 && *all {
				continue
			}
			fmt.Printf("  %-12s current HP %d  MP %d   (%d abilities)\n",
				kh3.CharNames[ci], kh3.GetStat(l.Plain, ci, kh3.StatHP),
				kh3.GetStat(l.Plain, ci, kh3.StatMP), len(owned))
			for _, e := range owned {
				mark := ""
				for _, c := range kh3.CriticalAbilities {
					if c == e[0] {
						mark = "  <-- Critical"
					}
				}
				fmt.Printf("      0x%03X  0x%08X  %-24s [%s]%s\n",
					e[0], uint32(e[1]), kh3.Abilities[e[0]], kh3.AbilityCategory[e[0]], mark)
			}
		}
		return nil
	})
}
