package kh3_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/thirteenth-order/kh3-save-editor/internal/fixture"
	"github.com/thirteenth-order/kh3-save-editor/internal/kh3"
)

// balancedLedger builds a save whose munny ledger actually adds up, which the
// plain fixture's does not: it sets munny and leaves the pair at zero. Patch
// only maintains an identity that already held, so every case below has to
// start from one that does.
func balancedLedger(t *testing.T, earned, spent uint32) []byte {
	t.Helper()
	p := fixture.Build(fixture.Default())
	binary.LittleEndian.PutUint32(p[kh3.MunnyEarnedOff:], earned)
	binary.LittleEndian.PutUint32(p[kh3.MunnySpentOff:], spent)
	binary.LittleEndian.PutUint32(p[0x28:], earned-spent)
	return p
}

func ledger(p []byte) (munny, earned, spent uint32) {
	h := kh3.ReadHeader(p)
	return h.Munny, h.MunnyEarned, h.MunnySpent
}

// Whichever of the three a document leaves alone is the one Patch recomputes,
// and when it leaves two alone the side opposite the change gives way: munny is
// a balance, munny_spent is history.
func TestPatchKeepsTheMunnyLedgerConsistent(t *testing.T) {
	for _, c := range []struct {
		name                 string
		doc                  string
		munny, earned, spent uint32
	}{
		{"setting munny raises earned and leaves spent alone",
			`{"header":{"munny":9999}}`, 9999, 10599, 600},
		{"setting earned moves the balance",
			`{"header":{"munny_earned":5000}}`, 4400, 5000, 600},
		{"setting spent moves the balance, not what was earned",
			`{"header":{"munny_spent":700}}`, 1195, 1895, 700},
		{"naming munny and spent pins earned",
			`{"header":{"munny":10,"munny_spent":5}}`, 10, 15, 5},
		{"naming munny and earned pins spent",
			`{"header":{"munny":100,"munny_earned":900}}`, 100, 900, 800},
		{"naming earned and spent pins munny",
			`{"header":{"munny_earned":4000,"munny_spent":1000}}`, 3000, 4000, 1000},
		{"naming all three consistently is taken at its word",
			`{"header":{"munny":1,"munny_earned":3,"munny_spent":2}}`, 1, 3, 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, changes, err := kh3.Patch(balancedLedger(t, 1895, 600), []byte(c.doc))
			if err != nil {
				t.Fatal(err)
			}
			m, e, s := ledger(out)
			if m != c.munny || e != c.earned || s != c.spent {
				t.Errorf("ledger is munny %d earned %d spent %d, want %d/%d/%d (changes: %v)",
					m, e, s, c.munny, c.earned, c.spent, changes)
			}
			if e-s != m {
				t.Errorf("ledger does not balance: %d - %d != %d", e, s, m)
			}
		})
	}
}

// A correction to a field the document did not name has to show up in the
// change list, or somebody edits munny and never learns that a second number
// moved with it.
func TestTheLedgerCorrectionIsReported(t *testing.T) {
	_, changes, err := kh3.Patch(balancedLedger(t, 1895, 600), []byte(`{"header":{"munny":9999}}`))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(changes, "\n")
	if !strings.Contains(joined, "header.munny: 1295 -> 9999") {
		t.Errorf("the change the document asked for is not reported:\n%s", joined)
	}
	if !strings.Contains(joined, "header.munny_earned: 1895 -> 10599") {
		t.Errorf("the correction is not reported:\n%s", joined)
	}
}

// Three numbers that cannot all be true leave nothing to solve for, so the
// document is wrong rather than the save.
func TestPatchRejectsAContradictoryMunnyLedger(t *testing.T) {
	doc := `{"header":{"munny":1,"munny_earned":100,"munny_spent":2}}`
	if _, _, err := kh3.Patch(balancedLedger(t, 1895, 600), []byte(doc)); err == nil {
		t.Error("a ledger that does not add up was accepted")
	}
}

// Keeping the identity must never wrap a field around. Spending more than was
// ever earned would put the balance below zero, which a u32 cannot hold.
func TestPatchRefusesALedgerCorrectionThatDoesNotFit(t *testing.T) {
	for _, doc := range []string{
		`{"header":{"munny_spent":5000}}`,               // balance would go negative
		`{"header":{"munny":4294967295}}`,               // earned would overflow
		`{"header":{"munny":9000,"munny_earned":8000}}`, // spent would go negative
	} {
		if _, _, err := kh3.Patch(balancedLedger(t, 1895, 600), []byte(doc)); err == nil {
			t.Errorf("%s was accepted", doc)
		}
	}
}

// The narrow half of the rule: a save whose ledger never added up is left
// exactly as it is, because choosing which of its three numbers to believe
// would be a guess. The plain fixture is that save.
func TestPatchLeavesAnAlreadyBrokenLedgerAlone(t *testing.T) {
	p := fixture.Build(fixture.Default())
	if _, e, s := ledger(p); e != 0 || s != 0 {
		t.Fatalf("the fixture ledger is no longer the unbalanced one this test needs: %d/%d", e, s)
	}
	out, changes, err := kh3.Patch(p, []byte(`{"header":{"munny":777}}`))
	if err != nil {
		t.Fatal(err)
	}
	m, e, s := ledger(out)
	if m != 777 || e != 0 || s != 0 {
		t.Errorf("ledger is %d/%d/%d, want the pair untouched at 0/0", m, e, s)
	}
	if len(changes) != 1 {
		t.Errorf("changes = %v, want only the munny write", changes)
	}
}

// A whole dump patched straight back stays byte-identical even though the
// document names all three fields. This is the case the golden vectors and the
// round-trip test both depend on.
func TestDumpingAndPatchingBackDoesNotDisturbTheLedger(t *testing.T) {
	for _, p := range [][]byte{
		balancedLedger(t, 1895, 600),
		fixture.Build(fixture.Default()), // the unbalanced one
	} {
		doc, err := kh3.Dump(p, "", kh3.CharCount)
		if err != nil {
			t.Fatal(err)
		}
		out, changes, err := kh3.Patch(p, doc)
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 0 {
			t.Errorf("round trip reported %v", changes)
		}
		if !bytes.Equal(out, p) {
			t.Error("round trip changed the save")
		}
	}
}
