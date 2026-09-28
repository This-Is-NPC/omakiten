package screenlayout

import "omakiten/internal/tui/components/screenkit"

// hostFooterRows is the rows the host paints BELOW every screen body: a
// separator newline and the indented keybinding row.
const hostFooterRows = 2

// leadingBlankRows is the blank row every screen body opens with. Arrange
// spends it itself and emits it, so a screen returns Result.View verbatim
// rather than prefixing a "\n" the budget did not know about.
const leadingBlankRows = 1

// BodyRows is the terminal rows the host has for a screen body at this
// geometry, INCLUDING the leading blank row.
//
// # Why this is not Kit.ViewportRows
//
// Kit.ViewportRows returns 0 for a terminal under four rows, and every existing
// caller reads that 0 as "render everything and let the host clamp". That
// sentinel is why screens overdraw below roughly twenty rows, and it is
// recorded as an open question on task #2442.
//
// This package cannot inherit it. The arranger's whole guarantee is that a body
// never paints a row the terminal does not have, and a budget that means
// "unlimited" at its lowest value cannot express that. So screenlayout derives
// the number itself, and 0 here means ZERO ROWS.
//
// # Zero rows versus an unmeasured terminal
//
// Kit.Rows applies screenkit's unmeasured-terminal assumption, so a body
// arranged before the host's first WindowSizeMsg is laid out at an assumed
// height rather than reported as zero rows and painted as nothing. That is a
// DIFFERENT case from a terminal of one to four rows, which really has no room
// and really does get zero — see TestAnUnmeasuredTerminalStillGetsABody and
// TestBodyRowsIsZeroNotUnlimitedOnATinyTerminal, which pin the two halves apart.
//
// It is a second derivation of the same quantity, which is normally the defect
// this package exists to remove — so the divergence is pinned rather than
// assumed: TestBodyRowsAgreesWithKitViewportRowsAboveTheSentinel asserts the two
// agree exactly (modulo the leading blank row ViewportRows subtracts and Arrange
// spends) at every geometry where ViewportRows is defined. The sentinel is the
// only difference, and it is the difference on purpose.
func BodyRows(kit screenkit.Kit) int {
	return max(0, kit.Rows()-kit.ChromeRows-hostFooterRows)
}

// distributeRows assigns each stacked section its rows out of `available`.
//
// Minimums first, then the surplus by weight up to each section's maximum.
// MinRows is a hard floor: when the minimums do not fit, sections are dropped
// from the bottom up until the remaining floors fit — a kept section is never
// driven below MinRows and then "surviving short". A zero assignment means
// dropped.
//
// Returns one entry per input spec; a zero means dropped.
func distributeRows(specs []Spec, available int) []int {
	assigned := make([]int, len(specs))
	if len(specs) == 0 || available <= 0 {
		return assigned
	}

	// Drop from the bottom until every kept section's floor fits. Leading
	// sections (form, header) survive a short terminal at the expense of the
	// feed below them — same priority the old soft shrink encoded by breaking
	// ties toward the last section, without ever painting a section short.
	kept := len(specs)
	for kept > 0 {
		need := 0
		for i := 0; i < kept; i++ {
			need += specs[i].MinRows
		}
		if need <= available {
			break
		}
		kept--
	}

	total := 0
	for i := 0; i < kept; i++ {
		assigned[i] = specs[i].MinRows
		total += specs[i].MinRows
	}

	shares := make([]share, len(specs))
	for i, s := range specs {
		shares[i] = share{floor: s.MinRows, ceiling: s.MaxRows, weight: s.Weight, eligible: assigned[i] > 0}
	}
	spreadSurplus(assigned, shares, available-total)
	return assigned
}

// share is one entry in a [spreadSurplus] distribution: the floor it starts
// from, the ceiling it may not pass (0 = unbounded), its weight in the split,
// and whether it may receive anything at all.
type share struct {
	floor    int
	ceiling  int
	weight   int
	eligible bool
}

// spreadSurplus hands `surplus` units out ONE AT A TIME to whichever eligible
// entry is furthest behind its weighted share, and stops when every entry has
// reached its ceiling.
//
// Unit-at-a-time rather than a computed share per entry, because a computed
// share has to be clamped against the ceiling, against the remaining budget and
// against a rounding floor — three clamps whose interactions are exactly the
// kind of arithmetic that is only ever right by accident. Incrementally there is
// one rule and it is exact; the loop runs at most `surplus` times over a handful
// of entries.
//
// Leftover units when every entry is capped simply go unspent, which is legal:
// the contract is "never more than the budget".
//
// One implementation, three callers — the stacked row split, the side-by-side
// column split and [reclaimSlack] — because three copies of a weighted
// distribution is three chances to drift, and this package exists to delete the
// second copy of a number, not to add two more.
func spreadSurplus(assigned []int, shares []share, surplus int) {
	for surplus > 0 {
		pick := -1
		bestShort, bestWeight := 0, 1
		for i, s := range shares {
			if !s.eligible || (s.ceiling > 0 && assigned[i] >= s.ceiling) {
				continue
			}
			// How far this entry is past its floor, per unit of weight.
			// Cross-multiplied to stay in integers.
			short := assigned[i] - s.floor + 1
			if pick < 0 || short*bestWeight < bestShort*s.weight {
				pick, bestShort, bestWeight = i, short, s.weight
			}
		}
		if pick < 0 {
			return
		}
		assigned[pick]++
		surplus--
	}
}

func atMax(s Spec, rows int) bool { return s.MaxRows > 0 && rows >= s.MaxRows }

// sectionWidth is the content columns a STACKED section gets: the whole
// available width, unless it is FIXED — MinWidth == MaxWidth, the same signal
// a fixed-height section states with MinRows == MaxRows.
//
// A comfortable-reading cap that is not fixed (Feed's 96-column ceiling) is a
// COLUMN rule: it says what the section is worth beside a sibling, and there
// is no sibling once the body has stacked. Applying it here was the arranger
// enforcing a column cap on a path with no columns, and it painted a stacked
// zone visibly narrower than the siblings it no longer shares a column with.
func sectionWidth(s Spec, available int) int {
	if s.MaxWidth > 0 && s.MaxWidth == s.MinWidth {
		return min(available, s.MaxWidth)
	}
	return available
}

// defaultColumnGap is the blank column between side-by-side sections when no
// section declares one. It is what shipped, so a body that says nothing about
// its gap is laid out exactly as it was before [Spec.ColumnGap] existed.
const defaultColumnGap = 1

// columnGapOf is the one derivation of a body's column gap: the widest any
// section declared, or the default when none did.
//
// One number for the whole body, and one place it comes from — the arithmetic
// that charges for the gap, the breakpoint that must afford it and the padding
// that paints it all read this, so they cannot disagree about how wide it is.
func columnGapOf(specs []Spec) int {
	gap := defaultColumnGap
	for _, s := range specs {
		gap = max(gap, s.ColumnGap)
	}
	return gap
}

// columnsOf folds the sections into the COLUMNS a side-by-side body will have:
// one per ungrouped section, and one per distinct [Spec.Group], each at the
// position its first member was declared at. Every entry is the indices of that
// column's members, in declaration order.
//
// It is the one derivation of "how many columns are there", and both the width
// arithmetic and the painting read it — so the gap the breakpoint charges for
// and the gaps the body paints are counted the same way.
func columnsOf(specs []Spec) [][]int {
	columns := make([][]int, 0, len(specs))
	at := make(map[ID]int, len(specs))
	for i, s := range specs {
		if group, seen := at[s.Group]; seen && s.Group != "" {
			columns[group] = append(columns[group], i)
			continue
		}
		if s.Group != "" {
			at[s.Group] = len(columns)
		}
		columns = append(columns, []int{i})
	}
	return columns
}

// specsAt is one column's member specs, in declaration order.
func specsAt(specs []Spec, members []int) []Spec {
	out := make([]Spec, len(members))
	for i, at := range members {
		out[i] = specs[at]
	}
	return out
}

// groupColumnSpec is the single Spec a column of stacked members is SIZED by.
//
// A column has one width and its members declare it jointly, so the fold is one
// sentence: the tightest floor, the tightest ceiling, and the strongest claim on
// the surplus. No member is ever placed narrower than it asked (the widest
// minimum wins) or wider than it allowed (the narrowest maximum wins), and a
// proportion or a gap any member declared is the column's.
//
// A column of one member is that member, unchanged — which is why a body that
// declares no group is arranged exactly as it was before groups existed.
func groupColumnSpec(members []Spec) Spec {
	column := members[0]
	for _, s := range members[1:] {
		column.MinWidth = max(column.MinWidth, s.MinWidth)
		if s.MaxWidth > 0 && (column.MaxWidth == 0 || s.MaxWidth < column.MaxWidth) {
			column.MaxWidth = s.MaxWidth
		}
		column.Weight = max(column.Weight, s.Weight)
		column.WidthPercent = max(column.WidthPercent, s.WidthPercent)
		column.ColumnGap = max(column.ColumnGap, s.ColumnGap)
		// Column is the opt-in, and it is unanimous: a member that did not ask
		// to be placed beside anything keeps the whole body stacked.
		column.Column = column.Column && s.Column
	}
	if column.MaxWidth > 0 && column.MaxWidth < column.MinWidth {
		column.MaxWidth = column.MinWidth
	}
	return column
}

// columnFloor is the width a column will ACTUALLY take at its narrowest: its
// declared minimum, or — when it declared a proportion — the proportion, since a
// proportional column does not shrink below its share to make room for a
// sibling.
//
// The breakpoint is decided against this rather than against MinWidth alone,
// because a wide proportion honoured by pushing a sibling under its declared
// minimum is the overflow this package exists to make inexpressible.
func columnFloor(s Spec, available int) int {
	if s.WidthPercent > 0 {
		return proportionalWidth(s, available)
	}
	return s.MinWidth
}

// proportionalWidth is `available * pct / 100`, raised to the declared minimum,
// cut to the declared comfortable measure, and never past the available width.
//
// The clamp order is the screens' own: a floor that would exceed the terminal
// still loses to the terminal, which is what `min(max(available*45/100, 44),
// min(96, available))` says when it is read from the inside out.
func proportionalWidth(s Spec, available int) int {
	width := max(available*s.WidthPercent/100, s.MinWidth)
	if s.MaxWidth > 0 {
		width = min(width, s.MaxWidth)
	}
	return min(width, available)
}

// fitsSideBySide reports whether every section opted into a column AND every
// COLUMN's minimum width can be honoured at once, gaps included.
//
// The unit is the column, not the section: grouped members share one column, so
// a body of three sections in two groups is charged for two columns and one gap.
// Fewer than two columns is a stack — which is why a body whose sections are all
// in one group stacks, exactly as a single section always has.
//
// The width comparison is against the honest screenkit.AvailableWidth (#2422),
// which never exceeds the terminal. When it floored at 24 it over-reported a
// narrow terminal by up to four columns, and a breakpoint built on an inflated
// input bakes the overflow into every layout above it.
func fitsSideBySide(specs []Spec, available int) bool {
	columns := columnsOf(specs)
	if len(columns) < 2 {
		return false
	}
	need := columnGapOf(specs) * (len(columns) - 1)
	for _, members := range columns {
		column := groupColumnSpec(specsAt(specs, members))
		if !column.Column || column.MinWidth <= 0 {
			return false
		}
		need += columnFloor(column, available)
	}
	return need <= available
}

// distributeWidths assigns each side-by-side section its columns.
//
// A column that declared a proportion is sized FIRST, out of `available`, and
// takes no further part in the split — a proportion is a claim on the whole
// width, not on what is left of it. Everything else is minimums first and then
// the surplus by weight, up to each section's MaxWidth.
//
// That remainder split is the same distribution as the stacked row split,
// through the same [spreadSurplus] — a column is a row turned ninety degrees and
// there is no reason for the two to round differently. When no section declares
// a proportion or a MaxWidth the columns spend `available` exactly; a section
// that caps itself leaves its refused surplus to the sections that can still
// take it.
func distributeWidths(specs []Spec, available int) []int {
	widths := make([]int, len(specs))
	used := columnGapOf(specs) * (len(specs) - 1)
	shares := make([]share, len(specs))
	for i, s := range specs {
		if s.WidthPercent > 0 {
			widths[i] = proportionalWidth(s, available)
			used += widths[i]
			continue // ineligible: its share is already spent
		}
		widths[i] = s.MinWidth
		used += s.MinWidth
		shares[i] = share{floor: s.MinWidth, ceiling: s.MaxWidth, weight: s.Weight, eligible: true}
	}
	spreadSurplus(widths, shares, available-used)
	return widths
}
