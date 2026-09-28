package entitydetail

import (
	"strings"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionBody is the one section Entity Detail declares. Its ITEMS are the
// composed document's lines, so the arranger owns the one window over them:
// there is no second window inside a pre-fitted item to keep in step with it.
//
// The spec states no SelectFirst and the block states no Cursor, which is the
// whole declaration a body-scroll surface makes. Both cursors resolve to the
// no-selection sentinel, so screenlayout.apply moves the section's OFFSET
// instead of a selection — the same shape the Task Description reader declares.
const sectionBody = screenlayout.ID("entity-detail-body")

func (s Screen) bodySection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionBody, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.composedBlock(frame, canvas.Width())
		},
	}
}

func (s Screen) bodyRoot(frame screenhost.Frame) screengrid.Node {
	section := s.bodySection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

func (s Screen) composedBlock(frame screenhost.Frame, width int) screenlayout.Block {
	kit := frame.Kit()
	valueWidth := width - gridtable.LabelWidth - 7
	if valueWidth < 20 {
		valueWidth = 20
	}
	if valueWidth > 140 {
		valueWidth = 140
	}
	key := entityDetailBodyKey{width: width, rendered: s.rendered}
	inputs := make([]string, 0, 5+2*len(s.payload.Rows))
	inputs = append(inputs, s.payload.Header, s.payload.BodyLabel, s.payload.Body, s.payload.Extra, frame.Text("tui.empty.body"))
	for _, row := range s.payload.Rows {
		inputs = append(inputs, row.Label, row.Value)
	}
	build := func() screenlayout.Block {
		body := s.payload.Body
		if strings.TrimSpace(body) != "" {
			s.md.Reload(kit.Markdown)
			body = markdown.Body(s.md, body, valueWidth, s.rendered)
		}
		if strings.TrimSpace(body) == "" {
			body = kit.Styles.Hint.Render(frame.Text("tui.empty.body"))
		}
		detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).Custom(gridtable.Styled(screenkit.Kicker(kit.Styles.HintAccent, screenkit.Sanitize(s.payload.Header))))
		for _, row := range s.payload.Rows {
			detail = detail.Row(screenkit.Sanitize(row.Label), screenkit.Sanitize(row.Value))
		}
		detail = detail.Kicker(screenkit.Sanitize(s.payload.BodyLabel)).Span(gridtable.Styled(body))
		if s.payload.Extra != "" {
			detail = detail.Span(gridtable.Styled(kit.Styles.Hint.Render(screenkit.Sanitize(s.payload.Extra))))
		}
		return screenlayout.Block{Items: framed.Document(detail.View(kit.Styles.Border))}
	}
	if s.body == nil {
		return build()
	}
	return s.body.memo.Block(key, inputs, build)
}

func (s Screen) syncBodyWindow(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.WithFocus(sectionBody).Resync(kit, screenlayout.HostBox(kit), s.bodyRoot(frame))
	return s
}
