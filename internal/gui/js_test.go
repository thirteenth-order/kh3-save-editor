package gui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// The browser half of this program is about twelve hundred lines that no Go
// test reaches: a validator, a JSON editor and a form that draws itself out of
// the published schema. tools/jscheck runs all three outside a browser against
// the same schema this server serves and a dump of the same fixture the format
// tests use, so a change that breaks the page fails here rather than in
// somebody's browser with a blank panel and a console nobody is watching.
//
// It skips when node is not installed. The Go suite has no other need of it,
// and a machine without it should still be able to run the tests.
func TestBrowserLogicHoldsUp(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; skipping the browser-side checks")
	}

	dir := t.TempDir()
	schema, err := json.Marshal(kh3.Describe())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema.json"), schema, 0o644); err != nil {
		t.Fatal(err)
	}
	// The full-size fixture, because a short save carries no record block and
	// the page's handling of one is part of what is being checked.
	detail, err := kh3.Dump(fixture.BuildFull(fixture.Default()), "", kh3.CharCount)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "detail.json"), detail, 0o644); err != nil {
		t.Fatal(err)
	}

	run := filepath.Join("..", "..", "tools", "jscheck", "run.js")
	out, err := exec.Command(node, run, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("the browser-side checks failed:\n%s", out)
	}
	if !strings.Contains(string(out), "all good") {
		t.Fatalf("the checks did not finish:\n%s", out)
	}
	t.Log("\n" + strings.TrimSpace(string(out)))
}
