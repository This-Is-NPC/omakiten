// Package notification renders configurable ASCII mascots on top of the
// regular TUI surface. The overlay helper here is the z-order primitive
// every mascot frame goes through; it is intentionally
// component-agnostic so detail screens, the home grid, and tests can
// stitch any pre-rendered string on top of any other.
package notification

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Position names one of the nine fixed anchor points the user can pick
// from in the notification.show hook args. The validator rejects any other
// string at LoadBundle time, so the renderer takes a typed value here
// and never has to fall back to a default.
type Position string

const (
	PositionTopLeft      Position = "top-left"
	PositionTopCenter    Position = "top-center"
	PositionTopRight     Position = "top-right"
	PositionMiddleLeft   Position = "middle-left"
	PositionCenter       Position = "center"
	PositionMiddleRight  Position = "middle-right"
	PositionBottomLeft   Position = "bottom-left"
	PositionBottomCenter Position = "bottom-center"
	PositionBottomRight  Position = "bottom-right"
)

// Positions is the closed set of anchor names. The hooks validator and
// the docs use this list directly so adding a new anchor is a one-line
// change.
var Positions = []Position{
	PositionTopLeft, PositionTopCenter, PositionTopRight,
	PositionMiddleLeft, PositionCenter, PositionMiddleRight,
	PositionBottomLeft, PositionBottomCenter, PositionBottomRight,
}

// IsValidPosition reports whether s names one of the known anchors.
func IsValidPosition(s string) bool {
	for _, p := range Positions {
		if string(p) == s {
			return true
		}
	}
	return false
}

// Overlay paints `over` on top of `base` anchored at `pos`. Each line
// of `over` replaces the visible cells of the corresponding line in
// `base`; ANSI escape codes carried by `over` are preserved verbatim
// while widths are computed in display cells via charmbracelet/x/ansi.
//
// The overlay is treated as an opaque rectangle: shorter overlay rows
// are right-padded with spaces so the base never bleeds through inside
// the overlay's bounding box. Overlay rows past the base height or
// columns past the base width are clipped, never expanded — the
// returned string keeps the exact line count and per-line cell width
// of `base`.
func Overlay(base, over string, pos Position) string {
	if over == "" {
		return base
	}
	baseLines := strings.Split(base, "\n")
	overLines := strings.Split(strings.TrimRight(over, "\n"), "\n")
	if len(overLines) == 0 || (len(overLines) == 1 && overLines[0] == "") {
		return base
	}

	baseWidths := lineWidths(baseLines)
	baseW := maxInt(baseWidths)
	baseH := len(baseLines)

	overWidth := maxInt(lineWidths(overLines))
	overHeight := len(overLines)

	offX, offY := resolveOffset(pos, baseW, baseH, overWidth, overHeight)

	for i, overLine := range overLines {
		row := offY + i
		if row < 0 || row >= baseH {
			continue
		}
		baseLines[row] = spliceLine(baseLines[row], baseWidths[row], padToWidth(overLine, overWidth), overWidth, offX, baseW)
	}
	return strings.Join(baseLines, "\n")
}

func resolveOffset(pos Position, baseW, baseH, overW, overH int) (int, int) {
	var x, y int
	switch pos {
	case PositionTopLeft:
		x, y = 0, 0
	case PositionTopCenter:
		x, y = (baseW-overW)/2, 0
	case PositionTopRight:
		x, y = baseW-overW, 0
	case PositionMiddleLeft:
		x, y = 0, (baseH-overH)/2
	case PositionCenter:
		x, y = (baseW-overW)/2, (baseH-overH)/2
	case PositionMiddleRight:
		x, y = baseW-overW, (baseH-overH)/2
	case PositionBottomLeft:
		x, y = 0, baseH-overH
	case PositionBottomCenter:
		x, y = (baseW-overW)/2, baseH-overH
	case PositionBottomRight:
		x, y = baseW-overW, baseH-overH
	default:
		panic("invalid notification position: " + string(pos))
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// spliceLine replaces a window of `baseLine` (visible cells [offX,
// offX+overWidth)) with `overLine` and pads the result back out to the
// original `baseLineWidth` so the joined output stays rectangular.
// All width math uses ansi.StringWidth so escape codes do not skew
// the offsets.
func spliceLine(baseLine string, baseLineWidth int, overLine string, overWidth, offX, baseW int) string {
	if offX >= baseW {
		return baseLine
	}
	right := offX + overWidth
	if right > baseW {
		// Clip the overlay to the base width.
		overLine = ansi.Truncate(overLine, baseW-offX, "")
		right = baseW
	}

	leftPart := ansi.Truncate(baseLine, offX, "")
	leftPart = padToWidth(leftPart, offX)

	var rightPart string
	if baseLineWidth > right {
		rightPart = ansi.TruncateLeft(baseLine, right, "")
	}

	return leftPart + overLine + rightPart
}

// padToWidth appends spaces to s until ansi.StringWidth(s) == width.
// Strings already wider than width are returned unchanged — the caller
// is responsible for truncating first when that matters.
func padToWidth(s string, width int) string {
	current := ansi.StringWidth(s)
	if current >= width {
		return s
	}
	return s + strings.Repeat(" ", width-current)
}

func lineWidths(lines []string) []int {
	widths := make([]int, len(lines))
	for i, line := range lines {
		widths[i] = ansi.StringWidth(line)
	}
	return widths
}

func maxInt(values []int) int {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
