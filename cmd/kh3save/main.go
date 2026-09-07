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

// commands is the dispatch table. It is a table rather than a switch so that
// the set of subcommands is one list: the usage screen above and the thing
// that runs them used to be two, and two lists of the same commands drift.
var commands = map[string]func([]string) error{
	"info":            cmdInfo,
	"verify":          cmdVerify,
	"swap":            cmdSwap,
	"abilities":       cmdAbilities,
	"decrypt":         cmdDecrypt,
	"encrypt":         cmdEncrypt,
	"grant-abilities": cmdGrantAbilities,
	"dump":            cmdDump,
	"patch":           cmdPatch,
	"diff":            cmdDiff,
	"rekey":           cmdRekey,
	"convert":         cmdConvert,
	"schema":          cmdSchema,
	"gui":             cmdGUI,
}

func cmdGUI(args []string) error {
	f := flag.NewFlagSet("gui", flag.ExitOnError)
	noBrowser := f.Bool("no-browser", false, "print the URL instead of opening a browser")
	// Loopback is the default and the safe answer everywhere except inside a
	// container, where the host cannot reach it. $KH3_ADDR is how the image
	// sets it without rewriting the command line.
	addr := f.String("addr", os.Getenv("KH3_ADDR"),
		"listen address (default 127.0.0.1 on a random port; only change this in a container)")
	if err := f.Parse(args); err != nil {
		return err
	}
	return gui.Serve(*noBrowser, *addr)
}

func main() {
	command, rest := parseCommand(os.Args)
	var err error
	switch run, ok := commands[command]; {
	case ok:
		err = run(rest)
	case command == "version" || command == "-v" || command == "--version":
		fmt.Println("kh3save", buildVersion())
	case command == "help" || command == "-h" || command == "--help":
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

// savePaths is the prologue every subcommand shares: parse the flags, which
// may appear before, after or between the positional arguments, then expand
// what is left into the saves it names.
func savePaths(f *flag.FlagSet, args []string) ([]string, error) {
	rest, err := parseArgs(f, args)
	if err != nil {
		return nil, err
	}
	return expand(rest)
}

// eachSave opens each path in turn and hands it over. It stops at the first
// save that will not open, because a command given four saves and unable to
// read the second has not been told what to do about that, and carrying on
// would half-apply an edit across a set.
//
// It is deliberately separate from savePaths: patch takes its document off the
// end of the arguments and convert and encrypt do not open saves at all, so a
// single do-everything helper would have to grow a flag per caller.
func eachSave(paths []string, account string, fn func(*kh3.Save) error) error {
	for _, p := range paths {
		l, err := load(p, account)
		if err != nil {
			return err
		}
		if err := fn(l); err != nil {
			return err
		}
	}
	return nil
}

// skipSystemFile reports whether this is the small system file, saying so
// first. It carries no playthrough, so most commands have nothing to do with
// one and should say that rather than failing or writing nothing in silence.
func skipSystemFile(l *kh3.Save, because string) bool {
	if kh3.IsSlot(l.Plain) {
		return false
	}
	fmt.Printf("skip  %s  (%s)\n", show(l.Path), because)
	return true
}

func fs(name string, account *string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ExitOnError)
	f.StringVar(account, "account", "", "SteamID64, or "+kh3.EpicAccount+" for Epic (auto-detected by default)")
	return f
}
