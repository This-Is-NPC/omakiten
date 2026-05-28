package palette

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/domain"
)

func upKey() tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyUp} }
func downKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyDown} }
func pgupKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyPgUp} }
func pgdownKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyPgDown} }

func seedSearchModel(hits []domain.SearchHit) Model {
	m := NewModel()
	m, _ = m.Update(keyMsg("tab")) // land on Search tab
	m.SetResults(hits)
	return m
}

func fakeHits(n int) []domain.SearchHit {
	out := make([]domain.SearchHit, n)
	for i := 0; i < n; i++ {
		out[i] = domain.SearchHit{
			EntityType: domain.SearchEntityTask,
			ID:         int64(100 + i),
			Snippet:    fmt.Sprintf("snippet %d", i),
		}
	}
	return out
}

func TestSetResultsStoresHitsAndResetsCursor(t *testing.T) {
	m := NewModel()
	m.SetResults(fakeHits(3))
	if !m.HasResults() {
		t.Fatalf("HasResults false after SetResults")
	}
	if m.ResultsCursor() != 0 {
		t.Fatalf("cursor = %d after SetResults, want 0", m.ResultsCursor())
	}
	got, ok := m.FocusedHit()
	if !ok || got.ID != 100 {
		t.Fatalf("FocusedHit = %v, ok=%v, want id=100", got, ok)
	}
}

func TestClearResultsResetsState(t *testing.T) {
	m := NewModel()
	m.SetResults(fakeHits(3))
	m.ClearResults()
	if m.HasResults() {
		t.Fatalf("HasResults true after ClearResults")
	}
	if _, ok := m.FocusedHit(); ok {
		t.Fatalf("FocusedHit returned ok=true after ClearResults")
	}
}

func TestDownMovesCursorWhileResultsFocused(t *testing.T) {
	m := seedSearchModel(fakeHits(3))
	m, _ = m.Update(downKey())
	if m.ResultsCursor() != 1 {
		t.Fatalf("after down, cursor = %d, want 1", m.ResultsCursor())
	}
	m, _ = m.Update(downKey())
	if m.ResultsCursor() != 2 {
		t.Fatalf("after second down, cursor = %d, want 2", m.ResultsCursor())
	}
}

func TestDownClampsAtLastRow(t *testing.T) {
	m := seedSearchModel(fakeHits(2))
	m, _ = m.Update(downKey())
	m, _ = m.Update(downKey())
	m, _ = m.Update(downKey())
	if m.ResultsCursor() != 1 {
		t.Fatalf("cursor = %d, want 1 (clamped at last row)", m.ResultsCursor())
	}
}

func TestUpClampsAtFirstRow(t *testing.T) {
	m := seedSearchModel(fakeHits(3))
	m, _ = m.Update(upKey())
	if m.ResultsCursor() != 0 {
		t.Fatalf("cursor = %d, want 0 (clamped at first row)", m.ResultsCursor())
	}
}

func TestPgDownMovesByFive(t *testing.T) {
	m := seedSearchModel(fakeHits(10))
	m, _ = m.Update(pgdownKey())
	if m.ResultsCursor() != 5 {
		t.Fatalf("after pgdown, cursor = %d, want 5", m.ResultsCursor())
	}
	m, _ = m.Update(pgupKey())
	if m.ResultsCursor() != 0 {
		t.Fatalf("after pgup, cursor = %d, want 0", m.ResultsCursor())
	}
}

func TestNavigationKeysIgnoredOnTricksTab(t *testing.T) {
	m := NewModel()
	m.SetResults(fakeHits(3))
	// Stay on Tricks tab (NewModel default); down should NOT move cursor.
	m, _ = m.Update(downKey())
	if m.ResultsCursor() != 0 {
		t.Fatalf("down on Tricks tab moved cursor to %d, want 0 (ignored)", m.ResultsCursor())
	}
}

func TestNavigationKeysIgnoredWhenNoResults(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(keyMsg("tab"))
	// down should reach the textinput (which ignores it), not crash.
	m, cmd := m.Update(downKey())
	if cmd != nil {
		if msg := runCmd(cmd); msg != nil {
			if _, ok := msg.(OpenHitMsg); ok {
				t.Fatalf("down with no results emitted OpenHitMsg")
			}
		}
	}
	if m.HasResults() {
		t.Fatalf("HasResults true unexpectedly")
	}
}

func TestEnterOnFocusedHitEmitsOpenHitMsg(t *testing.T) {
	hits := fakeHits(3)
	m := seedSearchModel(hits)
	m, _ = m.Update(downKey())
	_, cmd := m.Update(keyMsg("enter"))
	msg := runCmd(cmd)
	open, ok := msg.(OpenHitMsg)
	if !ok {
		t.Fatalf("enter on focused hit emitted %T, want OpenHitMsg", msg)
	}
	if open.Hit.ID != hits[1].ID {
		t.Fatalf("OpenHitMsg.Hit.ID = %d, want %d", open.Hit.ID, hits[1].ID)
	}
}

func TestEnterOnSearchWithoutResultsEmitsSearchMsg(t *testing.T) {
	m := NewModel()
	m, _ = m.Update(keyMsg("tab"))
	for _, r := range "ports" {
		m, _ = m.Update(keyMsg(string(r)))
	}
	_, cmd := m.Update(keyMsg("enter"))
	msg := runCmd(cmd)
	if _, ok := msg.(SearchMsg); !ok {
		t.Fatalf("enter on Search with no results emitted %T, want SearchMsg", msg)
	}
}

func TestTabTogglePreservedWithResults(t *testing.T) {
	m := seedSearchModel(fakeHits(3))
	m, _ = m.Update(keyMsg("tab"))
	if m.ActiveTab() != TabTricks {
		t.Fatalf("tab from Search-with-results should go to Tricks, got %v", m.ActiveTab())
	}
	// Results survive the tab toggle (state not cleared).
	if !m.HasResults() {
		t.Fatalf("HasResults dropped on tab toggle")
	}
}

func TestEscWithResultsStillDismisses(t *testing.T) {
	m := seedSearchModel(fakeHits(3))
	_, cmd := m.Update(keyMsg("esc"))
	msg := runCmd(cmd)
	if _, ok := msg.(DismissMsg); !ok {
		t.Fatalf("esc with results emitted %T, want DismissMsg (one-shot close per D4)", msg)
	}
}

func TestViewRendersResultListOnSearchTab(t *testing.T) {
	m := seedSearchModel(fakeHits(3))
	view := m.View()
	if !strings.Contains(view, "3 results") {
		t.Fatalf("view missing result count, got:\n%s", view)
	}
	if !strings.Contains(view, "#100") || !strings.Contains(view, "#102") {
		t.Fatalf("view missing hit id cells, got:\n%s", view)
	}
	if !strings.Contains(view, "task") {
		t.Fatalf("view missing entity-type cell, got:\n%s", view)
	}
	// Cursor marker present on first row.
	if !strings.Contains(view, "▸") {
		t.Fatalf("view missing cursor marker ▸, got:\n%s", view)
	}
}

func TestCleanSnippetStripsMarkTags(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"<mark>hit</mark> rest", "hit rest"},
		{"a <mark>b</mark> c <mark>d</mark>", "a b c d"},
		{"line one\nline two", "line one line two"},
		{"  padded  ", "padded"},
	}
	for _, c := range cases {
		got := cleanSnippet(c.in)
		if got != c.want {
			t.Errorf("cleanSnippet(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanSnippetStripsANSIEscapes(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"bold reset", "\x1b[1mhello\x1b[0m", "hello"},
		{"color sequence", "\x1b[38;5;196mred text\x1b[0m world", "red text world"},
		{"clear screen", "\x1b[2Joh no", "oh no"},
		{"mark + ansi mix", "<mark>\x1b[31mfoo\x1b[0m</mark> bar", "foo bar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cleanSnippet(c.in)
			if got != c.want {
				t.Errorf("cleanSnippet(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSetResultsDefensiveCopy(t *testing.T) {
	hits := fakeHits(3)
	m := NewModel()
	m.SetResults(hits)
	// Mutate the caller's slice; model state must not see the change.
	hits[0].ID = 999
	got, _ := m.FocusedHit()
	if got.ID != 100 {
		t.Fatalf("SetResults aliased caller slice: focused hit id = %d, want 100", got.ID)
	}
}

func TestSetMaxResultRowsCapsRenderedRows(t *testing.T) {
	m := seedSearchModel(fakeHits(200))
	m.SetMaxResultRows(10)
	view := m.View()
	// Count rendered result rows by looking for body-row prefixes
	// (marker cell followed by the `#` of the id column). The header
	// row carries `ID` not `#`, so it does not count.
	rendered := 0
	for _, line := range strings.Split(view, "\n") {
		trimmed := strings.TrimLeft(line, " ▸")
		if strings.HasPrefix(trimmed, "#") {
			rendered++
		}
	}
	if rendered > 10 {
		t.Fatalf("rendered result rows = %d, want <=10 with SetMaxResultRows(10)", rendered)
	}
	if rendered == 0 {
		t.Fatalf("no result rows rendered; view=%q", view)
	}
	if !strings.Contains(view, "200 results") {
		t.Fatalf("result count header missing from view")
	}
	if !strings.Contains(view, "▸") {
		t.Fatalf("cursor marker missing")
	}
}

func TestSetMaxResultRowsWindowSlidesWithCursor(t *testing.T) {
	m := seedSearchModel(fakeHits(50))
	m.SetMaxResultRows(5)
	for i := 0; i < 20; i++ {
		m, _ = m.Update(downKey())
	}
	if m.ResultsCursor() != 20 {
		t.Fatalf("cursor = %d, want 20", m.ResultsCursor())
	}
	view := m.View()
	if !strings.Contains(view, "#120 ") {
		t.Fatalf("focused hit #120 missing from view; view=\n%s", view)
	}
	if strings.Contains(view, "#100 ") {
		t.Fatalf("non-visible hit #100 still rendered; view=\n%s", view)
	}
	if !strings.Contains(view, "▸ #120") {
		t.Fatalf("cursor marker not on focused row; view=\n%s", view)
	}
}

func TestSetMaxResultRowsShowsMoreIndicator(t *testing.T) {
	m := seedSearchModel(fakeHits(20))
	m.SetMaxResultRows(5)
	view := m.View()
	if !strings.Contains(view, "more") {
		t.Fatalf("expected 'more' indicator for hidden rows; view=\n%s", view)
	}
}

func TestSetMaxResultRowsScrollHoldsUntilCursorReachesBottomEdge(t *testing.T) {
	m := seedSearchModel(fakeHits(50))
	m.SetMaxResultRows(10)
	// Move cursor inside the initial window — scroll must NOT slide.
	for i := 0; i < 9; i++ {
		m, _ = m.Update(downKey())
	}
	view := m.View()
	if strings.Contains(view, "↑") {
		t.Fatalf("scroll slid prematurely (got ↑ indicator); view=\n%s", view)
	}
	if !strings.Contains(view, "#100 ") {
		t.Fatalf("first row #100 dropped from view while cursor still in initial window; view=\n%s", view)
	}
	if !strings.Contains(view, "▸ #109") {
		t.Fatalf("cursor not on #109 (bottom of initial window); view=\n%s", view)
	}
	// Cross the bottom edge — scroll must slide by exactly one row.
	m, _ = m.Update(downKey())
	view = m.View()
	if strings.Contains(view, "#100 ") {
		t.Fatalf("#100 still rendered after sliding past bottom edge; view=\n%s", view)
	}
	if !strings.Contains(view, "↑ 1 more") {
		t.Fatalf("expected ↑ 1 more after sliding by one; view=\n%s", view)
	}
	if !strings.Contains(view, "▸ #110") {
		t.Fatalf("cursor not on #110 after first slide; view=\n%s", view)
	}
}

func TestSetMaxResultRowsScrollHoldsOnUpUntilCursorCrossesTopEdge(t *testing.T) {
	m := seedSearchModel(fakeHits(50))
	m.SetMaxResultRows(5)
	// Slide window down so resultsScroll > 0 (cursor=10 → scroll=6, window [6,11)).
	for i := 0; i < 10; i++ {
		m, _ = m.Update(downKey())
	}
	// Walk cursor back up but stay inside the visible window —
	// scroll must hold at 6.
	for i := 0; i < 4; i++ {
		m, _ = m.Update(upKey())
	}
	view := m.View()
	if !strings.Contains(view, "↑ 6 more") {
		t.Fatalf("scroll did not hold while cursor stayed in window; view=\n%s", view)
	}
	if !strings.Contains(view, "▸ #106") {
		t.Fatalf("cursor not on #106 (top of held window); view=\n%s", view)
	}
	// One more up crosses the top edge — scroll slides up by one.
	m, _ = m.Update(upKey())
	view = m.View()
	if !strings.Contains(view, "↑ 5 more") {
		t.Fatalf("expected ↑ 5 more after sliding up by one; view=\n%s", view)
	}
	if !strings.Contains(view, "▸ #105") {
		t.Fatalf("cursor not on #105 after top-edge slide; view=\n%s", view)
	}
}

func TestSetMaxResultRowsZeroMeansUnlimited(t *testing.T) {
	m := seedSearchModel(fakeHits(7))
	// No SetMaxResultRows call — default zero must render all 7.
	view := m.View()
	for i := 0; i < 7; i++ {
		want := fmt.Sprintf("#%d ", 100+i)
		if !strings.Contains(view, want) {
			t.Fatalf("default render missing id cell %s; view=\n%s", want, view)
		}
	}
}

func TestRenderResultListTruncatesLongSnippets(t *testing.T) {
	long := strings.Repeat("x", resultListMaxWidth*3)
	m := seedSearchModel([]domain.SearchHit{
		{EntityType: domain.SearchEntityTask, ID: 1, Snippet: long},
	})
	view := m.View()
	// The body row carries id `#1` in its id cell — find it and
	// assert the total row width respects resultListMaxWidth.
	for _, line := range strings.Split(view, "\n") {
		trimmed := strings.TrimLeft(line, " ▸")
		if !strings.HasPrefix(trimmed, "#1 ") {
			continue
		}
		if width := ansi.StringWidth(line); width > resultListMaxWidth {
			t.Fatalf("result row width = %d, exceeds budget %d (line=%q)", width, resultListMaxWidth, line)
		}
		if !strings.Contains(line, "…") {
			t.Fatalf("truncation indicator (…) missing on long snippet; line=%q", line)
		}
	}
}

func TestRenderResultListEmitsHeader(t *testing.T) {
	m := seedSearchModel(fakeHits(3))
	view := m.View()
	if !strings.Contains(view, "ID") || !strings.Contains(view, "TYPE") || !strings.Contains(view, "RESULT") {
		t.Fatalf("header row missing ID/TYPE/RESULT cells; view=\n%s", view)
	}
	rule := strings.Repeat("─", resultListMaxWidth)
	if !strings.Contains(view, rule) {
		t.Fatalf("horizontal rule of width %d missing under header; view=\n%s", resultListMaxWidth, view)
	}
}
