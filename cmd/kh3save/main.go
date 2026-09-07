// Command kh3save is an offline decrypt / inspect / edit tool for
// Kingdom Hearts III PC saves. No running game, no memory hooks.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"github.com/thirteenth-order/kh3-save-editor/internal/gui"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// version is stamped by the release build, which passes
// -ldflags "-X main.version=vX.Y.Z". Nothing else sets it.
var version = "dev"

// buildVersion is what `kh3save version` prints.
//
// `go install github.com/.../cmd/kh3save@latest` is one of the two install
// routes the README offers, and it passes no ldflags, so a binary installed
// that way reported "dev" and gave the one person most likely to be filing a
// bug no way to say which build they were on. The toolchain already records
// the module version it built from, so ask for that when the ldflag is absent.
//
// The release build, the Makefile and the Docker build all stamp the ldflag,
// so none of them reach the fallback. What does reach it is a bare `go build`,
// where Go records a pseudo-version off the commit -- v0.0.0-<date>-<sha>, and
// +dirty for an edited tree. That is longer than "dev" and strictly more
// useful in a bug report, so it is kept rather than flattened back.
// "(devel)" is guarded for anyway: it is what Go records when it has no VCS
// data to work from, and it says less than "dev" does.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Version == "" || bi.Main.Version == "(devel)" {
		return version
	}
	return bi.Main.Version
}

const usage = `kh3save: offline save tools for Kingdom Hearts III (PC)

  kh3save                                  open the browser UI (no arguments)
  kh3save gui       [-no-browser] [-addr]  same, explicitly
  kh3save info      <save|dir>...          show header fields
  kh3save verify    <save|dir>...          check both integrity fields
  kh3save swap      <save>... -d <val>     faithful difficulty change
  kh3save abilities <save>...              per-character ability list
  kh3save decrypt   <save>... -o <dir>     write plaintext
  kh3save encrypt   <plain>... -o <dir>    rewrap plaintext
  kh3save grant-abilities <save>... -critical   abilities on any difficulty
  kh3save dump      <save>... [-o f.json]  render a save as JSON
  kh3save patch     <save>... <doc.json>   apply a JSON document
  kh3save diff      <a> <b>                byte diff of two saves
  kh3save rekey     <save>... -to <id>     move a save between accounts
  kh3save convert   <save>... -to <form>   pc <-> plain (console) container
  kh3save schema    [-json]                every editable field, and its range

Anywhere a <save> or <dir> is accepted, a .zip backup of a KINGDOM HEARTS III
folder works too, and so does one save inside it:


The UI listens on 127.0.0.1 with a fresh random port and a fresh token every
run. -addr (or $KH3_ADDR) overrides that and is meant for containers only.

A save with no Steam wrapper -- what a console save tool hands back, and what
decrypt writes -- is read and written by every command above with no account
id at all. convert moves a save between the two forms, and -to plain writes
the length a console slot actually is, which decrypt does not.

The account id is auto-detected from the save path, and the Epic Games Store
build is keyed to the constant 1638 rather than to a per-user id, so its saves
need no account plumbing either. Override with -account or $KH3_ACCOUNT.

In-place edits are backed up to <file>.bak.<timestamp>; editing a save inside
an archive rewrites the archive, so the backup is of the whole .zip and is
named <archive>.bak.<timestamp>.zip. The game cannot read a zip, so unpack it
before playing.
`

// parseCommand splits a command line into a subcommand and its arguments.
//
// No arguments at all means the binary was double-clicked rather than run from
// a shell, and the person doing that wants the interface, not a usage screen
// in a console window that closes again. So the default subcommand is gui, and
// it goes through exactly the same path as typing it.
func parseCommand(argv []string) (string, []string) {
	if len(argv) < 2 {
		return "gui", nil
	}
	return argv[1], argv[2:]
}

func main() {
	command, rest := parseCommand(os.Args)
	var err error
	switch command {
	case "info":
		err = cmdInfo(rest)
	case "verify":
		err = cmdVerify(rest)
	case "swap":
		err = cmdSwap(rest)
	case "abilities":
		err = cmdAbilities(rest)
	case "decrypt":
		err = cmdDecrypt(rest)
	case "encrypt":
		err = cmdEncrypt(rest)
	case "grant-abilities":
		err = cmdGrantAbilities(rest)
	case "dump":
		err = cmdDump(rest)
	case "patch":
		err = cmdPatch(rest)
	case "diff":
		err = cmdDiff(rest)
	case "rekey":
		err = cmdRekey(rest)
	case "convert":
		err = cmdConvert(rest)
	case "schema":
		err = cmdSchema(rest)
	case "gui":
		f := flag.NewFlagSet("gui", flag.ExitOnError)
		noBrowser := f.Bool("no-browser", false, "print the URL instead of opening a browser")
		// Loopback is the default and the safe answer everywhere except inside
		// a container, where the host cannot reach it. $KH3_ADDR is how the
		// image sets it without rewriting the command line.
		addr := f.String("addr", os.Getenv("KH3_ADDR"),
			"listen address (default 127.0.0.1 on a random port; only change this in a container)")
		if err = f.Parse(rest); err == nil {
			err = gui.Serve(*noBrowser, *addr)
		}
	case "version", "-v", "--version":
		fmt.Println("kh3save", buildVersion())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// expand accepts files, directories or zip archives; directories yield their
// KHIII_*.bin and an archive yields the saves inside it. A directory walk does
// not descend into archives it happens to find: pass the .zip itself, so that
// pointing at a folder holding both a save and a backup of it does not list
// every slot twice.
func expand(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		// Already a single save inside an archive.
		if kh3.InArchive(p) {
			out = append(out, p)
			continue
		}
		if kh3.IsArchive(p) {
			members, err := kh3.ListArchiveSaves(p)
			if err != nil {
				return nil, err
			}
			out = append(out, members...)
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			n := d.Name()
			if strings.HasPrefix(n, "KHIII_") && strings.HasSuffix(n, ".bin") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no KHIII_*.bin files found")
	}
	sort.Strings(out)
	return out, nil
}

// show masks any account id inside a path before a human reads it.
//
// On Steam the save lives under a directory named after the SteamID64, so
// printing the path a command was given publishes the id, and the key derives
// from the id alone. Gating the `account` line behind -with-account while the
// path above it spells the id out in full accomplishes nothing, which is
// exactly what this output used to do.
//
// Mask on the way out and only there. Every read, write and key derivation
// uses the real path; this is for the copy that lands in a terminal, a
// screenshot or a pasted bug report.
func show(p string) string { return kh3.MaskPath(p) }

// load opens a save, adding the one thing the shared reader has no opinion
// about: -account on a file that has no account.
func load(path, account string) (*kh3.Save, error) {
	l, err := kh3.OpenFile(path, account)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", show(path), err)
	}
	if !l.Format.NeedsKey() && account != "" {
		return nil, fmt.Errorf("%s is not encrypted, so -account means nothing here; "+
			"use `kh3save convert -to pc -account %s` to give it one",
			show(path), account)
	}
	return l, nil
}

// commit writes an edited save, or says what it would have written. The seal,
// the self-check and the backup are kh3.Save.Commit's; what is here is the
// reporting around them.
func commit(l *kh3.Save, newPlain []byte, outDir string, dry bool) error {
	if dry {
		// Still seal and read it back: the point of -n is to find out whether
		// this edit produces a save, and skipping the check would answer a
		// different question.
		if _, err := l.SealChecked(newPlain); err != nil {
			return fmt.Errorf("refusing to write %s: %w", show(l.Path), err)
		}
		fmt.Println("  (dry run, nothing written)")
		return nil
	}
	dst := l.Path
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		dst = filepath.Join(outDir, kh3.Base(l.Path))
	}
	backup, err := l.Commit(newPlain, dst)
	if backup != "" {
		// Replacing one member rewrites the whole zip, so for a save inside an
		// archive the backup is of the archive, and says so.
		fmt.Println("  backup ->", show(backup))
	}
	if err != nil {
		return fmt.Errorf("refusing to write %s: %w", show(l.Path), err)
	}
	fmt.Println("  wrote", show(dst))
	return nil
}

// parseArgs parses flags that may appear before, after or between positional
// arguments. Go's flag package stops at the first non-flag, which would make
// "swap save.bin -d Proud" silently drop the -d; people type it both ways.
func parseArgs(f *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := f.Parse(args); err != nil {
			return nil, err
		}
		rest := f.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func fs(name string, account *string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ExitOnError)
	f.StringVar(account, "account", "", "SteamID64, or "+kh3.EpicAccount+" for Epic (auto-detected by default)")
	return f
}

func cmdInfo(args []string) error {
	var account string
	f := fs("info", &account)
	// The account id is a SteamID64 and the key is derived from it alone, so
	// either one identifies the Steam account. This output is what gets pasted
	// into bug reports, so both are opt-in.
	withAccount := f.Bool("with-account", false, "show the account id and the time the save was written")
	showKey := f.Bool("show-key", false, "show the derived AES key (identifies your account)")
	long := f.Bool("l", false, "also show party, equipment, magic, materials and story progress")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		h := kh3.ReadHeader(l.Plain)
		fmt.Printf("\n%s\n", show(p))
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
			continue
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
	}
	return nil
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
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
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
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		if !kh3.IsSlot(l.Plain) {
			fmt.Printf("skip  %s  (system file, no difficulty field)\n", show(p))
			continue
		}
		// Already on the target difficulty is usually nothing to do, but the
		// start-item flags can still have work: a Critical save missing the
		// earring it should have started with.
		if kh3.GetDifficulty(l.Plain) == target && !opt.GrantStartItems {
			fmt.Printf("skip  %s  already %s\n", show(p), kh3.Difficulties[target])
			continue
		}
		newPlain, changes, err := kh3.SwapDifficulty(l.Plain, target, opt)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Printf("skip  %s  nothing to change\n", show(p))
			continue
		}
		fmt.Printf("\n%s\n", show(p))
		for _, c := range changes {
			if strings.HasPrefix(c, " ") {
				fmt.Println(c)
			} else {
				fmt.Println(" ", c)
			}
		}
		if err := commit(l, newPlain, outDir, *dry); err != nil {
			return err
		}
	}
	return nil
}

func cmdAbilities(args []string) error {
	var account string
	f := fs("abilities", &account)
	all := f.Bool("all-characters", false, "include every character, not just the party")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		if !kh3.IsSlot(l.Plain) {
			continue
		}
		fmt.Printf("\n%s   difficulty %s\n", show(p), kh3.Difficulties[kh3.GetDifficulty(l.Plain)])
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
	}
	return nil
}

func cmdDecrypt(args []string) error {
	var account, outDir string
	f := fs("decrypt", &account)
	f.StringVar(&outDir, "o", "plain", "output directory")
	withAccount := f.Bool("with-account", false, "show the account id and the time the save was written")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, kh3.Base(p))
		if err := os.WriteFile(dst, l.Plain, 0o644); err != nil {
			return err
		}
		if *withAccount {
			fmt.Printf("%s  ->  %s  (%d bytes, account %s)\n", show(p), show(dst), len(l.Plain), l.Account)
		} else {
			fmt.Printf("%s  ->  %s  (%d bytes)\n", show(p), show(dst), len(l.Plain))
		}
	}
	return nil
}

func cmdEncrypt(args []string) error {
	var account, outDir string
	f := fs("encrypt", &account)
	f.StringVar(&outDir, "o", "encrypted", "output directory")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	if account == "" {
		account = os.Getenv("KH3_ACCOUNT")
	}
	if account == "" {
		return fmt.Errorf("encrypt needs -account <SteamID64> (plaintext carries no account id)")
	}
	key, err := kh3.DeriveKey(account)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, p := range rest {
		plain, err := kh3.ReadFile(p)
		if err != nil {
			return err
		}
		blob, err := kh3.Wrap(plain, key)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, kh3.Base(p))
		if err := os.WriteFile(dst, blob, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s  ->  %s  (%d bytes)\n", show(p), show(dst), len(blob))
	}
	return nil
}

// cmdConvert moves a save between the Steam container and the bare structure.
//
// This is the bridge to every non-Steam copy of the game: a console save tool
// opens its own container and hands back the structure with nothing around it,
// which is exactly FormatPlain. Going the other way gives that structure a
// Steam wrapper keyed to whichever account is going to load it.
func cmdConvert(args []string) error {
	var account, to, outDir string
	f := fs("convert", &account)
	f.StringVar(&to, "to", "", "target form: pc (Steam-encrypted) or plain")
	f.StringVar(&outDir, "o", "converted", "output directory")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}

	var want kh3.Format
	switch strings.ToLower(to) {
	case "pc", "steam", "encrypted":
		want = kh3.FormatSteam
	case "plain", "decrypted", "ps4", "console":
		want = kh3.FormatPlain
	case "":
		return fmt.Errorf("convert needs -to pc or -to plain")
	default:
		return fmt.Errorf("unknown target form %q: use pc or plain", to)
	}

	if want == kh3.FormatSteam && account == "" {
		account = os.Getenv("KH3_ACCOUNT")
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	for _, p := range paths {
		blob, err := kh3.ReadFile(p)
		if err != nil {
			return err
		}
		have := kh3.DetectFormat(blob)

		// Reading needs the source's key; writing needs the destination's.
		var readKey, writeKey []byte
		if have.NeedsKey() {
			if _, readKey, err = kh3.ResolveAccount(p, blob, account); err != nil {
				return err
			}
		}
		if want.NeedsKey() {
			if account == "" {
				return fmt.Errorf("converting to pc needs -account <SteamID64>: " +
					"the key is derived from it and a plain save carries no id")
			}
			if writeKey, err = kh3.DeriveKey(account); err != nil {
				return err
			}
		}

		plain, _, err := kh3.Open(blob, readKey)
		if err != nil {
			return fmt.Errorf("%s: %w", show(p), err)
		}
		// A save that never went through AES need not be block aligned, and
		// encrypting one that is not would drop its last partial block. Going
		// the other way, drop that alignment again: a console slot is exactly
		// 0x10+filesize, and the console-side tools rebuild the CRC to
		// end-of-file, so a tail they did not expect becomes a bad checksum.
		if want.NeedsKey() {
			plain = kh3.PadToBlock(plain)
		} else {
			plain = kh3.TrimToFileSize(plain)
		}

		out, err := kh3.Seal(plain, want, writeKey)
		if err != nil {
			return err
		}
		// Same self-check every write path here does: read our own output back
		// the way the consumer will, and refuse to write if it does not hold.
		if _, _, err := kh3.Open(out, writeKey); err != nil {
			return fmt.Errorf("refusing to write %s: %w", show(p), err)
		}

		dst := filepath.Join(outDir, kh3.Base(p))
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s  ->  %s  (%s -> %s, %d bytes)\n", show(p), show(dst), have, want, len(out))
	}
	return nil
}

func cmdRekey(args []string) error {
	var account, to, outDir string
	f := fs("rekey", &account)
	f.StringVar(&to, "to", "", "destination SteamID64")
	f.StringVar(&outDir, "o", "rekeyed", "output directory")
	withAccount := f.Bool("with-account", false, "show the account ids and the time the save was written")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	if to == "" {
		return fmt.Errorf("rekey needs -to <SteamID64>")
	}
	dstKey, err := kh3.DeriveKey(to)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		blob, err := kh3.Wrap(l.Plain, dstKey)
		if err != nil {
			return err
		}
		if _, err := kh3.Unwrap(blob, dstKey); err != nil {
			return fmt.Errorf("refusing to write %s: rekey self-check failed", show(p))
		}
		dst := filepath.Join(outDir, kh3.Base(p))
		if err := os.WriteFile(dst, blob, 0o644); err != nil {
			return err
		}
		if *withAccount {
			fmt.Printf("%s  %s -> %s  ->  %s\n", show(p), l.Account, to, show(dst))
		} else {
			fmt.Printf("%s  ->  %s\n", show(p), show(dst))
		}
	}
	return nil
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
	rest, err := parseArgs(f, args)
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
	ci := -1
	for i, n := range kh3.CharNames {
		if n == character {
			ci = i
		}
	}
	if ci < 0 {
		return fmt.Errorf("unknown character %q", character)
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		if !kh3.IsSlot(l.Plain) {
			fmt.Printf("skip  %s  (system file)\n", show(p))
			continue
		}
		buf := append([]byte(nil), l.Plain...)
		touched := false
		fmt.Printf("\n%s\n", show(p))
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
		if touched {
			if err := commit(l, buf, outDir, *dry); err != nil {
				return err
			}
		}
	}
	return nil
}

func cmdDump(args []string) error {
	var account, out string
	f := fs("dump", &account)
	f.StringVar(&out, "o", "", "write here instead of stdout (one save only)")
	chars := f.Int("characters", 3, "how many characters to include")
	withAccount := f.Bool("with-account", false,
		"include the account id and saved_at; both identify your Steam account and when\n"+
			"you were playing, so a dump omits them by default")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	paths, err := expand(rest)
	if err != nil {
		return err
	}
	// -o names one file, not an output directory the way it does everywhere
	// else here, so more than one save means each dump overwriting the last.
	// That used to happen in silence, printing a confident "-> f.json" per save
	// while only the final one survived. One save expands to many easily: a
	// directory or a .zip is an accepted argument.
	if out != "" && len(paths) > 1 {
		return fmt.Errorf("-o writes a single file, but that names %d saves; "+
			"dump them one at a time, or drop -o and redirect stdout", len(paths))
	}
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		// The account id is a SteamID64. A dump gets pasted into issues and
		// forum posts, so it is left out unless asked for; patch never reads it.
		acct := ""
		if *withAccount {
			acct = l.Account
		}
		data, err := kh3.Dump(l.Plain, acct, *chars)
		if err != nil {
			return err
		}
		if out == "" {
			fmt.Println(string(data))
			continue
		}
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s  ->  %s\n", show(p), show(out))
	}
	return nil
}

func cmdPatch(args []string) error {
	var account, outDir string
	f := fs("patch", &account)
	f.StringVar(&outDir, "o", "", "write here instead of editing in place")
	dry := f.Bool("n", false, "show what would change, write nothing")
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
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		newPlain, changes, err := kh3.Patch(l.Plain, doc)
		if err != nil {
			return err
		}
		fmt.Printf("\n%s\n", show(p))
		if len(changes) == 0 {
			fmt.Println("  no changes")
			continue
		}
		for _, c := range changes {
			fmt.Println(" ", c)
		}
		if err := commit(l, newPlain, outDir, *dry); err != nil {
			return err
		}
	}
	return nil
}

func cmdDiff(args []string) error {
	var account string
	f := fs("diff", &account)
	maxRun := f.Int("max-run", 64, "hide runs longer than this (0 = show all)")
	skipKnown := f.Bool("skip-known", false, "hide the size/version/CRC header at 0x00-0x0F")
	rest, err := parseArgs(f, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("diff needs exactly two files")
	}
	asPlain := func(path string) ([]byte, error) {
		blob, err := kh3.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(blob) >= 4 && string(blob[:4]) == string(kh3.Magic) {
			return blob, nil
		}
		l, err := load(path, account)
		if err != nil {
			return nil, err
		}
		return l.Plain, nil
	}
	a, err := asPlain(rest[0])
	if err != nil {
		return err
	}
	b, err := asPlain(rest[1])
	if err != nil {
		return err
	}
	if len(a) != len(b) {
		fmt.Printf("note: lengths differ (%d vs %d)\n", len(a), len(b))
	}
	n := min(len(a), len(b))
	type run struct{ start, end int }
	var runs []run
	for i := 0; i < n; {
		if a[i] != b[i] {
			s := i
			for i < n && a[i] != b[i] {
				i++
			}
			runs = append(runs, run{s, i})
		} else {
			i++
		}
	}
	skipped, total := 0, 0
	for _, r := range runs {
		total += r.end - r.start
		if *skipKnown && r.start < 0x10 {
			skipped++
			continue
		}
		if *maxRun > 0 && r.end-r.start > *maxRun {
			skipped++
			continue
		}
		x, y := a[r.start:r.end], b[r.start:r.end]
		extra := ""
		if r.end-r.start <= 4 {
			var xi, yi uint64
			for k := len(x) - 1; k >= 0; k-- {
				xi = xi<<8 | uint64(x[k])
				yi = yi<<8 | uint64(y[k])
			}
			extra = fmt.Sprintf("   %d -> %d", xi, yi)
		}
		fmt.Printf("0x%08x +%-4d  %x -> %x%s\n", r.start, r.end-r.start, x, y, extra)
	}
	fmt.Printf("\n%d differing runs (%d bytes)", len(runs), total)
	if skipped > 0 {
		fmt.Printf(", %d filtered", skipped)
	}
	fmt.Println()
	return nil
}
