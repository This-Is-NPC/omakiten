package relationshippicker

import (
	"omakiten/internal/tui/components/choice"
	"omakiten/internal/tui/components/dropdown"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionOptions is the one section Relationship Picker declares. MinRows is a
// hard floor (K1): with a single Weight:1 section there is no sibling to drop
// when the HostBox is short — the section simply receives whatever rows the
// panel viewport budgets.
const sectionOptions = screenlayout.ID("relationship-picker-options")

func (s Screen) optionsSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionOptions, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.optionsBlock(frame, canvas.Width(), canvas.Cursor())
		},
	}
}

func (s Screen) root(frame screenhost.Frame) screengrid.Node {
	section := s.optionsSection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

// optionsBlock pins the kicker crown as Header and exposes each option as one
// CapRows-clamped item so scroll is line-based and the crown does not scroll
// away. Cursor is read from the arranger canvas; it is not a second cursor
// authority in the screen.
func (s Screen) optionsBlock(frame screenhost.Frame, width, cursor int) screenlayout.Block {
	kit := frame.Kit()
	width = max(1, width)
	kicker := screenkit.FocusKicker(kit.Styles.HintAccent, s.kicker(frame))
	hint := s.hint(frame)
	if s.confirmingCancel {
		hint = frame.Text("tui.picker.dirty_discard_prompt")
	}
	hintLine := kit.Styles.Hint.Render(hint)
	opts := make([]dropdown.Option, len(s.payload.Options))
	for i, option := range s.payload.Options {
		label := screenkit.Sanitize(option.Label)
		if label == "" {
			label = screenkit.Sanitize(option.Value)
		}
		if option.None {
			label = frame.Text("tui.picker.none_option")
		}
		if option.Create {
			label = frame.Text("tui.picker.create_skill")
		}
		mode := choice.Radio
		if s.payload.Kind == PersonaSkills && !option.Create {
			mode = choice.Checkbox
		}
		opts[i] = dropdown.Option{
			Label:    label,
			Detail:   option.Detail,
			Selected: option.Selected,
			Mode:     mode,
		}
	}
	rows := dropdown.Rows(dropdown.Spec{
		Options: opts,
		Cursor:  cursor,
		Open:    true,
		Join:    dropdown.DetailDash,
	}, kit.CursorMarker(true), kit.CursorMarker(false), kit.Styles.Hint)
	if width < 3 {
		// A terminal too narrow to hold even a 1-column box skips the frame
		// entirely rather than drawing a degenerate one; CapRows is the only
		// clamp left to apply since there is no wrap to do it.
		return screenlayout.Block{
			Header: []string{kicker, hintLine},
			Items:  screenkit.CapRows(rows, max(1, width-panel.Borders)),
		}
	}
	return framed.List(kit.Styles.Border, width, kicker, []string{hintLine}, rows)
}

// panelBox is the geometry ArrangeIn / HandleKeyIn / ResyncIn share: panel
// content width and the PanelChrome viewport (no leading blank — Panel owns
// that row outside the arranged body). Header rows are charged by the
// arranger from Block.Header, not subtracted here.
//
// When the terminal is still unmeasured (Height<=0), Kit.ViewportRows returns
// the historical "0 means unlimited" sentinel. ArrangeIn treats 0 as ZERO rows,
// so HostBox supplies the unmeasured-height assumption instead.
func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().ViewportRows()
	if kit.Height <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	return screenlayout.Box{
		Width: kit.PanelContentWidth(),
		Rows:  rows,
	}
}

// syncOptionsWindow points the grid at the picker's cursor and re-clamps the
// window against live geometry. Call it on enter/resize so a payload change
// cannot leave a stale ceiling on the next keystroke. picker.Scroll is synced
// FROM the grid layout so any remaining picker readers agree with its window.
func (s Screen) syncOptionsWindow(frame screenhost.Frame) Screen {
	kit := frame.Kit()
	s.grid = s.grid.WithFocus(sectionOptions).WithCursor(sectionOptions, s.picker.Cursor).
		Resync(kit, s.panelBox(kit), s.root(frame))
	return s.syncPickerFromGrid()
}

func (s Screen) syncPickerFromGrid() Screen {
	layout := s.grid.Layout()
	if c := layout.Cursor(sectionOptions); c >= 0 {
		s.picker = s.picker.WithCursor(c, len(s.payload.Options), 0)
	}
	s.picker = s.picker.WithScroll(layout.Offset(sectionOptions))
	return s
}
