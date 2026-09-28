package commentdetail

import (
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/screenstate"
	"omakiten/internal/tui/screenhost"
)

// sectionBody is the one section Comment Detail declares, and the two modes
// mount on it. Read mode hands the arranger the comment's LINES so the one
// window over them is the arranger's; edit mode puts the growing header in
// Block.Header so the textarea budget is whatever rows remain (#2444).
//
// The spec states no SelectFirst: the reader is a body-scroll surface, and a
// seed of 0 would give it a selection it has no use for. Edit mode is unmoved
// by that — it declares Cursor: At(0) on its own block, which resolves the same
// whatever the seed was.
const sectionBody = screenlayout.ID("comment-body")
const sectionState = screenlayout.ID("comment-state")

func (s Screen) bodySection(frame screenhost.Frame) screenlayout.Func {
	scroll := screenlayout.ScrollItems
	if s.mode == ModeEdit {
		// The textarea owns caret motion and its internal viewport. The mode is
		// still mounted as a grid Cell, but the grid must not become a second
		// editor navigation engine.
		scroll = screenlayout.ScrollNone
	}
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionBody, MinRows: 1, Weight: 1,
			Scroll: scroll,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			if s.mode == ModeEdit {
				return s.editBlock(frame, canvas)
			}
			return s.readBlock(frame, canvas)
		},
	}
}
func (s Screen) root(frame screenhost.Frame) screengrid.Node {
	if s.stateActive(frame) {
		return s.stateRoot(frame)
	}
	section := s.bodySection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

func (s Screen) stateActive(frame screenhost.Frame) bool {
	return (s.mode != ModeEdit && s.err != nil) || s.loading || (s.mode != ModeEdit && s.payload.Comment.ID == 0)
}

func (s Screen) stateSection(frame screenhost.Frame) screenlayout.Func {
	kit := frame.Kit()
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionState, MinRows: 1},
		Body: func(screenlayout.Canvas) screenlayout.Block {
			body, _ := screenstate.Resolve(kit, kit.PanelContentWidth(), s.states(frame)...)
			return screenlayout.Block{Header: framed.Document(kit.Panel(body)), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s Screen) stateRoot(frame screenhost.Frame) screengrid.Node {
	section := s.stateSection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

func (s Screen) editBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	kit := frame.Kit()
	header := []string{
		kit.Styles.Kicker(fmt.Sprintf(frame.Text("tui.kicker.edit_comment_fmt"), s.payload.Comment.ID)),
		frame.Text("tui.form.hint.ctrl_s_saves") + " · " + frame.Text("tui.form.hint.alt_newline") + " · " + frame.Text("tui.form.hint.esc_cancels"),
	}
	if s.err != nil {
		header = append(header, kit.Styles.Error.Render(screenkit.Sanitize(s.err.Error())))
	}
	header = append(header, "")
	editHeight := max(1, canvas.Rows()-len(header))
	width := field.WidthFor(canvas.Width(), s.deps.EditTheme)
	if width < 12 {
		width = 12
	}
	field.Resize(&s.input, width, editHeight, s.deps.EditTheme)
	area := field.RenderArea(s.input, width, editHeight, true, s.deps.EditTheme)
	return screenlayout.Block{
		Header: header,
		Items:  []string{area},
		Cursor: screenlayout.At(0),
	}
}

func (s Screen) readBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	kit := frame.Kit()
	comment := s.payload.Comment
	valueWidth := canvas.Width() - gridtable.LabelWidth - 3
	if valueWidth < 24 {
		valueWidth = 24
	}
	detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).Custom(gridtable.Styled(kit.Styles.Kicker(fmt.Sprintf(frame.Text("tui.kicker.comment_fmt"), comment.ID))))
	if comment.Scope == "" || comment.Scope == domain.CommentScopeTask {
		detail = detail.Row(frame.Text("tui.row.task"), fmt.Sprintf("#%d", comment.TaskID))
	}
	detail = detail.Row(frame.Text("tui.row.author"), strings.TrimSpace(comment.AuthorType)).Row(frame.Text("tui.row.when"), strings.TrimSpace(comment.CreatedAt))
	if len(comment.Tags) > 0 {
		names := make([]string, len(comment.Tags))
		for i, tag := range comment.Tags {
			names[i] = tag.Label
		}
		detail = detail.Row(frame.Text("tui.row.tags"), strings.Join(names, " · "))
	}
	body := strings.TrimSpace(comment.Body)
	if body == "" {
		body = kit.Styles.Hint.Render(frame.Text("tui.comment.empty"))
	} else {
		s.md.Reload(kit.Markdown)
		body = markdown.Body(s.md, body, valueWidth, s.rendered)
	}
	// One window, and it is the arranger's: the composed comment goes in as the
	// lines it is made of, and screenlayout.apply moves this section's OFFSET.
	//
	// NoSelection() is stated rather than left to the zero Cursor, because this
	// entry is SHARED with edit mode, whose block declares At(0). An inherited
	// cursor would let that 0 survive back into read mode and put the reader on
	// the cursor branch of apply, which is the one thing this migration is
	// removing.
	return screenlayout.Block{
		Items:  framed.Document(detail.Kicker(frame.Text("tui.kicker.body")).Span(gridtable.Styled(body)).View(kit.Styles.Border)),
		Cursor: screenlayout.NoSelection(),
	}
}

func (s Screen) syncBodyWindow(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.Resync(kit, screenlayout.HostBox(kit), s.root(frame))
	return s
}
