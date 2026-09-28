package screenbody_test

import (
	"strings"
	"testing"

	"omakiten/internal/tui/components/screenbody"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
)

// A two-zone body must express PER-ZONE geometry, which is the capability the
// hardcoded Spec denied: New produced one Section with MinRows 1, Weight 1 and
// ScrollItems, so two zones could not differ in any of the three and a screen
// with real zones had to flatten them into one.
//
// This proves the capability with a BODY rather than by migrating a screen —
// deliberately. The two screens on this contract compose zero sections between
// them, so a migration could not exercise a second zone and a green on it would
// say nothing about the multi-zone case.
//
// The assertion is on the arranged RESULT rather than on the Spec that went in,
// because a test that reads back its own input proves only that a struct field
// round-trips. What has to be true is that the ARRANGER treated the zones
// differently, and the row split is where that becomes observable.
func TestTwoZoneBodyExpressesPerZoneGeometry(t *testing.T) {
	kit := screenkit.Kit{Width: 40, Height: 24}
	box := screenlayout.Box{Width: 40, Rows: 20}

	body := screenbody.NewZones(
		screenbody.NewZone(screenlayout.Spec{
			ID: "top", MinRows: 2, Weight: 1, Scroll: screenlayout.ScrollItems,
		}, nil),
		screenbody.NewZone(screenlayout.Spec{
			ID: "bottom", MinRows: 6, Weight: 3, Scroll: screenlayout.ScrollNone,
		}, nil),
	).ComposeZones(kit, box,
		func(screenlayout.Box) []string { return repeat("top", 30) },
		func(screenlayout.Box) []string { return repeat("bottom", 30) },
	)

	result := body.Arrange(kit, box)
	if strings.TrimSpace(result.View) == "" {
		t.Fatal("two-zone arrange produced an empty view; the zones were not arranged at all")
	}

	top := strings.Count(result.View, "top")
	bottom := strings.Count(result.View, "bottom")
	if top == 0 || bottom == 0 {
		t.Fatalf("both zones must paint: top=%d bottom=%d\n%s", top, bottom, result.View)
	}

	// Weight 3 against weight 1, both over-supplied with lines, so the heavier
	// zone must receive strictly more rows. Equality would mean the arranger
	// read one Spec for both — the exact defect this change removes.
	if bottom <= top {
		t.Errorf("per-zone Weight had no effect: weight-3 zone got %d rows, weight-1 zone got %d.\n"+
			"Both zones supplied 30 lines into a 20-row box, so the heavier zone must take more.\n%s",
			bottom, top, result.View)
	}
}

// The single-zone constructor must keep producing exactly the geometry the
// hardcoded Spec produced, or the two migrated screens' goldens move for a
// reason unrelated to their content. This pins the default rather than trusting
// that nobody edits it.
func TestSpecDefaultMatchesTheGeometryNewUsedToInvent(t *testing.T) {
	got := screenbody.Spec("some-id")
	want := screenlayout.Spec{
		ID: "some-id", MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems,
	}
	if got != want {
		t.Errorf("screenbody.Spec drifted from the geometry New hardcoded:\n got %+v\nwant %+v", got, want)
	}
}

func repeat(word string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = word
	}
	return out
}

// The compiler refuses Zone{Spec: ...} now that the field is unexported, which
// closes the reported one-liner. This covers the RESIDUAL that no signature can
// close: a bare Zone{} still constructs inside a caller that has the type, and
// its zero Spec would take the whole box and answer no key. The ID is the
// discriminator because it is never meaningfully empty — every section is keyed
// by it and it is the Body's only focus target — so requiring it rejects an
// OMITTED Spec without forbidding a deliberate Weight 0 or ScrollNone.
func TestNewZonesRejectsAZoneWithNoSpecID(t *testing.T) {
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("NewZones accepted a zone with an empty Spec.ID; the zero-Spec zone takes the " +
				"whole box and answers no key, so accepting it silently is the defect this rejects")
		}
		if msg, ok := got.(string); !ok || !strings.Contains(msg, "no Spec.ID") {
			t.Errorf("panic did not name the cause: %v", got)
		}
	}()
	_ = screenbody.NewZones(screenbody.NewZone(screenlayout.Spec{}, nil))
}

// A deliberate Weight 0 or ScrollNone is a real choice and must still build —
// the fix rejects an OMITTED Spec, not an unusual one.
func TestNewZonesAcceptsDeliberateZeroWeightWhenNamed(t *testing.T) {
	body := screenbody.NewZones(
		screenbody.NewZone(screenlayout.Spec{ID: "named", Weight: 0, Scroll: screenlayout.ScrollNone}, nil),
	)
	if got := body.Width(); got != 0 {
		t.Errorf("fresh body width = %d, want 0", got)
	}
}

// New and NewZones are SEPARATE DOORS to the same zero Spec, and a remedy that
// closes one leaves the other open. This covers the door the unexported field
// does NOT close: New states the Spec explicitly, so no syntax rule can stop
// New(screenlayout.Spec{}, nil) — only a check on the VALUE can.
func TestNewRejectsAnExplicitZeroSpec(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New accepted an explicit zero Spec; omission and explicit-zero reach the " +
				"same arranger state, so closing only the omission route is not a fix")
		}
	}()
	_ = screenbody.New(screenlayout.Spec{}, nil)
}
