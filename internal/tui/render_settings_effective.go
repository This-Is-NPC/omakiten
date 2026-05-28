package tui

import (
	"strings"

	"omakiten/internal/config"
)

// renderSettingsEffective renders the read-only effective-configuration
// viewer surfaced under Settings › Effective (screen `03 //settings`).
// Mirrors `renderSettingsGeneral` — one panel, indented body, footer
// hint, body wrapped in `sliceScrollRows` against the same Settings ›
// General scroll state so the existing key handler scrolls this tab
// the same way it scrolls General.
//
// Layout: one bordered summary table per top-level YAML section
// (kicker = `tui.settings.effective.section.<name>`), rows = `key path`
// + `effective value`. Tables stack vertically through the shared
// `renderSummaryTables` packer so narrow terminals get the same
// label/value reshape the sibling General tab already uses — no new
// layout primitive lands here per `feedback_tui_wrappable_sections`.
//
// Source layer (`default` / `project` / `env`) is reserved on the
// accessor but not yet threaded — the i18n keys exist
// (`tui.settings.effective.source.*`) so a follow-up wave can light up
// a third column without touching this renderer's call shape.
func (m Model) renderSettingsEffective() string {
	body := m.renderSettingsEffectiveBody()
	hint := m.styles.hint.Render(m.t("tui.settings.effective_hint"))

	bodyLines := strings.Split(body, "\n")
	viewport := m.settingsGeneralViewportRows()
	visible := m.sliceScrollRows(bodyLines, m.settingsGeneralLines.Scroll(), viewport)
	return "\n" + indentBlock(strings.Join(visible, "\n")+"\n\n"+hint, 2)
}

// renderSettingsEffectiveBody builds the per-section summary tables as
// a single rendered block. Extracted so the scroll handler can measure
// the body height without re-running the scroll wrapper, and so the
// render test can assert section / row counts directly against the
// inner body string (the outer renderer indents + clips and would
// otherwise hide the structure under chrome).
func (m Model) renderSettingsEffectiveBody() string {
	snap := m.repos.activeSnapshot()
	if snap == nil {
		return m.styles.hint.Render(m.t("tui.settings.effective_hint"))
	}
	tuples := snap.EffectiveTuples()
	if len(tuples) == 0 {
		return m.styles.hint.Render(m.t("tui.settings.effective_hint"))
	}

	// Bucket tuples by section while preserving the accessor's
	// declaration-order section list. `EffectiveSectionKeys` already
	// returns sections in the canonical Settings struct field order, so
	// we lean on it for the outer loop instead of re-tracking order
	// while we group.
	sectionKeys := snap.EffectiveSectionKeys()
	grouped := make(map[string][]config.EffectiveTuple, len(sectionKeys))
	for _, t := range tuples {
		grouped[t.Section] = append(grouped[t.Section], t)
	}

	// Cap the value column so long literals (long paths, packed
	// transition lists, multi-line YAML scalars) truncate with the
	// `…` glyph instead of wrapping and breaking the grid. The
	// summary-table packer scales the value column up in Auto mode, so
	// this is only a soft ceiling for individual cell values — the
	// table itself still fills the panel width.
	const valueCap = 80

	tables := make([][][]string, 0, len(sectionKeys))
	for _, section := range sectionKeys {
		rows := grouped[section]
		if len(rows) == 0 {
			continue
		}
		label := m.t("tui.settings.effective.section." + section)
		fields := make([][2]string, 0, len(rows))
		for _, r := range rows {
			keyPath := r.Key
			if keyPath == "" {
				// Scalar sections (no nested path) — show the section
				// name as the key so the row still reads as
				// `<section> · <value>` instead of leaving an empty
				// label cell.
				keyPath = section
			}
			fields = append(fields, [2]string{
				keyPath,
				truncateText(r.Value, valueCap),
			})
		}
		tables = append(tables, m.summaryRows(label, fields...))
	}

	if len(tables) == 0 {
		return m.styles.hint.Render(m.t("tui.settings.effective_hint"))
	}

	return m.renderSummaryTables(summaryTablesOpts{
		LabelWidth: 24,
		ValueWidth: 36,
		Auto:       true,
	}, tables...)
}
