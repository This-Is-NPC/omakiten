package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
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
	if m.settingsGeneralLines.Scroll() != maxOffset {
		t.Fatalf("scroll offset = %d after over-shooting; want clamped to maxSettingsGeneralScroll() = %d", m.settingsGeneralLines.Scroll(), maxOffset)
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

	if m.settingsGeneralLines.Scroll() == 0 {
		t.Fatalf("expected `G` to move scroll past 0 when body overflows; got 0")
	}
	rendered := m.renderSettingsGeneral()
	if !strings.Contains(rendered, themeMarker) {
		t.Fatalf("expected THEME row visible after `G`; rendered:\n%s", rendered)
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if m.settingsGeneralLines.Scroll() != 0 {
		t.Fatalf("expected `g` to reset scroll to 0; got %d", m.settingsGeneralLines.Scroll())
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
	if m.settingsGeneralLines.Scroll() != step {
		t.Fatalf("after pgdown: scroll = %d, want %d", m.settingsGeneralLines.Scroll(), step)
	}

	m.handleSettingsGeneralKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.settingsGeneralLines.Scroll() != 0 {
		t.Fatalf("after pgup: scroll = %d, want 0", m.settingsGeneralLines.Scroll())
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
	if m.settingsGeneralLines.Scroll() != 0 {
		t.Fatalf("expected `G` to be a no-op when body fits; got scroll = %d", m.settingsGeneralLines.Scroll())
	}
}

// TestClampSettingsGeneralScroll locks the boundary behavior of the
// shared clamp helper — the renderer relies on it to keep a leftover
// offset from a wider terminal from stranding the user past the new
// last row when the terminal is resized smaller.
func TestClampSettingsGeneralScroll(t *testing.T) {
	for _, tc := range []struct {
		name     string
		offset   int
		total    int
		viewport int
		want     int
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

// effectiveBundleFixture builds a Bundle with several populated
// top-level Settings sections so the effective-config composer has
// multiple cards to pack inside General. The exact values don't
// matter — the assertions key off the accessor's reported section /
// row counts so this fixture stays resilient against future Settings
// field additions.
func effectiveBundleFixture() config.Bundle {
	cachePrompts := true
	includeWF := false
	return config.Bundle{
		Kit: config.Kit{Key: "root"},
		Config: config.Settings{
			Output: config.OutputSettings{
				JSONMinified: false,
				OmitEmpty:    true,
			},
			Context: config.ContextSettings{
				DefaultLevel: 1,
				MaxTokens:    4096,
			},
			Workflow: config.WorkflowSettings{Active: "root"},
			Theme:    config.ThemeSettings{Active: "omacon"},
			MCP: config.MCPSettings{
				RecentCommentLimit:        5,
				MaxCommentChars:           300,
				IncludeWorkflowInContinue: &includeWF,
				CachePrompts:              &cachePrompts,
				RecentContextLimit:        3,
				NextWorkLimit:             4,
				SimilarTaskLimit:          6,
			},
			TagSynonyms: map[string]string{
				"bugfix": "bug",
				"feat":   "feature",
			},
			Languages: config.LanguageSettings{
				CLI: "en",
				TUI: "en",
			},
		},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "root",
			Name: "Root",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Dev", Position: 2},
				{ID: 3, Key: "done", Name: "Done", Position: 3},
			},
		}},
	}
}

// effectiveModelFixture wires a Model around the snapshot built from
// the bundle fixture, mirroring the runtime-cache plumbing the
// production renderer reads through.
func effectiveModelFixture(width, height int) (Model, *config.Snapshot) {
	snap := config.BuildSnapshot(effectiveBundleFixture())
	m := Model{
		styles:    newStyles(config.Theme{}),
		width:     width,
		height:    height,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		theme:     config.Theme{},
		workflow:  snap.Workflow(),
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Cache: runtimecache.Install(0, snap)},
	}
	return m, snap
}

// TestSettingsGeneralPreservesRuntimeAndProjectAfterComposition pins
// the regression guard for the composition: folding the
// effective-config sections into the General body must not drop the
// pre-existing Runtime / Project tables. We render the merged body
// and assert both pre-existing kickers still surface.
func TestSettingsGeneralPreservesRuntimeAndProjectAfterComposition(t *testing.T) {
	m, _ := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsGeneralBody())
	for _, label := range []string{m.t("tui.kicker.runtime"), m.t("tui.kicker.project")} {
		if !strings.Contains(body, strings.ToUpper(label)) {
			t.Fatalf("rendered General body missing pre-existing kicker %q; body:\n%s", label, body)
		}
	}
}

// TestSettingsGeneralEffectiveSectionCountMatchesAccessor pins the
// composition contract: every top-level section reported by
// EffectiveSectionKeys must surface as a distinct kicker in the
// rendered General body. The kicker resolves through the catalog
// (`tui.settings.effective.section.<n>`), so we strip ANSI and search
// for the human-readable label that the English pack ships.
func TestSettingsGeneralEffectiveSectionCountMatchesAccessor(t *testing.T) {
	m, snap := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsGeneralBody())
	sections := snap.EffectiveSectionKeys()
	if len(sections) == 0 {
		t.Fatal("EffectiveSectionKeys returned 0 — fixture has no populated Settings sections")
	}
	for _, section := range sections {
		label := m.t("tui.settings.effective.section." + section)
		if label == "tui.settings.effective.section."+section {
			t.Fatalf("catalog missing label for section %q (resolved to raw key)", section)
		}
		// The kicker style uppercases the label and prefixes "// ";
		// we search for the uppercased label so the assertion
		// survives a styling tweak that would otherwise change the
		// prefix.
		if !strings.Contains(body, strings.ToUpper(label)) {
			t.Fatalf("rendered body missing kicker for section %q (label %q); body:\n%s", section, label, body)
		}
	}
}

// TestSettingsGeneralEffectiveRowCountMatchesAccessor pins the
// composition contract leg 2: every tuple the accessor emits must
// surface as a row in the merged General body. Walks the tuple list
// and asserts the key path appears in the post-strip body — values
// are subject to truncation, so the key path is the deterministic
// anchor.
func TestSettingsGeneralEffectiveRowCountMatchesAccessor(t *testing.T) {
	m, snap := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsGeneralBody())
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		t.Fatal("EffectiveTuples returned 0 — fixture has no populated Settings sections")
	}
	for _, tup := range tuples {
		// Scalar sections emit Key="" — the composer falls back to
		// the section name, so search for that instead. The kicker
		// styling uppercases the key path, so we match the
		// uppercase form to stay style-independent.
		needle := tup.Key
		if needle == "" {
			needle = tup.Section
		}
		needle = strings.ToUpper(needle)
		if !strings.Contains(body, needle) {
			t.Fatalf("rendered body missing row for tuple %s.%s (looking for %q); body:\n%s", tup.Section, tup.Key, needle, body)
		}
	}
}

// TestSettingsGeneralNoHorizontalOverflow pins the layout contract:
// at the minimum supported terminal width, no rendered body line
// exceeds the available width. `availableWidth` is the renderer's
// own ceiling, so we measure visible (ANSI-stripped) line widths
// against it.
func TestSettingsGeneralNoHorizontalOverflow(t *testing.T) {
	// 80 cols is the canonical minimum the project's other renderers
	// target (matches the help screen + table fixtures). Anything
	// narrower drops below `availableWidth`'s 24-col floor and stops
	// being a meaningful overflow test.
	const minWidth = 80
	m, _ := effectiveModelFixture(minWidth, 200)

	body := m.renderSettingsGeneralBody()
	width := m.availableWidth()
	for i, line := range strings.Split(body, "\n") {
		// lipgloss.Width strips ANSI styling and accounts for
		// wide-glyph runes, so it reports the actual terminal-cell
		// footprint of the line.
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %d overflows availableWidth=%d (width=%d): %q", i, width, w, line)
		}
	}
}

// TestSettingsGeneralFooterHintIsReadOnly pins the hint decision: the
// single general_hint covers both the original Runtime / Project
// affordances (theme/config/subtask-kit pickers + $EDITOR) and the
// read-only contract the merged effective-config rows now inherit.
// Anchors that the read-only language stays present after the
// composition, since the previously separate effective_hint is no
// longer surfaced on the panel.
func TestSettingsGeneralFooterHintIsReadOnly(t *testing.T) {
	m, _ := effectiveModelFixture(160, 200)
	rendered := stripANSI(m.renderSettingsGeneral())
	if !strings.Contains(rendered, "read-only") {
		t.Fatalf("rendered General view missing read-only language in footer hint; got:\n%s", rendered)
	}
}

// TestSettingsGeneralEffectiveTruncatesLongValues pins the
// truncation contract: very long scalar values render with the
// terminal-native `…` glyph instead of wrapping or overflowing. We
// stage a custom Settings with a 200-char value and look for the
// ellipsis in the rendered body.
func TestSettingsGeneralEffectiveTruncatesLongValues(t *testing.T) {
	bundle := effectiveBundleFixture()
	bundle.Config.TagSynonyms = map[string]string{
		"long": strings.Repeat("x", 200),
	}
	snap := config.BuildSnapshot(bundle)
	m := Model{
		styles:    newStyles(config.Theme{}),
		width:     160,
		height:    200,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		workflow:  snap.Workflow(),
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Cache: runtimecache.Install(0, snap)},
	}
	body := stripANSI(m.renderSettingsGeneralBody())
	if !strings.Contains(body, "…") {
		t.Fatalf("expected truncation glyph `…` in body for 200-char value; body:\n%s", body)
	}
	if strings.Contains(body, strings.Repeat("x", 200)) {
		t.Fatalf("expected long value to be truncated; body still contains full 200-char string")
	}
}
