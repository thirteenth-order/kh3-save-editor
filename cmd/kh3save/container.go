// Moving a save between containers and accounts: decrypt, encrypt, convert and
// rekey. These work on the wrapper rather than on what is inside it.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

func cmdDecrypt(args []string) error {
	var account, outDir string
	f := fs("decrypt", &account)
	f.StringVar(&outDir, "o", "plain", "output directory")
	withAccount := f.Bool("with-account", false, "show the account id and the time the save was written")
	paths, err := savePaths(f, args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return eachSave(paths, account, func(l *kh3.Save) error {
		dst := filepath.Join(outDir, kh3.Base(l.Path))
		if err := os.WriteFile(dst, l.Plain, 0o644); err != nil {
			return err
		}
		if *withAccount {
			fmt.Printf("%s  ->  %s  (%d bytes, account %s)\n",
				show(l.Path), show(dst), len(l.Plain), l.Account)
		} else {
			fmt.Printf("%s  ->  %s  (%d bytes)\n", show(l.Path), show(dst), len(l.Plain))
		}
		return nil
	})
}

func cmdEncrypt(args []string) error {
	var account, outDir string
	f := fs("encrypt", &account)
	f.StringVar(&outDir, "o", "encrypted", "output directory")
	// Not eachSave: encrypt is handed plaintext, which has no container to
	// open and no account id to discover.
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
	return eachSave(paths, account, func(l *kh3.Save) error {
		blob, err := kh3.Wrap(l.Plain, dstKey)
		if err != nil {
			return err
		}
		// The same self-check every write path here does: read the output back
		// the way its new owner will, and refuse to write if it does not hold.
		if _, err := kh3.Unwrap(blob, dstKey); err != nil {
			return fmt.Errorf("refusing to write %s: rekey self-check failed", show(l.Path))
		}
		dst := filepath.Join(outDir, kh3.Base(l.Path))
		if err := os.WriteFile(dst, blob, 0o644); err != nil {
			return err
		}
		if *withAccount {
			fmt.Printf("%s  %s -> %s  ->  %s\n", show(l.Path), l.Account, to, show(dst))
		} else {
			fmt.Printf("%s  ->  %s\n", show(l.Path), show(dst))
		}
		return nil
	})
}
