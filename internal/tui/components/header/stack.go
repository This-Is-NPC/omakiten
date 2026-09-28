package header

import (
	"strings"
)

// Stack spends a terminal height budget across three named slots: top
// (header / chrome above the body), middle (the screen body or help
// overlay), and bottom (footer). Top and bottom keep their natural
// height; the middle is given the remainder and truncated from its
// bottom when it overdraws.
//
// This replaces the old clampViewToHeight join-then-chop: the cut is an
// explicit charge against the middle slot, so a screen that overdraws
// is no longer pretending the full body "fit". When height is
// non-positive the segments join untouched (same escape hatch the
// clamp had for an unset WindowSizeMsg).
func Stack(height int, top, middle, bottom string) string {
	if height <= 0 {
		return joinSegments(top, middle, bottom)
	}
	topLines := splitLines(top)
	bottomLines := splitLines(bottom)
	middleLines := splitLines(middle)

	budget := height - len(topLines) - len(bottomLines)
	if budget <= 0 {
		// Top+bottom alone fill (or overfill) the terminal. Keep the
		// anchored ends and let the top absorb the cut so something
		// always paints — matching the old footer-only-overflow path.
		joined := append(append([]string{}, topLines...), bottomLines...)
		if len(joined) > height {
			joined = joined[:height]
		}
		return strings.Join(joined, "\n")
	}
	if len(middleLines) > budget {
		middleLines = middleLines[:budget]
	}
	out := make([]string, 0, len(topLines)+len(middleLines)+len(bottomLines))
	out = append(out, topLines...)
	out = append(out, middleLines...)
	out = append(out, bottomLines...)
	return strings.Join(out, "\n")
}

func joinSegments(parts ...string) string {
	nonzero := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		nonzero = append(nonzero, p)
	}
	return strings.Join(nonzero, "\n")
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
