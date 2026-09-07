// diff compares two saves byte for byte. It is how every offset in this program
// was found, so it stays a subcommand rather than a script somebody rewrites.

package main

import (
	"fmt"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

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
