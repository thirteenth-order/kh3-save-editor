package kh3_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// The vectors in testdata/golden.json were produced by an independent Python
// implementation of this save format, written from the same reverse
// engineering but not from this code. Every sha256 is of the encrypted save
// the tool should write for that case.
//
// This is what remains of a live cross-implementation differential that ran on
// every commit. The Python side was removed once the Go tool reached parity;
// freezing its output keeps the evidence that two independent implementations
// agreed byte for byte, without the cost of maintaining both. Any change that
// alters an output byte fails here.
type goldenFile struct {
	Account string `json:"account"`
	Fixture struct {
		Slots map[string]string `json:"slots"`
	} `json:"fixture"`
	Cases []goldenCase `json:"cases"`
}

type goldenCase struct {
	Op         string   `json:"op"`
	Slot       int      `json:"slot"`
	Difficulty byte     `json:"difficulty"`
	Flags      []string `json:"flags"`
	Doc        string   `json:"doc"`
	ToAccount  string   `json:"toAccount"`
	SHA256     string   `json:"sha256"`
}

func loadGolden(t *testing.T) goldenFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g goldenFile
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Cases) == 0 {
		t.Fatal("golden.json has no cases")
	}
	return g
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// The vectors are only meaningful if our fixture builder still produces the
// same input the Python one did. Check that before checking anything else.
func TestGoldenFixturesStillMatch(t *testing.T) {
	g := loadGolden(t)
	for slot, plain := range fixture.Slots() {
		want, ok := g.Fixture.Slots[itoa(slot)]
		if !ok {
			t.Fatalf("golden.json has no fixture hash for slot %d", slot)
		}
		if got := sha(plain); got != want {
			t.Fatalf("slot%d fixture drifted from the one the vectors were built on:\n got  %s\n want %s\n"+
				"internal/fixture must stay byte-identical to the generator, or every vector below is meaningless",
				slot, got, want)
		}
	}
}

func itoa(i int) string { return string(rune('0' + i)) }

func TestGoldenVectors(t *testing.T) {
	g := loadGolden(t)
	key, err := kh3.DeriveKey(g.Account)
	if err != nil {
		t.Fatal(err)
	}
	slots := fixture.Slots()

	for _, c := range g.Cases {
		plain, ok := slots[c.Slot]
		if !ok {
			t.Fatalf("case references unknown slot %d", c.Slot)
		}
		name := c.Op + "/slot" + itoa(c.Slot)
		t.Run(name+"/"+describe(c), func(t *testing.T) {
			var out []byte
			outKey := key

			switch c.Op {
			case "identity":
				out = plain
			case "swap":
				opt := kh3.SwapOptions{
					ScaleHP:          !has(c.Flags, "no-scale-hp"),
					ScaleBonuses:     has(c.Flags, "scale-bonuses"),
					GrantStartItems:  has(c.Flags, "grant-start-items"),
					RevokeStartItems: has(c.Flags, "revoke-start-items"),
				}
				var err error
				out, _, err = kh3.SwapDifficulty(plain, c.Difficulty, opt)
				if err != nil {
					t.Fatal(err)
				}
			case "patch":
				var err error
				out, _, err = kh3.Patch(plain, []byte(c.Doc))
				if err != nil {
					t.Fatal(err)
				}
			case "rekey":
				out = plain
				outKey, err = kh3.DeriveKey(c.ToAccount)
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatalf("unknown op %q", c.Op)
			}

			blob, err := kh3.Wrap(out, outKey)
			if err != nil {
				t.Fatal(err)
			}
			if got := sha(blob); got != c.SHA256 {
				t.Errorf("output differs from the independent implementation\n got  %s\n want %s", got, c.SHA256)
			}
		})
	}
}

func has(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func describe(c goldenCase) string {
	switch c.Op {
	case "swap":
		s := "d" + itoa(int(c.Difficulty))
		for _, f := range c.Flags {
			s += "+" + f
		}
		return s
	case "patch":
		return "doc" + itoa(len(c.Doc)%10)
	}
	return "-"
}
