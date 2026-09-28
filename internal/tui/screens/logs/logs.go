// Package logs owns the Stats › Logs screen: the unified event inspector.
// Every event_type the project recorded inside the snapshot's
// `views.logs.window_days` window renders through a single 5-column row shape
// (time · type · entity · who · detail), with an F-cycle filter chip strip as
// outer chrome and optional categories + tool-call-health summary tables as a
// secondary screenlayout section that yields to the event feed on a short
// HostBox.
//
// The screen owns its cursor, screenlayout scroll window, active filter preset
// and loaded row buffer. The host pushes availability, display-only retention,
// and prepared payloads with Bind/Apply; every render reads geometry, theme and
// catalog off the frame.
package logs

import (
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// Retention is the resolved storage retention the footer note reports beside
// the configured display window. Known is false when policy data is absent.
type Retention struct {
	Known      bool
	MaxAgeDays int
	MaxRows    int
	WindowDays int
}

// ViewSettings carries display-only retention information prepared by the host.
type ViewSettings struct {
	Retention Retention
}

// Deps is the immutable host snapshot a screen is bound to. Available is false
// when the host cannot prepare a payload; the screen renders the placeholder.
type Deps struct {
	Available bool
	Settings  ViewSettings
}

// Screen is the Stats › Logs implementation of screenhost.Screen.
type Screen struct {
	deps Deps

	filter   domain.LogsFilterMode
	selected int
	rows     []domain.EventRow
	stats    domain.EventStats
	grid     screengrid.State
	// generation identifies the loaded buffer — rows AND the aggregate folded
	// from them — so a memoised composition can be matched against the data it
	// was composed from without comparing the data. See bufferSeq.
	generation uint64
	// body is what this screen composed last, kept so a keystroke does not
	// recompose what a keystroke cannot change. See logsBlockCache.
	body *logsBlockCache
}

// bufferSeq hands every buffer the screen is handed an identity no other buffer
// shares.
//
// A per-screen tick would be enough for the host, which applies reloads in a
// line. It is not enough in general: two screens descended from the same parent
// carry the same memo POINTER, and a per-screen tick would give their two
// different buffers the same number — an entry composed from one served to the
// other. A process-wide sequence cannot collide, and a memo key is the only
// thing that reads it, so nothing a user sees depends on its value.
var bufferSeq atomic.Uint64

// New returns an unbound screen on the no-op filter with an empty buffer.
func New() Screen { return Screen{grid: screengrid.NewState(), body: &logsBlockCache{}} }

// Bind returns a copy of the screen carrying a fresh host snapshot.
func (s Screen) Bind(deps Deps) Screen {
	s.deps = deps
	return s
}

// ID reports the stable screen identity.
func (s Screen) ID() screenhost.ID { return screenhost.StatsLogs }

// Filter is the active chip selection.
func (s Screen) Filter() domain.LogsFilterMode { return s.filter }

// Selected is the cursor row index.
func (s Screen) Selected() int { return s.selected }

// Rows is the loaded event buffer.
func (s Screen) Rows() []domain.EventRow { return s.rows }

// Stats is the aggregate the summary tables render.
func (s Screen) Stats() domain.EventStats { return s.stats }

func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionEvents) }

// available reports whether the host can prepare event data.
func (s Screen) available() bool { return s.deps.Available }

// Reset drops project-bound rows, aggregate state, cursor and scroll offset.
// The host calls it before changing project so the first frame cannot render
// data from the project that was just left while the new payload is loading.
func (s Screen) Reset() Screen {
	s.selected = 0
	s.rows = nil
	s.stats = domain.EventStats{}
	s.generation = bufferSeq.Add(1)
	s.grid = screengrid.NewState()
	return s
}

// Payload is a completed reload result: the raw rows plus the per-category
// totals aggregated across the wider window.
type Payload struct {
	Rows  []domain.EventRow
	Stats domain.EventStats
}

// The cursor is clamped to the new buffer: a reload can shrink the result under
// the current selection, and when the buffer empties the cursor must drop to 0
// so the empty-state renderer and any later marker lookup do not address a row
// that no longer exists.
func (s Screen) Apply(payload Payload) Screen {
	s.rows = payload.Rows
	if s.selected >= len(s.rows) {
		if len(s.rows) == 0 {
			s.selected = 0
		} else {
			s.selected = len(s.rows) - 1
		}
	}
	s.stats = payload.Stats
	s.generation = bufferSeq.Add(1)
	return s
}

// Update handles the screen's key vocabulary: `f` / `shift+F` cycle the filter
// preset, and the standard row-navigation verbs move the cursor.
func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	// Resolve filter actions before delegating declined keys to the event layout.
	switch key.String() {
	case "f":
		return s.cycleFilter(kit, 1)
	case "F":
		return s.cycleFilter(kit, -1)
	case "up", "k", "down", "j", "pgup", "ctrl+u", "pgdown", "ctrl+d", "home", "g", "end", "G":
		s = s.moveSelection(kit, key.String())
	default:
		if next, handled := s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.bodyRoot(kit)); handled {
			s.grid = next
			if c := s.grid.Layout().Cursor(sectionEvents); c >= 0 {
				s.selected = c
			}
		}
		return screenhost.Stay(s, nil)
	}
	return screenhost.Stay(s.syncEventsWindow(kit), nil)
}

func (s Screen) moveSelection(kit screenkit.Kit, key string) Screen {
	switch key {
	case "up", "k":
		if s.selected > 0 {
			s.selected--
		}
	case "down", "j":
		if s.selected < len(s.rows)-1 {
			s.selected++
		}
	case "pgup", "ctrl+u":
		s.selected -= screenkit.PageStep(s.viewportRows(kit))
		if s.selected < 0 {
			s.selected = 0
		}
	case "pgdown", "ctrl+d":
		s.selected += screenkit.PageStep(s.viewportRows(kit))
		if s.selected > len(s.rows)-1 {
			s.selected = len(s.rows) - 1
		}
		if s.selected < 0 {
			s.selected = 0
		}
	case "home", "g":
		s.selected = 0
	case "end", "G":
		if len(s.rows) > 0 {
			s.selected = len(s.rows) - 1
		}
	}
	return s
}

// cycleFilter rotates the active preset and re-fetches the panel rows so the
// chip selection and the visible rows stay aligned in one tick. Cursor and
// scroll reset (the filtered buffer can be shorter than the current selection)
// so the user lands on the first matching row instead of an empty selection.
// The host runs the reload and surfaces any failure through its status path.
func (s Screen) cycleFilter(kit screenkit.Kit, step int) screenhost.Outcome {
	s.filter = domain.CycleLogsFilter(s.filter, step)
	s.selected = 0
	if !s.available() {
		// Hosts that exercise the cycle without a live events port still
		// expect the mode to roll over; the refresh short-circuits there so
		// the row buffer stays untouched.
		return screenhost.Reload(s.syncEventsWindow(kit), nil)
	}
	return screenhost.Reload(s.syncEventsWindow(kit), nil)
}

// OwnsKey claims the filter vocabulary before the event layout. The layout may
// decline keys it does not own, but it must never shadow a domain filter action.
func (s Screen) OwnsKey(key tea.KeyMsg) bool {
	switch key.String() {
	case "f", "F":
		return true
	default:
		return false
	}
}

// Footer declares the screen-specific keybinding hints. The host appends the
// global navigation trailer (tab / ,// / ?).
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		{Key: "up/down", Label: frame.Text("tui.footer.select_row"), Primary: true},
		frame.FooterRefresh(true),
		frame.FooterScrollPage(false),
		frame.FooterTopBottom(false),
	}
}

// Help declares the screen's context-help group.
func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID:    "stats_logs",
		Title: frame.Text("tui.help.stats_logs.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "← →", Description: frame.Text("tui.help.stats_logs.switch_view")},
			{Key: "↑ ↓ · j k", Description: frame.Text("tui.help.stats_logs.select_row")},
			{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.stats_logs.scroll_halfpage")},
			{Key: "g · G", Description: frame.Text("tui.help.stats_logs.first_last_row")},
			{Key: "f", Description: frame.Text("tui.help.stats_logs.filter_cycle")},
			{Key: "shift+F", Description: frame.Text("tui.help.stats_logs.filter_cycle_back")},
			{Key: "r", Description: frame.Text("tui.help.stats_logs.refresh")},
		},
	}}
}

// Lifecycle re-clamps the scroll window against the current geometry. Every
// event is idempotent and cursor-preserving, so a host may dispatch them in any
// order. The host does not yet dispatch lifecycle events at all; when it starts
// to, this screen is already correct.
func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	switch event {
	case screenhost.LifecycleResize, screenhost.LifecycleEnter:
		return screenhost.Stay(s.syncEventsWindow(frame.Kit()), nil)
	}
	return screenhost.Stay(s, nil)
}

var _ screenhost.KeyOwner = Screen{}

var _ screenhost.Screen = Screen{}
