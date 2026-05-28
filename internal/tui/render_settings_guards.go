package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/gridtable"
)

// renderSettingsGuards renders the read-only N×N guards matrix surfaced
// under Settings › Guards. Rows = from-bucket; columns = to-bucket; cell
// content reflects the four states declared on the active workflow:
//
//   - diagonal (`from == to`) → sentinel `tui.settings.guards.sentinel_disallowed`
//   - no entry in `workflow.transitions` → same disallowed sentinel
//   - allowed + zero guards → sentinel `tui.settings.guards.sentinel_empty`
//   - allowed + ≥1 guards → comma-separated guard slugs from `snap.Guards`
//
// When the active project configures `subtask_kit` AND the resolved
// sub-kit workflow diverges from the root (bucket ordering, transition
// set, or per-cell guard payload), TWO matrices stack vertically — root
// first (kicker `tui.settings.guards.kicker_root`) then sub-kit
// (kicker `tui.settings.guards.kicker_subtask`). Identical kits OR
// absent `subtask_kit` collapse back to the single-matrix render with
// the original `tui.settings.guards_tab` kicker so projects without
// cascade overrides are visually unchanged.
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

// renderSettingsGuardsBody builds the body block, choosing between a
// single-matrix render (no subtask_kit OR root/sub-kit are guard-equal)
// and a dual-matrix render (subtask_kit present AND diverges). Extracted
// so the future key handler (259.4) can measure body height for scroll
// clamping without re-running the renderer's scroll wrapper —
// `renderSettingsGeneral` follows the same split.
func (m Model) renderSettingsGuardsBody() string {
	root := m.repos.activeSnapshot()
	sub, hasSub := root.SubtaskKit()

	if !hasSub || guardsMatricesEqual(root, sub) {
		return m.renderGuardsMatrix(root, m.workflow, m.t("tui.settings.guards_tab"))
	}

	rootMatrix := m.renderGuardsMatrix(root, root.Workflow(), m.t("tui.settings.guards.kicker_root"))
	subMatrix := m.renderGuardsMatrix(sub, sub.Workflow(), m.t("tui.settings.guards.kicker_subtask"))
	return rootMatrix + "\n\n" + subMatrix
}

// renderGuardsMatrix builds a single from/to matrix for the supplied
// snapshot+workflow with the supplied kicker label. Buckets render in
// Position order so the matrix mirrors workflow flow (backlog → done
// for omakase). Guard slugs come from the supplied snapshot when wired;
// absent a snapshot (test fixtures without a runtime cache) the cell
// falls back to the empty sentinel because the transition list alone
// cannot reveal guard payloads.
func (m Model) renderGuardsMatrix(snap *config.Snapshot, workflow domain.Workflow, kickerLabel string) string {
	buckets := orderedBuckets(workflow.Buckets)
	disallowed := m.t("tui.settings.guards.sentinel_disallowed")
	empty := m.t("tui.settings.guards.sentinel_empty")

	transitions := indexTransitions(workflow.Transitions)

	rows := make([][]string, 0, len(buckets)+2)
	rows = append(rows, []string{m.styles.kicker(kickerLabel)})

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

// guardsMatricesEqual returns true when the two snapshots produce
// guard-identical matrices: same bucket ordering (Position + Key), same
// transition set (regardless of declaration order), and same guard
// payload per allowed cell (guard list treated as a set so authors who
// reorder guards in YAML don't trip a false divergence).
//
// A nil sub snapshot is treated as "equal" so the caller short-circuits
// to the single-matrix render — `renderSettingsGuardsBody` already
// guards the nil case via the `hasSub` flag before calling this helper,
// but the defensive return keeps the helper safe to reuse.
func guardsMatricesEqual(a, b *config.Snapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	wa := a.Workflow()
	wb := b.Workflow()

	if !bucketSlicesEqual(orderedBuckets(wa.Buckets), orderedBuckets(wb.Buckets)) {
		return false
	}
	if !transitionSetsEqual(wa.Transitions, wb.Transitions) {
		return false
	}
	return guardPayloadsEqual(a, b, orderedBuckets(wa.Buckets))
}

// bucketSlicesEqual compares two position-ordered bucket slices on
// identity-relevant fields: ID, Key, Position. Two slices are equal
// when length matches AND each index pair shares all three fields.
// Permissions/Name are intentionally excluded — the guards matrix
// reflects flow + guards only; cosmetic label drift between root and
// sub-kit must not trip a divergence.
func bucketSlicesEqual(a, b []domain.Bucket) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Key != b[i].Key || a[i].Position != b[i].Position {
			return false
		}
	}
	return true
}

// transitionSetsEqual compares two transition slices as sets keyed by
// (fromKey, toKey). Order-independent so authors who reshuffle
// transitions in YAML don't trip a divergence.
func transitionSetsEqual(a, b []domain.WorkflowTransition) bool {
	if len(a) != len(b) {
		return false
	}
	idx := indexTransitions(a)
	for _, t := range b {
		if !idx[[2]string{t.FromBucketKey, t.ToBucketKey}] {
			return false
		}
	}
	return true
}

// guardPayloadsEqual walks every (from, to) cell defined by the shared
// bucket set and compares the guard slices on both snapshots. Guards
// are compared as a sorted-by-(Type,Tag,Count) set so list-order drift
// in YAML does not trip a divergence. Returns false on the first cell
// where guard payloads differ.
func guardPayloadsEqual(a, b *config.Snapshot, buckets []domain.Bucket) bool {
	for _, from := range buckets {
		for _, to := range buckets {
			if from.ID == to.ID {
				continue
			}
			ga := normalizeGuards(a.Guards(from.ID, to.ID))
			gb := normalizeGuards(b.Guards(from.ID, to.ID))
			if len(ga) != len(gb) {
				return false
			}
			for i := range ga {
				if !guardsEqual(ga[i], gb[i]) {
					return false
				}
			}
		}
	}
	return true
}

// normalizeGuards returns a sorted copy of the guard slice so the
// equality compare is stable across YAML declaration order. Buckets
// inside each guard are also sorted so authors who list blocker
// buckets in a different order between root and sub-kit don't trip a
// false divergence.
func normalizeGuards(in []domain.TransitionGuard) []domain.TransitionGuard {
	out := make([]domain.TransitionGuard, len(in))
	for i, g := range in {
		copyG := g
		if len(g.Buckets) > 0 {
			copyG.Buckets = append([]string(nil), g.Buckets...)
			sort.Strings(copyG.Buckets)
		}
		out[i] = copyG
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Tag != out[j].Tag {
			return out[i].Tag < out[j].Tag
		}
		if out[i].Count != out[j].Count {
			return out[i].Count < out[j].Count
		}
		return strings.Join(out[i].Buckets, ",") < strings.Join(out[j].Buckets, ",")
	})
	return out
}

// guardsEqual compares two guards on all surfaced fields. Hint is
// included because authors who customize the remediation tip on a
// sub-kit are signalling an intentional divergence the user should see.
func guardsEqual(a, b domain.TransitionGuard) bool {
	if a.Type != b.Type || a.Tag != b.Tag || a.Count != b.Count || a.Hint != b.Hint {
		return false
	}
	if len(a.Buckets) != len(b.Buckets) {
		return false
	}
	for i := range a.Buckets {
		if a.Buckets[i] != b.Buckets[i] {
			return false
		}
	}
	return true
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
