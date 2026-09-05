package gui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// ErrNoPicker means this machine has no folder dialog we know how to drive,
// which is normal on a bare Linux box. The UI falls back to a text field.
var ErrNoPicker = errors.New("no folder picker available")

const pickerPrompt = "Select your KINGDOM HEARTS III save folder"

// A browser cannot hand a real filesystem path to a server, because File
// objects deliberately hide it, so this process opens the dialog instead.
func pickFolder() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("osascript", "-e",
			`POSIX path of (choose folder with prompt "`+pickerPrompt+`")`).Output()
		if err != nil {
			return "", canceled(err)
		}
		return strings.TrimSpace(string(out)), nil

	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '` + pickerPrompt + `'
$d.ShowNewFolderButton = $false
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.SelectedPath }`
		// -STA is required or the dialog never appears.
		out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Output()
		if err != nil {
			return "", canceled(err)
		}
		return strings.TrimSpace(string(out)), nil

	default:
		for _, c := range [][]string{
			{"zenity", "--file-selection", "--directory", "--title=" + pickerPrompt},
			{"kdialog", "--getexistingdirectory", homeOr(".")},
		} {
			if _, err := exec.LookPath(c[0]); err != nil {
				continue
			}
			out, err := exec.Command(c[0], c[1:]...).Output()
			if err != nil {
				return "", canceled(err)
			}
			return strings.TrimSpace(string(out)), nil
		}
		return "", ErrNoPicker
	}
}

func homeOr(fallback string) string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return fallback
}

// canceled turns "the user pressed Cancel" into an empty selection rather
// than an error the UI would show as a failure.
func canceled(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return nil
	}
	return err
}

// pickerAvailable reports whether pickFolder can do anything here.
func pickerAvailable() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	default:
		for _, c := range []string{"zenity", "kdialog"} {
			if _, err := exec.LookPath(c); err == nil {
				return true
			}
		}
		return false
	}
}

// resolveSaveDir accepts any folder at or above a save directory and finds the
// data folder inside it, so pointing at "KINGDOM HEARTS III" works as well as
// pointing at the exact .../kh3sv2/data. A backup .zip resolves to itself and
// stands in for the folder it holds.
func resolveSaveDir(input string) (string, error) {
	p, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if kh3.IsArchive(p) {
		saves, err := kh3.ListArchiveSaves(p)
		if err != nil {
			return "", err
		}
		if len(saves) == 0 {
			return "", errors.New("no KHIII_*.bin files inside that archive")
		}
		return p, nil
	}
	if !fi.IsDir() {
		p = filepath.Dir(p)
	}
	if len(kh3.ListSaveFiles(p)) > 0 {
		return p, nil
	}
	// Look downwards for a data folder holding KHIII_*.bin. The real layout is
	//   Documents/KINGDOM HEARTS III/<platform>/<account>/SaveGames/kh3sv2/data
	// which is seven levels below Documents, and picking Documents is the
	// obvious thing to do, so the cap has to clear that with room to spare.
	const maxDepth = 9
	var found string
	depth := strings.Count(p, string(filepath.Separator))
	filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if strings.Count(path, string(filepath.Separator))-depth > maxDepth {
			return filepath.SkipDir
		}
		if len(kh3.ListSaveFiles(path)) > 0 {
			found = path
		}
		return nil
	})
	if found == "" {
		return "", errors.New("no KHIII_*.bin files in that folder or below it")
	}
	return found, nil
}
