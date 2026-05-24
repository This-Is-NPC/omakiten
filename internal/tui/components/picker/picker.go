// Package picker encapsulates the cursor + scroll state and key dispatch
// shared by every list picker in omakiten's TUI (blocker, theme, config,
// template-default, persona). The component owns navigation; row data
// and rendering stay with the parent screen so each picker can keep its
// own custom row layout (checkboxes, "+ create new" affordance, active
// dot, badges).
//
// Selection actions surface as Event values rather than tea.Cmd messages
// so the parent can dispatch synchronously inside its own Update — async
// Cmd round-trips would land the action on the next tick and the user
// would feel a one-frame lag.
package picker

import tea "github.com/charmbracelet/bubbletea"

// Mode determines whether enter confirms a single-highlighted row or
// ctrl+s confirms a multi-select set built up via space toggles.
type Mode int

const (
	// Single picks one row at a time; enter confirms.
	Single Mode = iota
	// Multi accumulates a checkbox set; space toggles, ctrl+s confirms.
	Multi
)

// Event reports the high-level outcome of the most recent Update.
// EventNone covers navigation keys (the picker handled them but there
// is nothing for the parent to act on); EventSelect/EventToggle/EventCancel
// hand off to the screen-specific action.
type Event int

const (
	EventNone   Event = iota
	EventSelect       // enter (Single) or ctrl+s (Multi)
	EventToggle       // space (Multi only)
	EventCancel       // esc
)

// Model owns cursor + scroll state for a single list picker. RowCount
// and Viewport are recomputed each frame by the parent (terminal width
// can change mid-session) and passed to Update; the picker uses them to
// clamp the cursor and recompute scroll without holding stale geometry
// across frames.
type Model struct {
	Cursor int
	Scroll int
	Mode   Mode

	lastEvent Event
}

// New returns a fresh picker in the given mode with cursor and scroll
// at zero. Callers can immediately set Cursor to a different starting
// row (e.g. open the persona picker on the persona's current selection).
func New(mode Mode) Model {
	return Model{Mode: mode}
}

// Init satisfies the Bubble Tea Model interface; pickers do no async
// work at construction.
func (m Model) Init() tea.Cmd { return nil }

// Update consumes a key message and returns the new picker state plus a
// nil cmd. rowCount and viewport are passed inline rather than stored
// on the model so the parent can recompute geometry per frame; this also
// keeps the picker stateless w.r.t. row data so two screens can share
// the same component definition without colliding.
//
// Returns (model, nil) — picker actions never spawn Cmds; the parent
// reads LastEvent() and dispatches the screen-specific action itself.
func (m Model) Update(msg tea.Msg, rowCount, viewport int) (Model, tea.Cmd) {
	m.lastEvent = EventNone
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
		m.lastEvent = EventCancel
	case "enter":
		if m.Mode == Single {
			m.lastEvent = EventSelect
		}
	case "ctrl+s":
		if m.Mode == Multi {
			m.lastEvent = EventSelect
		}
	case " ", "space":
		if m.Mode == Multi {
			m.lastEvent = EventToggle
		}
	}
	return m, nil
}

// LastEvent returns the high-level action signalled by the most recent
// Update — parents should check this on every Update to drive their own
// state machine (close screen on Cancel, save on Select, toggle on Toggle).
func (m Model) LastEvent() Event { return m.lastEvent }

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
func (m Model) WithCursor(idx, rowCount, viewport int) Model {
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
func (m Model) WithScroll(scroll int) Model {
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
// the visible window of length `viewport`. Returns 0 when content fits
// or viewport is non-positive — caller can use that to skip rendering
// indicator rows entirely.
func followCursor(scroll, cursor, viewport, total int) int {
	if viewport <= 0 || total <= viewport {
		return 0
	}
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+viewport {
		scroll = cursor - viewport + 1
	}
	if scroll < 0 {
		scroll = 0
	}
	if max := total - viewport; scroll > max {
		scroll = max
	}
	return scroll
}

func pageStep(viewport int) int {
	step := viewport / 2
	if step < 4 {
		return 4
	}
	return step
}
