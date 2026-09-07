// The subcommands that change a save: swap, grant-abilities and patch. Each
// one ends at commit, which is the only way anything here reaches the disk.

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

func parseDifficulty(s string) (byte, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if n, err := strconv.Atoi(t); err == nil {
		if _, ok := kh3.Difficulties[byte(n)]; ok {
			return byte(n), nil
		}
	}
	for v, name := range kh3.Difficulties {
		if strings.ToLower(name) == t {
			return v, nil
		}
	}
	if t == "normal" {
		return 1, nil
	}
	return 0, fmt.Errorf("unknown difficulty %q; use 0-3 or Beginner/Standard/Proud/Critical", s)
}

func cmdSwap(args []string) error {
	var account, diff, outDir string
	f := fs("swap", &account)
	f.StringVar(&diff, "d", "", "0-3 or Beginner/Standard/Proud/Critical")
	f.StringVar(&outDir, "o", "", "write here instead of editing in place")
	dry := f.Bool("n", false, "show what would change, write nothing")
	noHP := f.Bool("no-scale-hp", false, "leave current HP/MP alone")
	bonuses := f.Bool("scale-bonuses", false, "also scale bonus_hp/bonus_mp (see README)")
	grant := f.Bool("grant-start-items", false, "entering Critical, add the Soldier's Earring")
	revoke := f.Bool("revoke-start-items", false, "leaving Critical, take back an unequipped one")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	if diff == "" {
		return fmt.Errorf("swap needs -d <difficulty>")
	}
	target, err := parseDifficulty(diff)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	opt := kh3.SwapOptions{
		ScaleHP: !*noHP, ScaleBonuses: *bonuses,
		GrantStartItems: *grant, RevokeStartItems: *revoke,
	}
	return eachSave(paths, account, func(l *kh3.Save) error {
		if skipSystemFile(l, "system file, no difficulty field") {
			return nil
		}
		// Already on the target difficulty is usually nothing to do, but the
		// start-item flags can still have work: a Critical save missing the
		// earring it should have started with.
		if kh3.GetDifficulty(l.Plain) == target && !opt.GrantStartItems {
			fmt.Printf("skip  %s  already %s\n", show(l.Path), kh3.Difficulties[target])
			return nil
		}
		newPlain, changes, err := kh3.SwapDifficulty(l.Plain, target, opt)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Printf("skip  %s  nothing to change\n", show(l.Path))
			return nil
		}
		fmt.Printf("\n%s\n", show(l.Path))
		for _, c := range changes {
			if strings.HasPrefix(c, " ") {
				fmt.Println(c)
			} else {
				fmt.Println(" ", c)
			}
		}
		return commit(l, newPlain, outDir, *dry)
	})
}

type intList []int

func (l *intList) String() string { return "" }

func (l *intList) Set(v string) error {
	n, err := strconv.ParseInt(v, 0, 32)
	if err != nil {
		return err
	}
	*l = append(*l, int(n))
	return nil
}

func cmdGrantAbilities(args []string) error {
	var account, outDir, character string
	f := fs("grant-abilities", &account)
	f.StringVar(&outDir, "o", "", "write here instead of editing in place")
	f.StringVar(&character, "character", "Sora", "which character")
	crit := f.Bool("critical", false, "the three Critical-mode abilities")
	revoke := f.Bool("revoke", false, "remove instead of grant")
	dry := f.Bool("n", false, "show what would change, write nothing")
	var ids intList
	f.Var(&ids, "ability", "ability id, e.g. 0x068 (repeatable)")
	paths, err := savePaths(f, args)
	if err != nil {
		return err
	}
	var want []int
	if *crit {
		want = append(want, kh3.CriticalAbilities[:]...)
	}
	want = append(want, ids...)
	if len(want) == 0 {
		return fmt.Errorf("nothing to do: pass -critical and/or -ability ID")
	}
	ci := kh3.CharIndex(character)
	if ci < 0 {
		return fmt.Errorf("unknown character %q", character)
	}
	return eachSave(paths, account, func(l *kh3.Save) error {
		if skipSystemFile(l, "system file") {
			return nil
		}
		buf := append([]byte(nil), l.Plain...)
		touched := false
		fmt.Printf("\n%s\n", show(l.Path))
		for _, aid := range want {
			cur := kh3.GetAbility(buf, ci, aid)
			name := kh3.Abilities[aid]
			var target uint32
			if *revoke {
				if cur == kh3.AbilityAbsent {
					fmt.Printf("  %s: not present\n", name)
					continue
				}
				if !kh3.IsInnatelyOwned(cur) {
					fmt.Printf("  %s: granted by equipment (0x%08X), left alone\n", name, cur)
					continue
				}
				target = kh3.AbilityAbsent
			} else {
				if cur&1 != 0 {
					fmt.Printf("  %s: already owned (0x%08X)\n", name, cur)
					continue
				}
				target = kh3.AbilityInnateEquipped
			}
			kh3.SetAbility(buf, ci, aid, target)
			touched = true
			verb := "granted"
			if *revoke {
				verb = "revoked"
			}
			fmt.Printf("  %s 0x%03X %s: %s (0x%08X -> 0x%08X)\n",
				kh3.CharNames[ci], aid, name, verb, cur, target)
		}
		if !touched {
			return nil
		}
		return commit(l, buf, outDir, *dry)
	})
}

func cmdPatch(args []string) error {
	var account, outDir string
	f := fs("patch", &account)
	f.StringVar(&outDir, "o", "", "write here instead of editing in place")
	dry := f.Bool("n", false, "show what would change, write nothing")
	// Not savePaths: the document is the last positional argument, so the
	// saves are everything before it.
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("patch needs <save>... <doc.json>")
	}
	doc, err := os.ReadFile(rest[len(rest)-1])
	if err != nil {
		return err
	}
	paths, err := expand(rest[:len(rest)-1])
	if err != nil {
		return err
	}
	return eachSave(paths, account, func(l *kh3.Save) error {
		newPlain, changes, err := kh3.Patch(l.Plain, doc)
		if err != nil {
			return err
		}
		fmt.Printf("\n%s\n", show(l.Path))
		if len(changes) == 0 {
			fmt.Println("  no changes")
			return nil
		}
		for _, c := range changes {
			fmt.Println(" ", c)
		}
		return commit(l, newPlain, outDir, *dry)
	})
}
