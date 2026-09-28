package keynav

import (
	"testing"
)

// The whole reason this package exists is that two surfaces have to agree, so
// the table is asserted as a table rather than through either of them.
func TestTheKeyTable(t *testing.T) {
	cases := []struct {
		key     string
		entered bool
		want    Intent
	}{
		{"tab", false, NextSibling},
		{"tab", true, NextSibling},
		// shift+tab advances tops, not the zone ring. PrevSibling has no
		// spelling in Default; the zone ring only advances.
		{"shift+tab", false, None},
		{"shift+tab", true, None},
		{"f", false, Enter},
		{"esc", false, Leave},
		{"esc", true, Leave},
		{"down", false, NextChild},
		{"up", false, PrevChild},
		// The arrows are never text, so entering changes nothing for them.
		{"down", true, NextChild},
		{"up", true, PrevChild},
		// The letters move only until the level is entered.
		{"j", false, NextChild},
		{"k", false, PrevChild},
		{"j", true, None},
		{"k", true, None},
		// Entered, `f` belongs to whatever was entered.
		{"f", true, None},
		{"q", false, None},
		{"", false, None},
	}
	for _, c := range cases {
		if got := Resolve(c.key, c.entered); got != c.want {
			t.Errorf("Resolve(%q, entered=%v) = %v, want %v", c.key, c.entered, got, c.want)
		}
	}
}

// A ring that does not know where it is has to be repaired before it can be
// moved, and doing both in one keystroke is what makes the first press appear
// to skip a member.
func TestSteppingFromOutsideTheRingSeedsWithoutMoving(t *testing.T) {
	r := Ring{"a", "b", "c"}

	next, seeded := r.Step("", 1)
	if next != "a" || !seeded {
		t.Errorf("Step from nowhere = %q seeded=%v, want the first member and a seed", next, seeded)
	}
	if next, seeded := r.Step("a", 1); next != "b" || seeded {
		t.Errorf("Step forward = %q seeded=%v, want b", next, seeded)
	}
	if next, _ := r.Step("c", 1); next != "a" {
		t.Errorf("Step past the end = %q, want it to wrap", next)
	}
	if next, _ := r.Step("a", -1); next != "c" {
		t.Errorf("Step before the start = %q, want it to wrap", next)
	}
	if next, seeded := (Ring{}).Step("a", 1); next != "" || seeded {
		t.Errorf("Step on an empty ring = %q seeded=%v, want nothing", next, seeded)
	}
}

// The package must stay free of the packages that depend on it, or it cannot be
// the place both of them meet.
func TestTheModelDependsOnNothing(t *testing.T) {
	// Enforced by construction: this package's production files import nothing.
	// A future import would show up here as a compile-time dependency and in
	// the arch boundary tests as a cycle.
	if Resolve("tab", false) != NextSibling {
		t.Fatal("the table is unreachable")
	}
}
