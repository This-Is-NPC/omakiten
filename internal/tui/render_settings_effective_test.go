package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
)

// effectiveBundleFixture builds a Bundle with several populated
// top-level Settings sections so the renderer has multiple cards to
// pack. The exact values don't matter — the assertions key off the
// accessor's reported section / row counts so this fixture stays
// resilient against future Settings field additions.
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

// TestRenderSettingsEffectiveSectionCountMatchesAccessor pins AC §5:
// every top-level section reported by EffectiveSectionKeys must
// surface as a distinct kicker in the rendered body. The kicker
// resolves through the catalog (`tui.settings.effective.section.<n>`),
// so we strip ANSI and search for the human-readable label that the
// English pack ships.
func TestRenderSettingsEffectiveSectionCountMatchesAccessor(t *testing.T) {
	m, snap := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsEffectiveBody())
	sections := snap.EffectiveSectionKeys()
	if len(sections) == 0 {
		t.Fatal("EffectiveSectionKeys returned 0 — fixture has no populated Settings sections")
	}
	for _, section := range sections {
		label := m.t("tui.settings.effective.section." + section)
		if label == "tui.settings.effective.section."+section {
			t.Fatalf("catalog missing label for section %q (resolved to raw key)", section)
		}
		// The kicker style uppercases the label and prefixes "// "; we
		// search for the uppercased label so the assertion survives a
		// styling tweak that would otherwise change the prefix.
		if !strings.Contains(body, strings.ToUpper(label)) {
			t.Fatalf("rendered body missing kicker for section %q (label %q); body:\n%s", section, label, body)
		}
	}
}

// TestRenderSettingsEffectiveRowCountMatchesAccessor pins AC §5 leg 2:
// every tuple the accessor emits surfaces as a row in the rendered
// body. Walks the tuple list and asserts the key path appears in the
// post-strip body — values are subject to truncation, so the key path
// is the deterministic anchor.
func TestRenderSettingsEffectiveRowCountMatchesAccessor(t *testing.T) {
	m, snap := effectiveModelFixture(160, 200)

	body := stripANSI(m.renderSettingsEffectiveBody())
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		t.Fatal("EffectiveTuples returned 0 — fixture has no populated Settings sections")
	}
	for _, tup := range tuples {
		// Scalar sections emit Key="" — the renderer falls back to
		// the section name, so search for that instead. The kicker
		// styling uppercases the key path, so we match the uppercase
		// form to stay style-independent.
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

// TestRenderSettingsEffectiveNoHorizontalOverflow pins AC §4: at the
// minimum supported terminal width, no rendered body line exceeds the
// available width. `availableWidth` is the renderer's own ceiling, so
// we measure visible (ANSI-stripped) line widths against it.
func TestRenderSettingsEffectiveNoHorizontalOverflow(t *testing.T) {
	// 80 cols is the canonical minimum the project's other renderers
	// target (matches the help screen + table fixtures). Anything
	// narrower drops below `availableWidth`'s 24-col floor and stops
	// being a meaningful overflow test.
	const minWidth = 80
	m, _ := effectiveModelFixture(minWidth, 200)

	body := m.renderSettingsEffectiveBody()
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

// TestRenderSettingsEffectiveFooterHint asserts AC §3: the read-only
// footer hint surfaces via the catalog key 258.2 registered. Anchors
// the footer wiring so a future refactor cannot silently drop the
// hint while still passing the structural section / row assertions.
func TestRenderSettingsEffectiveFooterHint(t *testing.T) {
	m, _ := effectiveModelFixture(160, 200)
	rendered := stripANSI(m.renderSettingsEffective())
	hint := m.t("tui.settings.effective_hint")
	if !strings.Contains(rendered, hint) {
		t.Fatalf("rendered effective view missing footer hint %q; got:\n%s", hint, rendered)
	}
}

// TestRenderSettingsEffectiveTruncatesLongValues pins AC §2: very
// long scalar values render with the terminal-native `…` glyph
// instead of wrapping or overflowing. We stage a custom Settings with
// a 200-char value and look for the ellipsis in the rendered body.
func TestRenderSettingsEffectiveTruncatesLongValues(t *testing.T) {
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
	body := stripANSI(m.renderSettingsEffectiveBody())
	if !strings.Contains(body, "…") {
		t.Fatalf("expected truncation glyph `…` in body for 200-char value; body:\n%s", body)
	}
	if strings.Contains(body, strings.Repeat("x", 200)) {
		t.Fatalf("expected long value to be truncated; body still contains full 200-char string")
	}
}
