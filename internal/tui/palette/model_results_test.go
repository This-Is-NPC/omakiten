package palette

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
			Snippet:    "snippet " + string(rune('a'+i)),
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
	if !strings.Contains(view, "task #100") || !strings.Contains(view, "task #102") {
		t.Fatalf("view missing hit rows, got:\n%s", view)
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
