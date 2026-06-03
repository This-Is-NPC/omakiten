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
			Workflow: config.WorkflowSettings{Active: "root"},
			Theme:    config.ThemeSettings{Active: "omacon"},
			MCP: config.MCPSettings{
				RecentCommentLimit:        5,
				MaxCommentChars:           300,
				IncludeWorkflowInContinue: &includeWF,
				CachePrompts:              &cachePrompts,
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
// EffectiveSectionKeys MINUS the hide list must surface as a distinct
// kicker in the rendered General body. Hidden sections are surfaced
// elsewhere (Runtime card / Project line / dedicated sub-tabs) so they
// must NOT appear as kickers here — that leg is covered by
// TestSettingsGeneralEffectiveSectionsHidden. The kicker resolves
// through the catalog (`tui.settings.effective.section.<n>`), so we
// strip ANSI and search for the human-readable label that the English
// pack ships.
func TestSettingsGeneralEffectiveSectionCountMatchesAccessor(t *testing.T) {
	m, snap := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsGeneralBody())
	sections := snap.EffectiveSectionKeys()
	if len(sections) == 0 {
		t.Fatal("EffectiveSectionKeys returned 0 — fixture has no populated Settings sections")
	}
	visible := 0
	for _, section := range sections {
		if hiddenEffectiveSections[section] {
			continue
		}
		visible++
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
	// Sanity: visible count = accessor count - hidden count present in
	// the fixture. Locks the arithmetic so a future regression that
	// silently drops a visible section is caught here.
	expectedVisible := len(sections)
	for _, section := range sections {
		if hiddenEffectiveSections[section] {
			expectedVisible--
		}
	}
	if visible != expectedVisible {
		t.Fatalf("visible-section walk = %d, want %d (accessor=%d, hidden_in_fixture=%d)", visible, expectedVisible, len(sections), len(sections)-expectedVisible)
	}
}

// TestSettingsGeneralEffectiveRowCountMatchesAccessor pins the
// composition contract leg 2: every tuple the accessor emits in a
// visible section must surface as a row in the merged General body.
// Tuples belonging to hidden sections (Runtime/Project/sub-tab dupes)
// are skipped so the assertion mirrors the renderer's filter. Walks
// the tuple list and asserts the key path appears in the post-strip
// body — values are subject to truncation, so the key path is the
// deterministic anchor.
func TestSettingsGeneralEffectiveRowCountMatchesAccessor(t *testing.T) {
	m, snap := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsGeneralBody())
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		t.Fatal("EffectiveTuples returned 0 — fixture has no populated Settings sections")
	}
	for _, tup := range tuples {
		if hiddenEffectiveSections[tup.Section] {
			continue
		}
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

// TestSettingsGeneralEffectiveSectionOrder pins the user-relevance
// ordering applied by orderEffectiveSections + the hide filter:
// the first visible effective section is `theme` (tier-1 leader, top
// of the order table now that `languages` is hidden) and the last
// known section is `hooks` (tail of the order table — power-user
// escape hatch). The fixture seeds enough sections so all tiers are
// represented; we walk the body once, collect each section kicker's
// index, and assert the relative ordering.
func TestSettingsGeneralEffectiveSectionOrder(t *testing.T) {
	bundle := effectiveBundleFixture()
	// Seed `hooks` so it lands in the rendered body and we can pin
	// it as the tail of the order table. The default fixture omits
	// it, which would otherwise make the "last is hooks" leg vacuous.
	bundle.Config.Hooks = []config.HookSpec{{On: "task.created", Do: "log"}}
	snap := config.BuildSnapshot(bundle)
	m := Model{
		styles:    newStyles(config.Theme{}),
		width:     160,
		height:    200,
		top:       topSettings,
		sub:       subSettingsGeneral,
		project:   domain.ProjectContext{Slug: "test-project"},
		theme:     config.Theme{},
		workflow:  snap.Workflow(),
		languages: config.LanguageSettings{CLI: "en", TUI: "en"},
		repos:     Repositories{Cache: runtimecache.Install(0, snap)},
	}
	body := stripANSI(m.renderSettingsGeneralBody())

	// Section kickers and label rows both render as `// LABEL` — the
	// difference is the value cell. Kickers render an empty value
	// (summaryRows emits `[]string{kicker, ""}` for the title row);
	// label rows always carry content (`—` placeholder at minimum).
	// We scan line-by-line and only accept lines whose value cell
	// (everything between the second and third `│` border) is blank.
	sections := snap.EffectiveSectionKeys()
	lines := strings.Split(body, "\n")
	indexOf := func(section string) int {
		needle := "// " + strings.ToUpper(m.t("tui.settings.effective.section."+section))
		for i, line := range lines {
			if !strings.Contains(line, needle) {
				continue
			}
			parts := strings.Split(line, "│")
			if len(parts) < 4 {
				continue
			}
			if strings.TrimSpace(parts[2]) != "" {
				continue
			}
			return i
		}
		return -1
	}

	// Assertion 1: the first visible effective-section kicker in the
	// rendered body is `theme`. Hidden sections (`languages`,
	// `workflow`, `template_defaults`, `tag_synonyms`) must not
	// appear, so `theme` leads regardless of the accessor's
	// struct-field order.
	themeIdx := indexOf("theme")
	if themeIdx < 0 {
		t.Fatalf("rendered body missing `theme` section; body:\n%s", body)
	}
	for _, present := range sections {
		if present == "theme" || hiddenEffectiveSections[present] {
			continue
		}
		idx := indexOf(present)
		if idx < 0 {
			continue
		}
		if idx < themeIdx {
			t.Fatalf("section %q rendered before `theme` (idx %d < %d); theme must be the head of the visible order; body:\n%s", present, idx, themeIdx, body)
		}
	}

	// Assertion 2: every known visible section that precedes `hooks`
	// in the render-order table is either absent or also before
	// `hooks` — so the last rendered known section is `hooks` for
	// this fixture.
	hooksIdx := indexOf("hooks")
	if hooksIdx < 0 {
		t.Fatalf("rendered body missing `hooks` section; fixture seeding failed; body:\n%s", body)
	}
	for _, present := range sections {
		if hiddenEffectiveSections[present] || present == "hooks" {
			continue
		}
		// Only rank against sections in the known order table.
		known := false
		for _, k := range effectiveSectionRenderOrder {
			if k == present {
				known = true
				break
			}
		}
		if !known {
			continue
		}
		idx := indexOf(present)
		if idx > hooksIdx {
			t.Fatalf("known section %q rendered after `hooks` (idx %d > %d); hooks must be the tail of the order table; body:\n%s", present, idx, hooksIdx, body)
		}
	}
}

// TestSettingsGeneralEffectiveSectionsHidden pins the hide policy:
// `languages`, `workflow`, `template_defaults`, and `tag_synonyms`
// are suppressed in Settings › General because their effective values
// are already surfaced elsewhere (Runtime card / Project line /
// dedicated sub-tabs). The fixture populates `tag_synonyms` and
// `workflow` so this test would catch a regression that re-introduced
// either kicker. `languages` and `template_defaults` are also asserted
// even though the default fixture doesn't populate the latter — the
// negative assertion is cheap and locks the contract for both keys.
//
// Kicker detection mirrors TestSettingsGeneralEffectiveSectionOrder:
// kickers render as `// LABEL` with an empty value cell; row labels
// (e.g. PROJECT card's `workflow` row) share the `// LABEL` prefix but
// always carry a value, so we filter on the cell being blank to avoid
// a false positive against the Project card's `workflow` row.
func TestSettingsGeneralEffectiveSectionsHidden(t *testing.T) {
	m, _ := effectiveModelFixture(160, 200)
	body := stripANSI(m.renderSettingsGeneralBody())
	lines := strings.Split(body, "\n")

	hasKicker := func(label string) bool {
		needle := "// " + strings.ToUpper(label)
		for _, line := range lines {
			if !strings.Contains(line, needle) {
				continue
			}
			parts := strings.Split(line, "│")
			if len(parts) < 4 {
				continue
			}
			if strings.TrimSpace(parts[2]) != "" {
				continue
			}
			return true
		}
		return false
	}

	for section := range hiddenEffectiveSections {
		label := m.t("tui.settings.effective.section." + section)
		if label == "tui.settings.effective.section."+section {
			t.Fatalf("catalog missing label for hidden section %q (resolved to raw key) — hide check needs a real label to assert against", section)
		}
		if hasKicker(label) {
			t.Fatalf("rendered body must not contain kicker for hidden section %q (label %q); body:\n%s", section, label, body)
		}
	}
}

// TestOrderEffectiveSectionsUnknownTail pins the defensive contract:
// sections not listed in effectiveSectionRenderOrder are appended in
// their original input order, after all known sections. Guards future
// Settings field additions from silently dropping out of the viewer.
// `theme`/`backup`/`hooks` are picked from the current order table so
// the test stays decoupled from the exact tier roster (e.g. a reorder
// inside the known set would not break this assertion as long as
// known-leads-unknowns holds).
func TestOrderEffectiveSectionsUnknownTail(t *testing.T) {
	in := []string{"future_one", "hooks", "backup", "future_two", "theme"}
	got := orderEffectiveSections(in)
	want := []string{"theme", "backup", "hooks", "future_one", "future_two"}
	if len(got) != len(want) {
		t.Fatalf("orderEffectiveSections len = %d, want %d (%v vs %v)", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("orderEffectiveSections[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestSettingsGeneralEffectiveTruncatesLongValues pins the
// truncation contract: very long scalar values render with the
// terminal-native `…` glyph instead of wrapping or overflowing. We
// stage a custom Settings with a 200-char value and look for the
// ellipsis in the rendered body. Uses `theme.active` as the carrier
// because `tag_synonyms` (the earlier choice) is now hidden from the
// effective viewer — the truncation logic itself is section-agnostic
// so the assertion still pins the same code path.
func TestSettingsGeneralEffectiveTruncatesLongValues(t *testing.T) {
	bundle := effectiveBundleFixture()
	bundle.Config.Theme = config.ThemeSettings{Active: strings.Repeat("x", 200)}
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
