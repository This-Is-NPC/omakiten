package description

import (
	"fmt"
	"strings"

	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// bodySpec is the one section Task Description declares, geometry included.
// The arranger windows the lines; the screen does not. The Spec is stated here
// rather than at the New call because a single-zone screen says so, and says it
// once.
var bodySpec = screenlayout.Spec{ID: "description-body", MinRows: 1, Weight: 1, Scroll: screenlayout.ScrollItems}

func (s Screen) composeRead(frame screenhost.Frame, width int) []string {
	kit := frame.Kit()
	valueWidth := width - gridtable.LabelWidth - 3
	if valueWidth < 24 {
		valueWidth = 24
	}
	detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).
		Custom(gridtable.Styled(kit.Styles.Kicker(fmt.Sprintf(frame.Text("tui.kicker.description_fmt"), s.task.ID)))).
		Row(frame.Text("tui.row.title"), s.task.Title).
		Row(frame.Text("tui.row.bucket"), s.task.BucketKey).
		Kicker(frame.Text("tui.kicker.description"))
	body := strings.TrimSpace(s.task.Description)
	if body == "" {
		body = kit.Styles.Hint.Render(frame.Text("tui.empty.task_no_description"))
	} else {
		s.md.Reload(kit.Markdown)
		body = markdown.Body(s.md, s.task.Description, valueWidth, s.rendered)
	}
	lines := strings.Split(detail.Span(gridtable.Styled(body)).View(kit.Styles.Border), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}
