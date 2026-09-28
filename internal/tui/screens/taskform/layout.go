package taskform

import (
	"fmt"
	"strings"

	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionForm is the one section Task Form declares to the arranger. The form
// used to paint every field into the panel with no row budget (#2443); the
// arranger windows the field lines so the body never past the HostBox.
const sectionForm = screenlayout.ID("task-form")

func (s Screen) formSection(frame screenhost.Frame, labels Labels) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionForm, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems, SelectFirst: true,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			width := taskFormWidth(canvas.Width())
			form := s.form.resize(width, s.deps.Theme)
			header := s.formHeader(frame)
			items := formFieldItems(form.view(width, labels, s.deps.Theme))
			return screenlayout.Block{
				Header: header,
				Items:  items,
				Cursor: screenlayout.At(activeFormItem(items)),
			}
		},
	}
}

func (s Screen) formRoot(frame screenhost.Frame, labels Labels) screengrid.Node {
	section := s.formSection(frame, labels)
	return screengrid.Cell(section.Def, section.Body)
}

func taskFormWidth(boxWidth int) int {
	width := boxWidth - 8
	if width < 32 {
		return 32
	}
	if width > 120 {
		return 120
	}
	return width
}

func (s Screen) formHeader(frame screenhost.Frame) []string {
	kit := frame.Kit()
	kicker := s.payload.Kicker
	if kicker == "" {
		kicker = text(frame, "tui.kicker.new_task", "New task")
		if s.payload.Mode == Edit {
			kicker = fmt.Sprintf(text(frame, "tui.kicker.edit_task_fmt", "Edit task · #%d"), s.payload.TaskID)
		}
	}
	hint := strings.Join([]string{
		text(frame, "tui.form.hint.ctrl_s_saves", "ctrl+s saves"),
		text(frame, "tui.form.hint.tab_switches_field", "tab switches field"),
		text(frame, "tui.form.hint.esc_cancels", "esc cancels"),
	}, " · ")
	if s.form.confirmingDiscard() {
		hint = text(frame, "tui.taskedit.dirty_discard_prompt", "Unsaved changes. Press esc again to discard.")
	}
	header := []string{screenkit.Kicker(kit.Styles.Info, kicker), kit.Styles.Hint.Render(hint)}
	if s.err != "" && s.form.parentError != s.err {
		header = append(header, kit.Styles.Error.Render(screenkit.Sanitize(s.err)))
	}
	header = append(header, "")
	return header
}

// formFieldItems splits the form view into whole fields so scrolling never
// cuts a bordered box in half. Field labels are lines whose trimmed text starts
// with "// "; blank lines between fields stay attached to the field above.
func formFieldItems(formView string) []string {
	lines := strings.Split(formView, "\n")
	items := make([]string, 0, 5)
	cur := make([]string, 0, 8)
	flush := func() {
		if len(cur) == 0 {
			return
		}
		items = append(items, strings.Join(cur, "\n"))
		cur = cur[:0]
	}
	for _, line := range lines {
		if isFormFieldLabel(line) && len(cur) > 0 {
			flush()
		}
		cur = append(cur, line)
	}
	flush()
	if len(items) == 0 {
		return []string{formView}
	}
	return items
}

func isFormFieldLabel(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "// ")
}

func activeFormItem(items []string) int {
	for i, item := range items {
		if strings.Contains(item, "> // ") {
			return i
		}
	}
	return 0
}

func (s Screen) labels(frame screenhost.Frame) Labels {
	labels := s.deps.Labels
	if labels.Title != "" {
		return labels
	}
	return Labels{
		Title:       text(frame, "tui.form.label.title", "Title"),
		Description: text(frame, "tui.form.label.description", "Description"),
		Priority:    text(frame, "tui.form.label.priority", "Priority"),
		Tags:        text(frame, "tui.form.label.tags", "Tags"),
		Parent:      text(frame, "tui.form.label.parent", "Parent"),
	}
}

// syncFormWindow re-measures the body so the next View windows the field the
// user is editing. It resyncs THE GRID, not a bare screenlayout.State lifted
// out of it: the grid is the single entry to this body, and a local arranger
// state pushed back through WithLayout also discarded the frame memo the next
// keystroke would have reused.
//
// No cursor is seeded here. The section STATES its cursor —
// formSection declares Cursor: screenlayout.At(activeFormItem(items)) — and
// screenlayout.Cursor.resolve returns that index whatever is stored, so a
// WithCursor write is read once by the resync that follows it and discarded.
// Resync's own absorb then writes the resolved index back, which is the same
// index the seed would have carried. Focus needs no seeding either: the lone
// cell is the only ring member, and Resync focuses ring[0] when nothing is.
func (s Screen) syncFormWindow(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.Resync(kit, screenlayout.HostBox(kit), s.formRoot(frame, s.labels(frame)))
	return s
}
