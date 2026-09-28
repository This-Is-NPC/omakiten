package plannetwork

import (
	"strings"

	networkprojection "omakiten/internal/plannetwork"
	"omakiten/internal/tui/components/screenkit"
)

// lane paints the fixed-width filament block for one outline row. Each lane is
// one character wide; a final trailing slot carries the horizontal arm head:
// "►" on rows where a filament branches or terminates, "─" on source rows, " "
// on rows with no filament touch.
//
// style is the muted tone the plan network paints filaments in — handed in
// rather than read from Styles, because muted is not in the screen contract.
func lane(styles screenkit.Styles, rowIdx int, filaments []networkprojection.Filament, laneCount int) string {
	if laneCount <= 0 {
		return ""
	}
	cells := make([]string, laneCount)
	for i := range cells {
		cells[i] = " "
	}
	markLaneVerticals(cells, rowIdx, filaments)
	hasSrc, hasDst := paintLaneGlyphs(cells, rowIdx, filaments)
	trailing := " "
	switch {
	case hasDst:
		trailing = "►"
	case hasSrc:
		trailing = "─"
	}
	return styles.Hint.Render(strings.Join(cells, "") + trailing)
}

func markLaneVerticals(cells []string, rowIdx int, filaments []networkprojection.Filament) {
	for _, f := range filaments {
		end := f.EndRow()
		if rowIdx > f.SrcRow && rowIdx < end {
			cells[f.Lane] = "│"
		}
	}
}

func paintLaneGlyphs(cells []string, rowIdx int, filaments []networkprojection.Filament) (hasSrc, hasDst bool) {
	for _, f := range filaments {
		glyph, source, destination := laneGlyph(f, rowIdx)
		if glyph == "" {
			continue
		}
		cells[f.Lane] = glyph
		extendLaneArm(cells, f.Lane)
		hasSrc = hasSrc || source
		hasDst = hasDst || destination
	}
	return hasSrc, hasDst
}

func laneGlyph(f networkprojection.Filament, rowIdx int) (glyph string, source, destination bool) {
	switch {
	case rowIdx == f.SrcRow:
		return "┌", true, false
	case rowIdx <= f.SrcRow || rowIdx > f.EndRow():
		return "", false, false
	}
	for i, row := range f.DstRows {
		if row == rowIdx {
			if i == len(f.DstRows)-1 {
				return "└", false, true
			}
			return "├", false, true
		}
	}
	return "", false, false
}

func extendLaneArm(cells []string, lane int) {
	for i := lane + 1; i < len(cells); i++ {
		switch cells[i] {
		case "│":
			cells[i] = "┼"
		case " ":
			cells[i] = "─"
		}
	}
}
