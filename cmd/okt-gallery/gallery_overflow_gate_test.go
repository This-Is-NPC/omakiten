package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestGalleryEntriesDeclareMinimums — wave J portão 1. An entry without a
// declared floor is incomplete: there is no honest allowlist for "we have not
// decided how small this may be".
func TestGalleryEntriesDeclareMinimums(t *testing.T) {
	if len(entries()) < 10 {
		t.Fatalf("entries() returned %d — the catalog is empty, the gate is vacuously green", len(entries()))
	}
	for _, e := range entries() {
		if e.minWidth <= 0 || e.minHeight <= 0 {
			t.Errorf("%s: incomplete entry — declare minWidth and minHeight (got %d×%d)",
				e.name, e.minWidth, e.minHeight)
		}
	}
}

// TestGalleryScenariosFitTheirFrame — every scenario paints with vertical
// overflow == 0. A scenario that omits an axis uses the entry's declared
// minimum on that axis. No allowlist: an overflowing scenario is a broken
// scenario, not named debt.
func TestGalleryScenariosFitTheirFrame(t *testing.T) {
	scenarios := 0
	for _, e := range entries() {
		scenarios += checkEntryFits(t, e)
	}
	if scenarios < 50 {
		t.Fatalf("only %d scenarios checked — the gate is vacuously green", scenarios)
	}
}

func checkEntryFits(t *testing.T, e entry) int {
	t.Helper()
	if e.minWidth <= 0 || e.minHeight <= 0 {
		t.Errorf("%s: skip fit check — mins undeclared", e.name)
		return 0
	}
	if len(e.scenarios) == 0 {
		t.Errorf("%s has no scenarios", e.name)
		return 0
	}
	for _, s := range e.scenarios {
		assertScenarioFits(t, e, s)
	}
	return len(e.scenarios)
}

func assertScenarioFits(t *testing.T, e entry, s scenario) {
	t.Helper()
	w, h := scenarioFrameSize(e, s)
	m := openEntry(t, e.name, 200, 80)
	m = m.applyScenario(indexOfScenario(e, s.name))
	m.frameProps = m.writeFrame("width", w)
	m.frameProps = m.writeFrame("height", h)
	m = m.clampFrame()
	_, overflow := m.frameContent()
	if overflow > 0 {
		t.Errorf("%s/%s @ %dx%d: vertical overflow +%d rows (gate is absolute, no allowlist)",
			e.name, s.name, w, h, overflow)
	}
	// The RAW paint, not frameContent's: that one runs every line through
	// fitLine first, so asking it about width is asking the clipper whether it
	// clipped. What has to fit is what the component produced.
	assertScenarioFitsWidth(t, e, s, m.live.View(m.ctx()), m.innerWidth())
}

// assertScenarioFitsWidth is the other axis, and it was missing.
//
// The gate above checked ROWS only. Everything this catalog exists to prove is
// about what happens when a surface gets less WIDTH than it wants, so the axis
// the gate did not measure was the axis the work is about — four components
// were painting past the right edge of their frame with every test green
// (inputline, header, helpoverlay, column, found by eye in review).
//
// Absolute, like its sibling: a row wider than the frame is a broken scenario,
// not named debt.
func assertScenarioFitsWidth(t *testing.T, e entry, s scenario, painted string, inner int) {
	t.Helper()
	if inner <= 0 {
		return
	}
	for i, row := range strings.Split(painted, "\n") {
		if got := lipgloss.Width(row); got > inner {
			t.Errorf("%s/%s @ %d columns: row %d is %d cells, +%d past the frame: %q",
				e.name, s.name, inner, i, got, got-inner, row)
			return // one row is the finding; the rest are the same defect
		}
	}
}

// TestEveryScenarioDeclaresAGeometryItsEntryAllows — a scenario may not ask for
// less than the entry's own declared floor.
//
// This is how the broken helpoverlay scenario got written: 40 columns for an
// overlay whose minWidth is 70. The component was not misbehaving, it was being
// measured outside the contract it declares, and nothing said so.
func TestEveryScenarioDeclaresAGeometryItsEntryAllows(t *testing.T) {
	checked := 0
	for _, e := range entries() {
		for _, s := range e.scenarios {
			if s.width > 0 && s.width < e.minWidth {
				t.Errorf("%s/%s declares width %d, below the entry floor of %d — raise the scenario or lower the floor",
					e.name, s.name, s.width, e.minWidth)
			}
			if s.height > 0 && s.height < e.minHeight {
				t.Errorf("%s/%s declares height %d, below the entry floor of %d",
					e.name, s.name, s.height, e.minHeight)
			}
			checked++
		}
	}
	if checked < 50 {
		t.Fatalf("only %d scenarios checked — the gate is vacuously green", checked)
	}
}

func indexOfScenario(e entry, name string) int {
	for i, s := range e.scenarios {
		if s.name == name {
			return i
		}
	}
	return 0
}
