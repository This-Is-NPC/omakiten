package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// maxVisibleLineWidth returns the widest rendered line in s, measured in
// display cells after stripping ANSI. The bounded-width regression tests
// assert this never exceeds the model terminal width for the targeted
// screens and data shapes (task #595 SMART success criterion).
func maxVisibleLineWidth(s string) (max int, widest string) {
	for _, line := range strings.Split(s, "\n") {
		if w := lipgloss.Width(line); w > max {
			max = w
			widest = line
		}
	}
	return max, widest
}

// ---------------------------------------------------------------------------
// Shared truncation helper — visible cell width for wide glyphs (criterion 4)
// ---------------------------------------------------------------------------

func TestTruncateTextRespectsVisibleCellWidth(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
	}{
		{"cjk", strings.Repeat("漢", 20), 10},       // each ideograph = 2 cells
		{"emoji", strings.Repeat("🌟", 20), 9},       // each emoji = 2 cells
		{"mixed", "ascii漢字ascii漢字ascii漢字", 12},     // mixed widths
		{"ascii", strings.Repeat("a", 40), 15},      // baseline ascii
		{"already_fits", "漢字", 10},                  // under budget → untouched
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := truncateText(tc.in, tc.max)
			if w := lipgloss.Width(out); w > tc.max {
				t.Fatalf("truncateText(%q, %d) = %q has visible width %d, exceeds the cell budget %d",
					tc.in, tc.max, out, w, tc.max)
			}
		})
	}
}

func TestTruncateTextZeroBudget(t *testing.T) {
	if got := truncateText("漢字", 0); got != "" {
		t.Fatalf("truncateText with max=0 should be empty; got %q", got)
	}
}

// ---------------------------------------------------------------------------
// wrapWords — hard-wraps single overlong tokens by cell width (criterion 1/4)
// ---------------------------------------------------------------------------

func TestWrapWordsHardWrapsOverlongToken(t *testing.T) {
	const width = 20
	// A single unbroken token (a long URL / path) far wider than the column.
	token := strings.Repeat("x", 137)
	lines := wrapWords(token, width, width)
	if len(lines) < 2 {
		t.Fatalf("expected the overlong token to wrap across multiple lines; got %d line(s)", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("wrapWords line %d %q has width %d, exceeds column %d", i, line, w, width)
		}
	}
	// The fragments must reconstruct the original token with no loss.
	if joined := strings.Join(lines, ""); joined != token {
		t.Fatalf("hard-wrapped fragments lost data:\n got %q\nwant %q", joined, token)
	}
}

func TestWrapWordsHardWrapsWideGlyphToken(t *testing.T) {
	const width = 10
	// An unbroken run of wide glyphs (no spaces) — splitting on rune count
	// would still overflow because each glyph is 2 cells.
	token := strings.Repeat("漢", 30)
	lines := wrapWords(token, width, width)
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("wrapWords line %d %q has visible width %d, exceeds column %d", i, line, w, width)
		}
	}
}

// TestWrapWordsHardWrapsFirstLineToFirstWidth guards the first-line sizing
// fix: an overlong token that starts a fresh first line must be hard-wrapped
// against firstWidth, not restWidth. With firstWidth > restWidth, the prior
// code re-read the limit after an internal flush and under-filled the first
// line to restWidth.
func TestWrapWordsHardWrapsFirstLineToFirstWidth(t *testing.T) {
	const firstWidth = 20
	const restWidth = 8
	token := strings.Repeat("z", 60)
	lines := wrapWords(token, firstWidth, restWidth)
	if len(lines) < 2 {
		t.Fatalf("expected the overlong token to wrap across multiple lines; got %d", len(lines))
	}
	// First fragment must fill firstWidth (no spaces, so it should be exactly
	// firstWidth cells), proving it was not sized to restWidth.
	if w := lipgloss.Width(lines[0]); w != firstWidth {
		t.Fatalf("first line %q width %d, want firstWidth %d (under-fill regression)", lines[0], w, firstWidth)
	}
	// Subsequent fragments stay within restWidth.
	for i := 1; i < len(lines); i++ {
		if w := lipgloss.Width(lines[i]); w > restWidth {
			t.Fatalf("line %d %q width %d exceeds restWidth %d", i, lines[i], w, restWidth)
		}
	}
	if joined := strings.Join(lines, ""); joined != token {
		t.Fatalf("hard-wrapped fragments lost data:\n got %q\nwant %q", joined, token)
	}
}

// TestTruncatePathHeadTruncationRuneSafe guards the head-truncation branch of
// truncatePath against byte-index slicing: a multibyte/CJK leaf directory must
// be cut on a rune boundary, yielding valid UTF-8 within the visible width.
func TestTruncatePathHeadTruncationRuneSafe(t *testing.T) {
	// Leaf is all wide glyphs so even the leaf alone overflows the width,
	// forcing the head-truncate branch ("…" + tail-by-width).
	path := "/home/user/" + strings.Repeat("漢", 30)
	for _, width := range []int{6, 9, 12, 15} {
		out := truncatePath(path, width)
		if !utf8.ValidString(out) {
			t.Fatalf("truncatePath(width=%d) = %q is not valid UTF-8 (byte-slice regression)", width, out)
		}
		if w := lipgloss.Width(out); w > width {
			t.Fatalf("truncatePath(width=%d) = %q has visible width %d, exceeds bound %d", width, out, w, width)
		}
		if !strings.HasPrefix(out, "…") {
			t.Fatalf("truncatePath(width=%d) = %q should keep the head ellipsis", width, out)
		}
	}
}

func TestWrapWordsMixesWrappedTokenWithWords(t *testing.T) {
	const width = 16
	s := "short " + strings.Repeat("y", 50) + " tail"
	lines := wrapWords(s, width, width)
	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d %q exceeds width %d", i, line, w)
		}
	}
	joined := strings.Join(lines, " ")
	if !strings.Contains(joined, "short") || !strings.Contains(joined, "tail") {
		t.Fatalf("surrounding words dropped during hard-wrap:\n%v", lines)
	}
}

// ---------------------------------------------------------------------------
// Tasks > table — overflow from long bucket/priority/unbroken title (crit 1)
// ---------------------------------------------------------------------------

func boundedTableModel(width int, tasks []domain.Task) Model {
	m := Model{
		styles:     newStyles(config.Theme{}),
		width:      width,
		height:     50,
		top:        topTasks,
		sub:        subTable,
		tasks:      tasks,
		priorities: boundedTablePriorities(),
	}
	return m
}

func boundedTablePriorities() []config.PriorityDefinition {
	return []config.PriorityDefinition{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "normal"},
		// A deliberately overlong priority label to stress the priority column.
		{ID: 3, Value: "super-critical-blocker-priority"},
	}
}

func boundedOverflowTasks() []domain.Task {
	return []domain.Task{
		{
			ID:        1,
			Title:     strings.Repeat("unbreakabletitletoken", 12), // long unbroken title
			BucketKey: "an-extremely-long-bucket-key-name",         // long bucket key
			Priority:  domain.Priority(3),                          // long priority label
		},
		{
			ID:        2,
			Title:     "漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字漢字", // wide-glyph title
			BucketKey: "dev",
			Priority:  domain.Priority(2),
		},
	}
}

func TestTableWidePathContainsLongData(t *testing.T) {
	// Wide enough to take the full table path (availableWidth >= 74).
	const width = 100
	m := boundedTableModel(width, boundedOverflowTasks())
	out := m.renderTable()
	got, widest := maxVisibleLineWidth(stripANSI(out))
	if got > width {
		t.Fatalf("table line width %d exceeds terminal width %d:\n%q", got, width, widest)
	}
}

func TestTableCompactPathContainsLongData(t *testing.T) {
	// Narrow enough to take the compact path (availableWidth < 74).
	const width = 60
	m := boundedTableModel(width, boundedOverflowTasks())
	out := m.renderTable()
	got, widest := maxVisibleLineWidth(stripANSI(out))
	if got > width {
		t.Fatalf("compact table line width %d exceeds terminal width %d:\n%q", got, width, widest)
	}
}

func TestTableVeryNarrowContainsLongData(t *testing.T) {
	// Extreme floor: the compact width clamps to a small band; even a long
	// bucket+priority prefix must not push a row past the terminal.
	const width = 40
	m := boundedTableModel(width, boundedOverflowTasks())
	out := m.renderTable()
	got, widest := maxVisibleLineWidth(stripANSI(out))
	if got > width {
		t.Fatalf("very-narrow table line width %d exceeds terminal width %d:\n%q", got, width, widest)
	}
}

// ---------------------------------------------------------------------------
// Home — long names/slugs/paths/tags on a narrow terminal (criterion 2)
// ---------------------------------------------------------------------------

func boundedHomeModel(width int) Model {
	m := Model{
		styles: newStyles(config.Theme{}),
		width:  width,
		height: 50,
		top:    topHome,
	}
	longName := strings.Repeat("LongProjectNameToken", 6)
	m.homeProjects = []domain.Project{
		{
			ID:       1,
			Name:     longName,
			Slug:     "an-extremely-long-project-slug-with-no-spaces",
			RootPath: "/home/user/very/deeply/nested/path/to/a/project/directory/leaf",
		},
	}
	m.homeProjectTags = map[int64][]domain.Tag{
		1: {{ID: 1, Name: "an-extremely-long-unbreakable-tag-label", Label: "an-extremely-long-unbreakable-tag-label"}},
	}
	m.homeProjectPending = map[int64]int{1: 3}
	return m
}

func TestHomeNarrowContainsLongProjectData(t *testing.T) {
	for _, width := range []int{30, 50, 80} {
		m := boundedHomeModel(width)
		out := m.renderHome()
		got, widest := maxVisibleLineWidth(stripANSI(out))
		if got > width {
			t.Fatalf("home line width %d exceeds terminal width %d at terminal=%d:\n%q",
				got, width, width, widest)
		}
	}
}

// ---------------------------------------------------------------------------
// Project view — stacked / narrow layout containment (criterion 3)
// ---------------------------------------------------------------------------

func TestProjectViewStackedContainsPanels(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	// Seed an activity card with a long unbroken body so the activity rail is
	// stressed in the stacked layout.
	m.projectActivity = []domain.Event{
		{ID: 1, EntityType: domain.EventEntityProject, EntityID: 1, EventType: domain.EventTypeComment,
			Body: strings.Repeat("unbreakablebodytoken", 10), AuthorType: "agent"},
	}
	m.activityCardsCache = activityCardsCacheEntry{}

	for _, width := range []int{36, 50, 70} {
		m.width = width
		// Confirm we are below the side-by-side threshold (stacked layout).
		if m.availableWidth() >= m.activityPanelWidth()+projectMetaPanelMinWidth+2 {
			continue // wide enough for side-by-side; not the stacked case
		}
		out := m.renderProjectView()
		got, widest := maxVisibleLineWidth(stripANSI(out))
		if got > width {
			t.Fatalf("stacked project view line width %d exceeds terminal width %d at terminal=%d:\n%q",
				got, width, width, widest)
		}
	}
}
