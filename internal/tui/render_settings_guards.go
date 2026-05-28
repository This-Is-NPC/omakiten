package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/gridtable"
)

// renderSettingsGuards renders the read-only N×N matrix surfaced under
// Settings › Guards. Rows = from-bucket; columns = to-bucket; cell
// content reflects the four states declared on the active workflow:
//
//   - diagonal (`from == to`) → sentinel `tui.settings.guards.sentinel_disallowed`
//   - no entry in `workflow.transitions` → same disallowed sentinel
//   - allowed + zero guards → sentinel `tui.settings.guards.sentinel_empty`
//   - allowed + ≥1 guards → comma-separated guard slugs from `snap.Guards`
//
// The body wraps in `sliceScrollRows` so the user can scroll once the
// dispatch wiring (259.4) installs handlers — for the moment the offset
// is hard-pinned to 0 because no handler ships yet; the renderer still
// clips correctly when the matrix exceeds the viewport so the layout
// budget is honoured the day the handlers land.
func (m Model) renderSettingsGuards() string {
	body := m.renderSettingsGuardsBody()
	hint := m.styles.hint.Render(m.t("tui.settings.guards_hint"))

	bodyLines := strings.Split(body, "\n")
	viewport := m.settingsGuardsViewportRows()
	visible := m.sliceScrollRows(bodyLines, 0, viewport)
	return "\n" + indentBlock(strings.Join(visible, "\n")+"\n\n"+hint, 2)
}

// renderSettingsGuardsBody builds the from/to matrix as a single
// rendered block. Extracted so the future key handler (259.4) can
// measure body height for scroll clamping without re-running the
// renderer's scroll wrapper — `renderSettingsGeneral` follows the same
// split.
//
// Buckets are ordered by Position so the matrix mirrors workflow flow
// (backlog → done for omakase). Guard slugs come from the active
// snapshot when wired; absent a snapshot (test fixtures without a
// runtime cache) the cell falls back to the empty sentinel because the
// transition list alone cannot reveal guard payloads.
func (m Model) renderSettingsGuardsBody() string {
	buckets := orderedBuckets(m.workflow.Buckets)
	disallowed := m.t("tui.settings.guards.sentinel_disallowed")
	empty := m.t("tui.settings.guards.sentinel_empty")

	transitions := indexTransitions(m.workflow.Transitions)
	snap := m.repos.activeSnapshot()

	rows := make([][]string, 0, len(buckets)+2)
	rows = append(rows, []string{m.styles.kicker(m.t("tui.settings.guards_tab"))})

	headerCells := make([]string, 0, len(buckets)+1)
	headerCells = append(headerCells, m.styles.kicker(m.t("tui.settings.guards.header_from")+" \\ "+m.t("tui.settings.guards.header_to")))
	for _, b := range buckets {
		headerCells = append(headerCells, m.styles.kicker(b.Key))
	}
	rows = append(rows, headerCells)

	for _, from := range buckets {
		cells := make([]string, 0, len(buckets)+1)
		cells = append(cells, m.styles.kicker(from.Key))
		for _, to := range buckets {
			cells = append(cells, guardCellContent(from, to, transitions, snap, disallowed, empty))
		}
		rows = append(rows, cells)
	}

	labelWidth, valueWidth := guardColumnWidths(m.availableWidth(), buckets, rows)
	widths := make([]int, 0, len(buckets)+1)
	widths = append(widths, labelWidth)
	for range buckets {
		widths = append(widths, valueWidth)
	}
	return gridtable.Render(rows, widths, m.styles.border)
}

// settingsGuardsViewportRows mirrors `settingsGeneralViewportRows` —
// `panelViewportRows(2)` reserves the blank separator + hint row that
// `renderSettingsGuards` appends below the body.
func (m Model) settingsGuardsViewportRows() int {
	return m.panelViewportRows(2)
}

// orderedBuckets returns the workflow buckets sorted by Position so the
// matrix renders in workflow order. Stable on the original index for
// fixtures where Position is unset (zero) on every bucket — common in
// constructor-only test fixtures that pass `[]domain.Bucket{{Key: ...}}`.
func orderedBuckets(in []domain.Bucket) []domain.Bucket {
	out := append([]domain.Bucket(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Position < out[j].Position
	})
	return out
}

// indexTransitions builds a presence set keyed by (fromKey, toKey) so
// the cell loop can answer "is this transition declared?" in O(1)
// without scanning the slice once per cell.
func indexTransitions(in []domain.WorkflowTransition) map[[2]string]bool {
	out := make(map[[2]string]bool, len(in))
	for _, t := range in {
		out[[2]string{t.FromBucketKey, t.ToBucketKey}] = true
	}
	return out
}

// guardCellContent picks the right sentinel or guard-slug list for the
// (from, to) cell. A nil snapshot degrades allowed transitions to the
// empty sentinel because guard payloads live on the snapshot — the
// transition list alone reveals only existence, not the guard slug.
func guardCellContent(from, to domain.Bucket, transitions map[[2]string]bool, snap *config.Snapshot, disallowed, empty string) string {
	if from.Key == to.Key {
		return disallowed
	}
	if !transitions[[2]string{from.Key, to.Key}] {
		return disallowed
	}
	if snap == nil {
		return empty
	}
	guards := snap.Guards(from.ID, to.ID)
	if len(guards) == 0 {
		return empty
	}
	slugs := make([]string, 0, len(guards))
	for _, g := range guards {
		slugs = append(slugs, g.Type)
	}
	return strings.Join(slugs, ", ")
}

// guardColumnWidths sizes the label column to the widest rendered
// kicker and divides the remaining panel width across the to-bucket
// columns. Mirrors the auto-sizing the `summary_table` helper does for
// two-column tables, extended to N data columns so the 4×4 omakase
// matrix consumes the full panel width without wrapping a guard slug
// mid-token (which would lose its ANSI styling on the dropped line).
func guardColumnWidths(available int, buckets []domain.Bucket, rows [][]string) (label, value int) {
	const minLabel = 10
	const minValue = 12
	label = minLabel
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		if w := lipgloss.Width(row[0]); w > label {
			label = w
		}
	}
	value = minValue
	if n := len(buckets); n > 0 {
		// `n+1` border columns flank the n+1 cells (left edge + n
		// inter-cell separators + right edge); subtract them so the
		// data cells share the rest of the panel width evenly.
		room := available - label - (n + 1)
		if perCol := room / n; perCol > value {
			value = perCol
		}
	}
	return label, value
}
