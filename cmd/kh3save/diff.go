// diff compares two saves byte for byte. It is how every offset in this program
// was found, so it stays a subcommand rather than a script somebody rewrites.

package main

import (
	"fmt"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// A run is a stretch of bytes where two saves disagree, as a half-open range.
type run struct{ start, end int }

func (r run) len() int { return r.end - r.start }

// diffRuns finds every stretch where a and b disagree, comparing up to the
// length of the shorter one.
//
// It is separate from the printing because it is the part with an answer that
// can be checked: a run that starts one byte early or ends one byte late names
// the wrong offset, and naming offsets is what this command is for.
func diffRuns(a, b []byte) []run {
	var runs []run
	n := min(len(a), len(b))
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
	return runs
}

// leNumber reads a run as the little-endian integer it would be if it were
// one, which is what makes a four-byte difference readable as "1177 -> 1895"
// rather than as two hex blobs.
func leNumber(b []byte) uint64 {
	var v uint64
	for k := len(b) - 1; k >= 0; k-- {
		v = v<<8 | uint64(b[k])
	}
	return v
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
	// A file that is already plaintext is compared as it lies: diff is what
	// gets pointed at decrypt's output, and asking for an account id there
	// would defeat the purpose.
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

	runs := diffRuns(a, b)
	skipped, total := 0, 0
	for _, r := range runs {
		total += r.len()
		if *skipKnown && r.start < 0x10 {
			skipped++
			continue
		}
		if *maxRun > 0 && r.len() > *maxRun {
			skipped++
			continue
		}
		x, y := a[r.start:r.end], b[r.start:r.end]
		extra := ""
		if r.len() <= 4 {
			extra = fmt.Sprintf("   %d -> %d", leNumber(x), leNumber(y))
		}
		fmt.Printf("0x%08x +%-4d  %x -> %x%s\n", r.start, r.len(), x, y, extra)
	}
	fmt.Printf("\n%d differing runs (%d bytes)", len(runs), total)
	if skipped > 0 {
		fmt.Printf(", %d filtered", skipped)
	}
	fmt.Println()
	return nil
}
