package notification

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOverlay_ninePositions(t *testing.T) {
	base := strings.Repeat(strings.Repeat(".", 10)+"\n", 6)
	base = strings.TrimRight(base, "\n")
	over := "AB\nCD"

	cases := []struct {
		pos     Position
		row     int
		col     int
	}{
		{PositionTopLeft, 0, 0},
		{PositionTopCenter, 0, 4},
		{PositionTopRight, 0, 8},
		{PositionMiddleLeft, 2, 0},
		{PositionCenter, 2, 4},
		{PositionMiddleRight, 2, 8},
		{PositionBottomLeft, 4, 0},
		{PositionBottomCenter, 4, 4},
		{PositionBottomRight, 4, 8},
	}
	for _, c := range cases {
		t.Run(string(c.pos), func(t *testing.T) {
			out := Overlay(base, over, c.pos)
			lines := strings.Split(out, "\n")
			if len(lines) != 6 {
				t.Fatalf("line count drifted: got %d", len(lines))
			}
			if got := substr(lines[c.row], c.col, 2); got != "AB" {
				t.Errorf("row %d col %d: want AB, got %q (line=%q)", c.row, c.col, got, lines[c.row])
			}
			if got := substr(lines[c.row+1], c.col, 2); got != "CD" {
				t.Errorf("row %d col %d: want CD, got %q (line=%q)", c.row+1, c.col, got, lines[c.row+1])
			}
		})
	}
}

func TestOverlay_preservesANSI(t *testing.T) {
	base := strings.Repeat(".", 12) + "\n" + strings.Repeat(".", 12)
	red := "\x1b[31mHI\x1b[0m"
	out := Overlay(base, red, PositionTopLeft)
	if !strings.Contains(out, "\x1b[31m") {
		t.Errorf("ANSI start escape lost: %q", out)
	}
	if !strings.Contains(out, "\x1b[0m") {
		t.Errorf("ANSI reset escape lost: %q", out)
	}
	first := strings.SplitN(out, "\n", 2)[0]
	if ansi.StringWidth(first) != 12 {
		t.Errorf("base line width drifted: got %d, want 12", ansi.StringWidth(first))
	}
}

func TestOverlay_clipsLargerThanBase(t *testing.T) {
	base := strings.Repeat(".", 4) + "\n" + strings.Repeat(".", 4)
	over := "WIDESTUFF\nMOREWIDE"
	out := Overlay(base, over, PositionTopLeft)
	for _, line := range strings.Split(out, "\n") {
		if ansi.StringWidth(line) != 4 {
			t.Errorf("clipped line width drifted: got %d, want 4 (line=%q)", ansi.StringWidth(line), line)
		}
	}
	if !strings.HasPrefix(strings.Split(out, "\n")[0], "WIDE") {
		t.Errorf("expected first 4 cells WIDE, got %q", out)
	}
}

func TestOverlay_paintsOpaqueBox(t *testing.T) {
	base := strings.Repeat(strings.Repeat("#", 8)+"\n", 4)
	base = strings.TrimRight(base, "\n")
	over := "AB\nCDE" // ragged: first row 2 cells, second 3
	out := Overlay(base, over, PositionTopLeft)
	first := strings.Split(out, "\n")[0]
	// overlay width = 3, so first row should be "AB " then base padding
	if !strings.HasPrefix(first, "AB ") {
		t.Errorf("expected ragged short row padded with spaces, got %q", first)
	}
}

func TestIsValidPosition(t *testing.T) {
	for _, p := range Positions {
		if !IsValidPosition(string(p)) {
			t.Errorf("Positions[%s] not recognized", p)
		}
	}
	if IsValidPosition("nowhere") {
		t.Errorf("unknown position accepted")
	}
}

// substr extracts the visible substring [col, col+n) of a possibly
// ANSI-styled line. We strip ANSI for asserting on the visible chars
// because Overlay may reorder padding into surrounding parts.
func substr(line string, col, n int) string {
	plain := ansi.Strip(line)
	if col >= len(plain) {
		return ""
	}
	end := col + n
	if end > len(plain) {
		end = len(plain)
	}
	return plain[col:end]
}
