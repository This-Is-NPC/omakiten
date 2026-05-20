package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// TestSettingsGeneralScrollsWhenBodyOverflowsViewport locks the fix for
// the "Settings → general dead-end" bug: at a terminal height shorter
// than the rendered Runtime + Project tables, the bottom rows
// (including PROJECT → THEME) used to be silently clipped because the
// renderer emitted a static block. The fix wraps the body in a scroll
// window. This test mounts the view at an overflow-inducing height,
// asserts the THEME row is clipped initially, sends enough `j` presses
// to reach the bottom, and asserts the row is now visible.
func TestSettingsGeneralScrollsWhenBodyOverflowsViewport(t *testing.T) {
	const themeMarker = "marker-theme-row"

	m := &Model{
		styles:    newStyles(config.Theme{}),
		width:     120,
		height:    20,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		theme:     config.Theme{Key: themeMarker},
		workflow:  domain.Workflow{Key: "test-wf", Buckets: []domain.Bucket{{Key: "backlog"}, {Key: "dev"}, {Key: "review"}, {Key: "done"}}},
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
	}

	body := m.renderSettingsGeneralBody()
	bodyLines := strings.Count(body, "\n") + 1
	viewport := m.settingsGeneralViewportRows()
	if viewport <= 0 {
		t.Fatalf("viewport budget = 0; fixture too small to scroll (bodyLines=%d, height=%d)", bodyLines, m.height)
	}
	if bodyLines <= viewport {
		t.Fatalf("fixture does not overflow: bodyLines=%d viewport=%d — pick a smaller height", bodyLines, viewport)
	}

	clipped := m.renderSettingsGeneral()
	if strings.Contains(clipped, themeMarker) {
		t.Fatalf("expected THEME row to be clipped at top of scroll; rendered:\n%s", clipped)
	}

	for i := 0; i < bodyLines+5; i++ {
		m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}

	expanded := m.renderSettingsGeneral()
	if !strings.Contains(expanded, themeMarker) {
		t.Fatalf("expected THEME row visible after scrolling to bottom; rendered:\n%s", expanded)
	}

	maxOffset := m.maxSettingsGeneralScroll()
	if m.settingsGeneralScroll != maxOffset {
		t.Fatalf("scroll offset = %d after over-shooting; want clamped to maxSettingsGeneralScroll() = %d", m.settingsGeneralScroll, maxOffset)
	}
}

// TestSettingsGeneralEndJumpsToBottom asserts the `G` / `end` binding
// jumps the scroll offset to the same row as paging all the way down.
// Used to anchor the bottom-most row inside the viewport in a single
// keystroke — same affordance the sibling Settings sub-tabs offer.
func TestSettingsGeneralEndJumpsToBottom(t *testing.T) {
	const themeMarker = "marker-theme-row"
	m := &Model{
		styles:    newStyles(config.Theme{}),
		width:     120,
		height:    20,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		theme:     config.Theme{Key: themeMarker},
		workflow:  domain.Workflow{Key: "test-wf", Buckets: []domain.Bucket{{Key: "backlog"}, {Key: "dev"}, {Key: "review"}, {Key: "done"}}},
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})

	if m.settingsGeneralScroll == 0 {
		t.Fatalf("expected `G` to move scroll past 0 when body overflows; got 0")
	}
	rendered := m.renderSettingsGeneral()
	if !strings.Contains(rendered, themeMarker) {
		t.Fatalf("expected THEME row visible after `G`; rendered:\n%s", rendered)
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.settingsGeneralScroll != 0 {
		t.Fatalf("expected `g` to reset scroll to 0; got %d", m.settingsGeneralScroll)
	}
}

// TestSettingsGeneralPageDownAdvancesByViewportStep asserts pgdown
// scrolls by `taskViewPageStep(viewport)` and that pgup returns to the
// origin. Keeps the page-step bindings from silently regressing — the
// integration test only exercises `j`, so a typo in the pgdown/pgup
// arms of `handleSettingsGeneralKey` would otherwise go unnoticed.
func TestSettingsGeneralPageDownAdvancesByViewportStep(t *testing.T) {
	m := &Model{
		styles:    newStyles(config.Theme{}),
		width:     120,
		height:    20,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		theme:     config.Theme{Key: "marker-theme-row"},
		workflow:  domain.Workflow{Key: "test-wf", Buckets: []domain.Bucket{{Key: "backlog"}, {Key: "dev"}, {Key: "review"}, {Key: "done"}}},
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
	}

	step := taskViewPageStep(m.settingsGeneralViewportRows())
	if step <= 0 {
		t.Fatalf("page step = %d; fixture too small to exercise pgdown", step)
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.settingsGeneralScroll != step {
		t.Fatalf("after pgdown: scroll = %d, want %d", m.settingsGeneralScroll, step)
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.settingsGeneralScroll != 0 {
		t.Fatalf("after pgup: scroll = %d, want 0", m.settingsGeneralScroll)
	}
}

// TestSettingsGeneralEndIsNoOpWhenBodyFits guards the invariant that
// `G` does nothing when the entire body already fits in the viewport —
// otherwise the user would see the scroll hints appear on a screen that
// has no content hidden, which would be a confusing affordance.
func TestSettingsGeneralEndIsNoOpWhenBodyFits(t *testing.T) {
	m := &Model{
		styles:    newStyles(config.Theme{}),
		width:     120,
		height:    200,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		theme:     config.Theme{Key: "marker-theme-row"},
		workflow:  domain.Workflow{Key: "test-wf", Buckets: []domain.Bucket{{Key: "backlog"}, {Key: "dev"}, {Key: "review"}, {Key: "done"}}},
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
	}

	body := m.renderSettingsGeneralBody()
	bodyLines := strings.Count(body, "\n") + 1
	viewport := m.settingsGeneralViewportRows()
	if bodyLines > viewport {
		t.Fatalf("fixture overflows: bodyLines=%d viewport=%d — pick a taller height", bodyLines, viewport)
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if m.settingsGeneralScroll != 0 {
		t.Fatalf("expected `G` to be a no-op when body fits; got scroll = %d", m.settingsGeneralScroll)
	}
}

// TestClampSettingsGeneralScroll locks the boundary behavior of the
// shared clamp helper — the renderer relies on it to keep a leftover
// offset from a wider terminal from stranding the user past the new
// last row when the terminal is resized smaller.
func TestClampSettingsGeneralScroll(t *testing.T) {
	for _, tc := range []struct {
		name           string
		offset         int
		total          int
		viewport       int
		want           int
	}{
		{"viewport zero short-circuits", 5, 30, 0, 0},
		{"body fits clamps to zero", 5, 8, 12, 0},
		{"negative offset clamps to zero", -3, 30, 10, 0},
		{"in-range offset preserved", 5, 30, 10, 5},
		{"over-shoot clamps to max", 999, 30, 10, 30 - 8 /*dataRows = viewport-2*/},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clampSettingsGeneralScroll(tc.offset, tc.total, tc.viewport)
			if got != tc.want {
				t.Fatalf("clampSettingsGeneralScroll(%d, %d, %d) = %d, want %d", tc.offset, tc.total, tc.viewport, got, tc.want)
			}
		})
	}
}
