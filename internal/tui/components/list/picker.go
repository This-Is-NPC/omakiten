package list

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/scrollwindow"
)

// PickerMode determines whether enter confirms a single-highlighted row or
// ctrl+s confirms a multi-select set built up via space toggles.
type PickerMode int

const (
	// Single picks one row at a time; enter confirms.
	Single PickerMode = iota
	// Multi accumulates a checkbox set; space toggles, ctrl+s confirms.
	Multi
)

// PickerEvent reports the high-level outcome of the most recent Update.
// PickerNone covers navigation keys (the picker handled them but there
// is nothing for the parent to act on); PickerSelect/PickerToggle/PickerCancel
// hand off to the screen-specific action.
type PickerEvent int

const (
	PickerNone   PickerEvent = iota
	PickerSelect             // enter (Single) or ctrl+s (Multi)
	PickerToggle             // space (Multi only)
	PickerCancel             // esc
)

// Picker owns cursor + scroll state for a single list picker. RowCount
// and Viewport are recomputed each frame by the parent (terminal width
// can change mid-session) and passed to Update; the picker uses them to
// clamp the cursor and recompute scroll without holding stale geometry
// across frames.
type Picker struct {
	Cursor int
	Scroll int
	Mode   PickerMode

	lastEvent PickerEvent
}

// NewPicker returns a fresh picker in the given mode with cursor and scroll
// at zero. Callers can immediately set Cursor to a different starting
// row (e.g. open the persona picker on the persona's current selection).
func NewPicker(mode PickerMode) Picker {
	return Picker{Mode: mode}
}

// Init satisfies the Bubble Tea Model interface; pickers do no async
// work at construction.
func (m Picker) Init() tea.Cmd { return nil }

// Update consumes a key message and returns the new picker state plus a
// nil cmd. rowCount and viewport are passed inline rather than stored
// on the model so the parent can recompute geometry per frame; this also
// keeps the picker stateless w.r.t. row data so two screens can share
// the same component definition without colliding.
//
// Returns (model, nil) — picker actions never spawn Cmds; the parent
// reads LastEvent() and dispatches the screen-specific action itself.
func (m Picker) Update(msg tea.Msg, rowCount, viewport int) (Picker, tea.Cmd) {
	m.lastEvent = PickerNone
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if cursor, handled := navKey(keyMsg, m.Cursor, rowCount, viewport); handled {
		m.Cursor = cursor
		m.Scroll = followCursor(m.Scroll, cursor, viewport, rowCount)
		return m, nil
	}

	switch keyMsg.String() {
	case "esc":
		m.lastEvent = PickerCancel
	case "enter":
		if m.Mode == Single {
			m.lastEvent = PickerSelect
		}
	case "ctrl+s":
		if m.Mode == Multi {
			m.lastEvent = PickerSelect
		}
	case " ", "space":
		if m.Mode == Multi {
			m.lastEvent = PickerToggle
		}
	}
	return m, nil
}

// LastEvent returns the high-level action signalled by the most recent
// Update — parents should check this on every Update to drive their own
// state machine (close screen on Cancel, save on Select, toggle on Toggle).
func (m Picker) LastEvent() PickerEvent { return m.lastEvent }

// WithCursor returns a copy of m with the cursor jumped to idx and the
// scroll re-followed so the new cursor sits inside the visible window.
// rowCount and viewport are passed inline (same shape as Update) so the
// picker stays stateless w.r.t. row data — the parent owns geometry per
// frame.
//
// Clamps idx to [0, rowCount-1]; rowCount <= 0 collapses cursor + scroll
// to 0. Used by parent screens that drive the cursor through an external
// authoritative field (e.g. settings_picker setting the cursor onto the
// currently-active entity at open time).
func (m Picker) WithCursor(idx, rowCount, viewport int) Picker {
	if rowCount <= 0 {
		m.Cursor = 0
		m.Scroll = 0
		return m
	}
	if idx < 0 {
		idx = 0
	}
	if idx > rowCount-1 {
		idx = rowCount - 1
	}
	m.Cursor = idx
	m.Scroll = followCursor(m.Scroll, idx, viewport, rowCount)
	return m
}

// WithScroll returns a copy of m with the scroll offset re-clamped to
// keep the cursor inside the viewport. Used by surfaces that re-derive
// scroll from a variable-height layout (the home grid does this every
// frame because project cards have differing heights, so the
// `viewport` here is the row budget the caller already computed via
// scrollwindow.Follow on its own heights slice).
func (m Picker) WithScroll(scroll int) Picker {
	if scroll < 0 {
		scroll = 0
	}
	m.Scroll = scroll
	return m
}

// View is intentionally absent from this component — every picker in
// omakiten renders its rows differently (custom badges, sticky "+ create
// new" affordance, active dots) so a one-size-fits-all View would force
// callers into an awkward "row builder callback" API. Parents render
// their own rows and read m.Cursor / m.Scroll for the marker + viewport
// slice. See picker_test.go for the exact behaviours guaranteed.

// navKey routes the navigation keys shared by every list-picker —
// up/down/k/j/pgup/pgdn/ctrl+u/ctrl+d/home/g/end/G — into a single new
// cursor value. Returns (newCursor, true) when the key is a recognised
// navigation key, or (cursor, false) so callers can fall through to
// picker-specific keys (space, enter, ctrl+s, esc).
func navKey(key tea.KeyMsg, cursor, rowCount, viewport int) (int, bool) {
	if rowCount <= 0 {
		return 0, false
	}
	switch key.String() {
	case "up", "k":
		if cursor > 0 {
			cursor--
		}
	case "down", "j":
		if cursor < rowCount-1 {
			cursor++
		}
	case "pgup", "ctrl+u":
		cursor -= pageStep(viewport)
	case "pgdown", "ctrl+d":
		cursor += pageStep(viewport)
	case "home", "g":
		cursor = 0
	case "end", "G":
		cursor = rowCount - 1
	default:
		return cursor, false
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > rowCount-1 {
		cursor = rowCount - 1
	}
	return cursor, true
}

// followCursor returns the new scroll offset that keeps `cursor` inside
// the window the renderer will actually paint for `viewport` terminal
// rows. Returns 0 when content fits or viewport is non-positive — caller
// can use that to skip rendering indicator rows entirely.
//
// `viewport` is the renderer's full row budget, not a pre-shrunk data
// window: the shared scroll window (scrollwindow.Slice under HintsSplit)
// reserves one or two of those rows for the ▲/▼ indicators. Delegating to
// scrollwindow.Follow is what makes the follow agree with that reservation
// — the previous hand-rolled
// `cursor - viewport + 1` / `total - viewport` pair assumed a full
// viewport of rows and left the last options unreachable even on end/G.
func followCursor(scroll, cursor, viewport, total int) int {
	if viewport <= 0 || total <= viewport {
		return 0
	}
	scroll = scrollwindow.Follow(scroll, cursor, scrollwindow.UnitHeights(total), viewport, scrollwindow.HintsSplit)
	if scroll < 0 {
		scroll = 0
	}
	// Follow only ever advances, so a stale offset carried in from a
	// larger list or a smaller terminal still needs the shared ceiling.
	if max := scrollwindow.MaxOffset(total, viewport, scrollwindow.HintsSplit); scroll > max {
		scroll = max
	}
	return scroll
}
