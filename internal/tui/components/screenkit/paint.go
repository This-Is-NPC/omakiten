package screenkit

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PaintColumn paints every entry through style in ONE Render call, in place.
//
// lipgloss resolves a style into terminal codes per Render call and then
// applies them line by line, so a column painted as one block carries exactly
// the codes N separate calls would have produced — at one resolution for the
// whole box instead of one per row. The block form also right-pads every line
// out to the widest one, because lipgloss aligns any multi-line render; that
// pad is the one thing to undo, which is why entries here are labels, counts
// and meta tails that never end in a space.
//
// The painted lines are written back over entries rather than returned in a new
// slice, and a column of one skips the block entirely: a column is a handful of
// rows, so the allocations around the paint are the same order as the paint.
//
// It lives here because five surfaces had written it — components/header,
// components/tokenstrip, components/overlay, screens/taskdetail and
// screens/projectresume — and the two in screens/ were a screen owning a paint
// policy. One implementation, in the package that owns the Styles they paint
// with.
func PaintColumn(style lipgloss.Style, entries []string) []string {
	switch len(entries) {
	case 0:
		return nil
	case 1:
		entries[0] = style.Render(entries[0])
		return entries
	}
	block := style.Render(strings.Join(entries, "\n"))
	for i := range entries {
		line := block
		if cut := strings.IndexByte(block, '\n'); cut >= 0 {
			line, block = block[:cut], block[cut+1:]
		}
		entries[i] = strings.TrimRight(line, " ")
	}
	return entries
}
