// Package insights owns the Stats › Insights screen: the intelligence layer's
// six today-readings (stuck tasks, cycle time, WIP, guard hotspots, the error
// loop, and the per-model contrast).
//
// The screen owns its scroll offset and its cached reading. It never sees the
// root Model: the host pushes availability and prepared bucket labels with Bind
// and Apply, and every render reads geometry, theme and catalog off the frame.
package insights

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

// Deps is the immutable host snapshot a screen is bound to. Available is false
// when the host cannot prepare a payload; the screen renders the placeholder.
type Deps struct {
	Available bool
}

// Screen is the Stats › Insights implementation of screenhost.Screen.
type Screen struct {
	deps     Deps
	insights domain.Insights
	buckets  []domain.Bucket
	loaded   bool
	grid     screengrid.State
}

// New returns an unbound screen with nothing loaded yet.
func New() Screen { return Screen{grid: screengrid.NewState()} }

// Bind returns a copy of the screen carrying a fresh host snapshot.
func (s Screen) Bind(deps Deps) Screen {
	s.deps = deps
	return s
}

// ID reports the stable screen identity.
func (s Screen) ID() screenhost.ID { return screenhost.StatsInsights }

// Loaded reports whether a reading has landed. Before the first load the view
// shows the computing placeholder rather than an all-empty board, which would
// misread as healthy.
func (s Screen) Loaded() bool { return s.loaded }

// Insights is the cached reading.
func (s Screen) Insights() domain.Insights { return s.insights }

// Scroll is the current body offset, exposed for host-side assertions.
func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionBody) }

// available reports whether the host can prepare an insights payload.
func (s Screen) available() bool { return s.deps.Available }

// Payload is a completed reload result plus host-prepared bucket labels.
type Payload struct {
	Insights    domain.Insights
	BucketNames []domain.Bucket
}

// Apply folds a reload result into the screen and marks it loaded. The host
// owns the stale-result guard and only calls Apply for a result it accepted.
func (s Screen) Apply(payload Payload) Screen {
	s.insights = payload.Insights
	s.buckets = payload.BucketNames
	s.loaded = true
	return s
}

// Update owns the read-only scroll vocabulary — j/k, PgUp/PgDn, ctrl+u/ctrl+d,
// g/G nudge the body offset when the six-section panel is taller than the
// terminal (offset-only, no cursor). `r` (refresh) stays on the host's common
// key path. The body is resynced before every scroll so the arranger clamps
// against the current reading: the realtime tick can grow or shrink the body
// between keypresses.
func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	root := s.bodyRoot(kit)
	s.grid, _ = s.grid.HandleKey(kit, s.panelBox(kit), key.String(), root)
	return screenhost.Stay(s, nil)
}

// viewportRows is the body-row budget for the Insights panel. The only chrome
// the scrolled body does not own is the bordered panel around it, and that cost
// is measured off the panel rather than restated here; the kit's viewport helper
// already accounts for the host chrome, the leading blank and the footer.
func (s Screen) viewportRows(kit screenkit.Kit) int {
	return kit.PanelChrome().ViewportRows()
}

// Footer declares the screen-specific keybinding hints. The host appends the
// global navigation trailer (tab / ,// / ?).
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		frame.FooterRefresh(true),
		frame.FooterScroll(false),
	}
}

// Help declares the screen's context-help group.
func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID:    "stats_insights",
		Title: frame.Text("tui.help.stats_insights.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "r", Description: frame.Text("tui.help.stats_insights.refresh")},
			// Reuses the footer's translated "scroll" label — the binding is
			// the shared read-only scroll vocabulary, no bespoke copy needed.
			{Key: "j k · pgup · pgdn · g G", Description: frame.Text("tui.footer.scroll")},
		},
	}}
}

// Lifecycle re-clamps the scroll window against the current geometry. Every
// event is idempotent, so a host may dispatch them in any order. The host does
// not yet dispatch lifecycle events at all; when it starts to, this screen is
// already correct.
func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	if event == screenhost.LifecycleEnter {
		s.grid = screengrid.NewState()
	}
	kit := frame.Kit()
	s.grid = s.grid.Resync(kit, s.panelBox(kit), s.bodyRoot(kit))
	return screenhost.Stay(s, nil)
}

var _ screenhost.Screen = Screen{}
