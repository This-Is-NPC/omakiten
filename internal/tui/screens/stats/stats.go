// Package stats owns the Stats › General screen: the per-AI-model metrics
// breakdown, the project totals / token budget summary, and the 7d/30d/all
// period picker bound to that dataset.
//
// The screen is a value type. It holds its own period selection and the last
// prepared summary, and it never sees the root Model: the host pushes
// availability and bundle totals with Bind, and every render reads geometry,
// theme and catalog off the frame.
package stats

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/screenhost"
)

// Periods is the canonical period cycle order. It is also the render order of
// the inline picker, so the two can never drift.
var Periods = []string{"7d", "30d", "all"}

// DefaultPeriod is the selection an unset screen renders and queries with.
const DefaultPeriod = "30d"

// Totals is the host-owned bundle projection the summary tables report. The
// screen does not load tasks/comments/tags itself — those belong to the root
// bundle reload — so the host pushes the counts it already holds.
type Totals struct {
	Tasks    int
	Comments int
	Tags     int
	Tokens   domain.TokenMetrics
}

// Deps is the immutable host snapshot a screen is bound to. The host rebuilds
// it every dispatch, so a bundle swap, a project switch or a fresh token count
// lands on the next interaction without the screen retaining host state.
type Deps struct {
	Available bool
	Totals    Totals
}

// Screen is the Stats › General implementation of screenhost.Screen.
type Screen struct {
	deps    Deps
	period  string
	summary domain.MetricsSummary
	// grid owns the model table's scroll offset and focus path. A project with
	// more AI models than the terminal affords — which at the 80-column floor
	// is any project with more than a handful — needs somewhere for the rest of
	// the table to be. The grid delegates item windowing to screenlayout while
	// keeping the screen's mount and navigation on one Cell path.
	grid screengrid.State
}

// New returns an unbound screen on the default period.
func New() Screen { return Screen{grid: screengrid.NewState()} }

// Bind returns a copy of the screen carrying a fresh host snapshot.
func (s Screen) Bind(deps Deps) Screen {
	s.deps = deps
	return s
}

// ID reports the stable screen identity.
func (s Screen) ID() screenhost.ID { return screenhost.StatsGeneral }

// Period is the active selection, defaulted for an unset screen.
func (s Screen) Period() string {
	if s.period == "" {
		return DefaultPeriod
	}
	return s.period
}

// Summary is the last loaded metrics reading.
func (s Screen) Summary() domain.MetricsSummary { return s.summary }

// Scroll is the model table's current window offset, exposed for host-side
// assertions.
func (s Screen) Scroll() int { return s.grid.Layout().Offset(sectionModels) }

// available reports whether the host can prepare a metrics payload.
func (s Screen) available() bool { return s.deps.Available }

// Payload is a completed reload result.
type Payload struct{ Summary domain.MetricsSummary }

// Apply folds a reload result into the screen. The host owns the stale-result
// guard and only calls Apply for a result it has accepted.
//
// The window rewinds: a reload can return fewer models than the offset
// addresses, and a stale offset over a shorter table shows a blank window with
// no way to tell why.
func (s Screen) Apply(payload Payload) Screen {
	s.summary = payload.Summary
	s.grid = screengrid.NewState()
	return s
}

// Update handles the screen's key vocabulary: ←/h and →/l cycle the period and
// reload, and the standard row-navigation verbs move the model table's window
// through screenlayout. A reload failure reports the message as a semantic
// status outcome rather than writing to host state.
func (s Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(s, nil)
	}
	kit := frame.Kit()
	switch key.String() {
	case "left", "h":
		return s.cyclePeriod(-1)
	case "right", "l":
		return s.cyclePeriod(1)
	}
	if next, handled := s.grid.HandleKey(kit, s.panelBox(kit), key.String(), s.modelsRoot(kit)); handled {
		s.grid = next
	}
	return screenhost.Stay(s, nil)
}

// cyclePeriod steps the picker and reloads the dataset it is bound to.
func (s Screen) cyclePeriod(step int) screenhost.Outcome {
	s.period = Periods[(s.periodIndex()+step+len(Periods))%len(Periods)]
	// The host owns the service call and folds the prepared summary.
	return screenhost.Reload(s, nil)
}

// periodIndex locates the active period in the cycle, defaulting to 30d when
// the stored value is unset or unknown.
func (s Screen) periodIndex() int {
	for i, period := range Periods {
		if period == s.period {
			return i
		}
	}
	return 1 // default: 30d
}

// Footer declares the screen-specific keybinding hints. The host appends the
// global navigation trailer (tab / ,// / ?) so a screen never advertises a key
// it does not handle.
func (s Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	return []screenhost.FooterBinding{
		{Key: "←/→", Label: frame.Text("tui.footer.period_7d_30d_all"), Primary: true},
		{Key: "up/down", Label: frame.Text("tui.footer.scroll"), Primary: true},
		frame.FooterRefresh(false),
		frame.FooterTopBottom(false),
	}
}

// Help declares the screen's context-help group.
func (s Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{
		ID:    "stats_general",
		Title: frame.Text("tui.help.stats_general.title"),
		Bindings: []screenhost.HelpBinding{
			{Key: "← →", Description: frame.Text("tui.help.stats_general.cycle_period")},
			{Key: "↑ ↓ · j k", Description: frame.Text("tui.help.stats_general.scroll")},
			{Key: "pgup · pgdn · ctrl+u · ctrl+d", Description: frame.Text("tui.help.stats_general.scroll_halfpage")},
			{Key: "g · G", Description: frame.Text("tui.help.stats_general.first_last_row")},
			{Key: "r", Description: frame.Text("tui.help.stats_general.refresh")},
		},
	}}
}

// Lifecycle re-clamps the model table's window against the current geometry.
// Every event is idempotent, so a host may dispatch them in any order. The host
// does not yet dispatch lifecycle events at all; when it starts to, this screen
// is already correct.
func (s Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	switch event {
	case screenhost.LifecycleResize, screenhost.LifecycleEnter:
		s = s.syncModelsWindow(frame.Kit())
	}
	return screenhost.Stay(s, nil)
}
