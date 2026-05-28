package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/palette"
)

// routeBindings maps a palette.Route slug back to the (top, sub)
// pair the TUI navigation state machine uses. Source of truth is
// state.go's topID/subID enums plus the palette.Route constants
// in internal/tui/palette/registry.go — adding a screen means
// landing the route in both halves of this map so the registry
// enumeration test and a `nav:<code>` keypress agree on the
// destination.
var routeBindings = map[palette.Route]navState{
	palette.RouteTasksBoard:        {top: topTasks, sub: subBoard},
	palette.RouteTasksTable:        {top: topTasks, sub: subTable},
	palette.RouteTasksGraph:        {top: topTasks, sub: subGraph},
	palette.RouteTasksPlans:        {top: topTasks, sub: subPlans},
	palette.RouteStatsGeneral:      {top: topStats, sub: subStatsGeneral},
	palette.RouteStatsLogs:         {top: topStats, sub: subStatsLogs},
	palette.RouteSettingsGeneral:   {top: topSettings, sub: subSettingsGeneral},
	palette.RouteSettingsLaws:      {top: topSettings, sub: subSettingsLaws},
	palette.RouteSettingsPersonas:  {top: topSettings, sub: subSettingsPersonas},
	palette.RouteSettingsSkills:    {top: topSettings, sub: subSettingsSkills},
	palette.RouteSettingsTemplates: {top: topSettings, sub: subSettingsTemplates},
	palette.RouteSettingsTags:      {top: topSettings, sub: subSettingsTags},
}

// dispatchTrick is the built-in palette dispatch path. Every
// submission first emits the `trick.executed` event so user hooks
// react regardless of verb (AC 7), then the verb switch handles
// the two built-ins:
//
//   - nav: registry → route → jumpToRoute; miss returns an error
//     surfaced in the palette inline status (AC 5)
//   - op : operand parses as task id → openTaskView; unknown task
//     surfaces inline (no panic)
//
// Any other verb has no built-in dispatch; the event already
// emitted in step 1 is the entire contract for user-defined verbs
// (D2). Returning nil keeps the overlay open so the user can
// submit again or hit Esc.
func (m *Model) dispatchTrick(token palette.Token) tea.Cmd {
	m.emitTrickEvent(token)
	switch token.Verb {
	case "nav":
		if m.paletteRegistry == nil {
			m.palette.SetStatus("nav registry not initialised")
			return nil
		}
		route, ok := m.paletteRegistry.Resolve(token.Operand)
		if !ok {
			m.palette.SetStatus(fmt.Sprintf("no screen for nav code %q", token.Operand))
			return nil
		}
		if err := m.jumpToRoute(route); err != nil {
			m.palette.SetStatus(err.Error())
			return nil
		}
		m.paletteOpen = false
		return nil
	case "op":
		id, err := strconv.ParseInt(strings.TrimSpace(token.Operand), 10, 64)
		if err != nil || id <= 0 {
			m.palette.SetStatus(fmt.Sprintf("op operand must be a positive task id, got %q", token.Operand))
			return nil
		}
		task, lookupErr := m.repos.Tasks.GetTaskByID(m.ctx, m.project.ID, id, nil)
		if lookupErr != nil {
			m.palette.SetStatus(fmt.Sprintf("task #%d not found", id))
			return nil
		}
		m.paletteOpen = false
		m.openTaskView(task)
		return nil
	default:
		// User-defined verb. Event already emitted; built-in has
		// nothing to do. Close the overlay so a hook side-effect
		// (notification, exec) takes the user's attention without
		// the palette overlay sitting on top.
		m.paletteOpen = false
		return nil
	}
}

// emitTrickEvent records the trick.executed event with the
// {verb, operand, raw} payload. Best-effort: a recording failure
// must not break the dispatch path, so the error is swallowed
// (the engine still has the in-process event via the bus when
// available).
func (m *Model) emitTrickEvent(token palette.Token) {
	if m.repos.Events == nil {
		return
	}
	payload := map[string]string{
		"verb":    token.Verb,
		"operand": token.Operand,
		"raw":     token.Raw,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = m.repos.Events.RecordEntityEvent(m.ctx, domain.EventEntitySystem, 0, m.project.ID, domain.EventTypeTrickExecuted, string(raw))
}

// jumpToRoute is the navigation equivalent of jumpTop+jumpSub:
// resolves a palette.Route to its (top, sub) binding and rotates
// the model state through the same syncEntityKindFromSub path so
// downstream renderers (settings, entity lists) refresh in step
// with the nav change. Unknown routes return an error so the
// caller can render an inline status.
func (m *Model) jumpToRoute(route palette.Route) error {
	target, ok := routeBindings[route]
	if !ok {
		return fmt.Errorf("palette: route %q has no binding", route)
	}
	m.pushHistory()
	m.top = target.top
	m.sub = target.sub
	m.syncEntityKindFromSub()
	return nil
}

// buildPaletteRegistry materialises the palette ScreenRegistry
// from the active snapshot's config.tricks.nav overrides. Reading
// through Snapshot.Tricks (not Editor.Load) keeps the construction
// path off the bundle-editor LoadBundle entry, which the
// refresh_noscan_test guard panics on for any code path the
// snapshot can already serve. Unknown override routes downgrade
// to non-fatal warnings inside palette.New so the registry stays
// usable while the user fixes their config.
func buildPaletteRegistry(repos Repositories) (*palette.Registry, error) {
	overrides := map[string]palette.Route{}
	if snap := repos.activeSnapshot(); snap != nil {
		for code, route := range snap.Tricks().Nav {
			overrides[code] = palette.Route(route)
		}
	}
	reg, _, err := palette.New(palette.DefaultScreens(), overrides)
	return reg, err
}

// paletteSearchResultMsg is the async tail of dispatchPaletteSearch.
// Carrying the formatted status string (not the raw SearchHit slice)
// keeps Update arm free of snippet truncation logic and means the
// goroutine returns a fully-rendered result the UI assigns directly.
type paletteSearchResultMsg struct {
	query  string
	status string
}

// dispatchPaletteSearch returns a tea.Cmd that runs the FTS5 query
// off the UI goroutine. The synchronous original blocked Update for
// the full Search round-trip (FTS5 query + activity.Track writes), so
// even a sub-100ms call read as a freeze because the palette could
// not redraw until Update returned. Hand the work to a goroutine via
// tea.Cmd, post paletteSearchResultMsg back, and let the root Update
// fold the formatted status into the open overlay. Mirrors the
// existing dispatchNotification + home project delete patterns.
//
// Pre-dispatch the helper sets a "searching <query>…" status
// synchronously so the user gets immediate feedback while the
// goroutine works.
func (m *Model) dispatchPaletteSearch(query string) tea.Cmd {
	if m.repos.Search == nil {
		m.palette.SetStatus("search not wired in this build")
		return nil
	}
	m.palette.SetStatus(fmt.Sprintf("searching %q…", query))
	search := m.repos.Search
	ctx := m.ctx
	project := m.project
	return func() tea.Msg {
		hits, err := search.Search(ctx, project, query, nil)
		if err != nil {
			return paletteSearchResultMsg{query: query, status: "search failed: " + err.Error()}
		}
		return paletteSearchResultMsg{query: query, status: formatPaletteSearchResults(query, hits)}
	}
}

// formatPaletteSearchResults renders the FTS5 hits as a result-count
// header plus up to the first three rows. Pulled out of the dispatch
// path so the goroutine produces a plain string and the Update arm
// only has to assign it.
func formatPaletteSearchResults(query string, hits []domain.SearchHit) string {
	if len(hits) == 0 {
		return fmt.Sprintf("no results for %q", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d results", len(hits))
	cap := 3
	if len(hits) < cap {
		cap = len(hits)
	}
	for i := 0; i < cap; i++ {
		h := hits[i]
		snippet := strings.TrimSpace(h.Snippet)
		if len(snippet) > 60 {
			snippet = snippet[:60] + "…"
		}
		fmt.Fprintf(&b, "\n  %s #%d %s", h.EntityType, h.ID, snippet)
	}
	return b.String()
}
