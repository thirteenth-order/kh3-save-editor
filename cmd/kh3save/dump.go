// dump renders a save as the JSON document patch reads back.

package main

import (
	"fmt"
	"os"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

func cmdDump(args []string) error {
	var account, out string
	f := fs("dump", &account)
	f.StringVar(&out, "o", "", "write here instead of stdout (one save only)")
	chars := f.Int("characters", 3, "how many characters to include")
	withAccount := f.Bool("with-account", false,
		"include the account id and saved_at; both identify your Steam account and when\n"+
			"you were playing, so a dump omits them by default")
	paths, err := savePaths(f, args)
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
	return eachSave(paths, account, func(l *kh3.Save) error {
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
			return nil
		}
		if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s  ->  %s\n", show(l.Path), show(out))
		return nil
	})
}
