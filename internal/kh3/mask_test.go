package kh3

import "strings"

import "testing"

func TestMaskAccount(t *testing.T) {
	const id = "76561190000000000"
	middle := id[6 : len(id)-4]

	got := MaskAccount(id)
	if strings.Contains(got, middle) {
		t.Errorf("MaskAccount leaked the middle of the id: %q", got)
	}
	if !strings.HasPrefix(got, id[:6]) || !strings.HasSuffix(got, id[len(id)-4:]) {
		t.Errorf("MaskAccount = %q, want the first six and last four digits kept", got)
	}
	if len(got) != len(id) {
		t.Errorf("MaskAccount changed the length: %q", got)
	}
	if MaskAccount(id) == MaskAccount("76561190000000001") {
		t.Error("masking collapsed two distinct accounts")
	}
	for _, s := range []string{"", "123", "added"} {
		if MaskAccount(s) != s {
			t.Errorf("MaskAccount(%q) = %q", s, MaskAccount(s))
		}
	}
}

// Masking the id in the interface is pointless if the path beside it spells it
// out, which is how Steam names the directory.
func TestMaskPath(t *testing.T) {
	const id = "76561190000000000"
	in := "/home/u/Documents/KINGDOM HEARTS III/Steam/" + id + "/SaveGames/kh3sv2/data"

	got := MaskPath(in)
	if strings.Contains(got, id) {
		t.Errorf("MaskPath left the id intact: %q", got)
	}
	if !strings.Contains(got, MaskAccount(id)) {
		t.Errorf("MaskPath = %q, want the masked id in place", got)
	}
	// Everything else about the path must survive.
	for _, keep := range []string{"KINGDOM HEARTS III", "SaveGames", "kh3sv2", "data"} {
		if !strings.Contains(got, keep) {
			t.Errorf("MaskPath dropped %q: %q", keep, got)
		}
	}
	// A path with no id is returned unchanged.
	plain := "/tmp/fixture/data/KHIII_slot0.bin"
	if MaskPath(plain) != plain {
		t.Errorf("MaskPath altered a path with no id: %q", MaskPath(plain))
	}
	// Two accounts stay distinguishable after masking.
	a := MaskPath("/s/" + id + "/x")
	b := MaskPath("/s/76561190000000001/x")
	if a == b {
		t.Error("masking collapsed two distinct paths")
	}
}
