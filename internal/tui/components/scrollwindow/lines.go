package scrollwindow

// SliceLines clamps scroll to a valid offset for `lines` at the given viewport
// height and returns the visible window plus counts hidden above/below.
// Routes through Slice with HintsNone — the detail-screen
// View renders the combined-footer hint OUTSIDE the slice budget by
// asking callers to pass viewport-1 when they need a footer row, so
// the slicer itself reserves nothing. The pre-clamp preserves the
// "jump to end" behavior callers depend on (sentinel scroll = 1<<20
// resolves to len-viewport, not len-1).
func SliceLines(lines []string, scroll, viewport int) (visible []string, above, below int) {
	if viewport <= 0 || len(lines) <= viewport {
		return lines, 0, 0
	}
	if scroll < 0 {
		scroll = 0
	}
	if maxOffset := len(lines) - viewport; scroll > maxOffset {
		scroll = maxOffset
	}
	heights := make([]int, len(lines))
	for i := range heights {
		heights[i] = 1
	}
	end := Slice(scroll, heights, viewport, HintsNone)
	return lines[scroll:end], Above(scroll), Below(end, len(lines))
}
