// Package palette implements the global Ctrl+K trick palette: a
// modal overlay with a Tricks tab (verb-prefixed shortcut input
// `verb:operand`) and a Search tab (FTS5 fuzzy search). The package
// owns the ScreenRegistry that maps positional `nav:<code>` digits
// to (top, sub) navigation pairs, the verb parser, the built-in
// `nav` and `op` handlers, and the Bubbletea overlay model.
//
// User-defined verbs (anything outside the reserved `nav` / `op`
// pair) emit `trick.executed{verb, operand, raw}` through the
// hooks engine without a built-in side-effect, so users wire any
// custom behaviour through standard `hooks:` entries filtered on
// `when: {verb, operand}`.
package palette

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
)

// Route is the opaque slug a Resolve hit produces. The slug shape
// is `<top>.<sub>` (e.g. `tasks.board`, `settings.laws`) and the
// TUI side owns the slug→(top,sub) translation table; the registry
// itself only cares that slugs match a known route at New time.
type Route string

// ScreenDescriptor is one row in the positional indexer: the
// 2-digit `nav:` code, the Route it resolves to, and an i18n key
// used by the palette View for display. Title is the i18n key, not
// the rendered label, so locale rotations land without a registry
// rebuild.
type ScreenDescriptor struct {
	Code     string
	Route    Route
	TitleKey string
}

// Warning is a non-fatal registry diagnostic surfaced by New —
// collisions and unknown override routes. Warnings keep the
// registry usable while the user is asked to fix their config; a
// hard error would lock the TUI out of the palette over a typo.
type Warning struct {
	Code    string
	Message string
}

// codePattern enforces the positional grammar: 2 digits in 1-9
// (zero is reserved so the user never has to type a leading zero
// and so the indexer never collides with a single-digit shortcut
// that a future grammar revision might want).
var codePattern = regexp.MustCompile(`^[1-9][1-9]$`)

// validRoutes is the closed set every ScreenDescriptor + override
// slug must match. Adding a screen means appending to this slice
// AND wiring the new (top, sub) into routeBindings in route.go.
var validRoutes = map[Route]struct{}{
	RouteTasksBoard:        {},
	RouteTasksTable:        {},
	RouteTasksGraph:        {},
	RouteTasksPlans:        {},
	RouteStatsGeneral:      {},
	RouteStatsLogs:         {},
	RouteSettingsGeneral:   {},
	RouteSettingsLaws:      {},
	RouteSettingsPersonas:  {},
	RouteSettingsSkills:    {},
	RouteSettingsTemplates: {},
	RouteSettingsTags:      {},
	RouteSettingsEffective: {},
	RouteSettingsGuards:    {},
}

// Canonical route slugs. Mirror the (topID, subID) pairs declared
// in internal/tui/state.go (topTasks/subBoard, etc.). The TUI
// glues the slug back to the nav pair via routeBindings.
const (
	RouteTasksBoard        Route = "tasks.board"
	RouteTasksTable        Route = "tasks.table"
	RouteTasksGraph        Route = "tasks.graph"
	RouteTasksPlans        Route = "tasks.plans"
	RouteStatsGeneral      Route = "stats.general"
	RouteStatsLogs         Route = "stats.logs"
	RouteSettingsGeneral   Route = "settings.general"
	RouteSettingsLaws      Route = "settings.laws"
	RouteSettingsPersonas  Route = "settings.personas"
	RouteSettingsSkills    Route = "settings.skills"
	RouteSettingsTemplates Route = "settings.templates"
	RouteSettingsTags      Route = "settings.tags"
	RouteSettingsEffective Route = "settings.effective"
	RouteSettingsGuards    Route = "settings.guards"
)

// DefaultScreens returns the canonical positional layout the
// indexer ships with: 1x = Tasks subs, 2x = Stats subs, 3x =
// Settings subs. The 2-digit cap (`[1-9][1-9]`) leaves 81 slots
// for a current load of 12, so adding a sub means appending a new
// descriptor with the next unused code under its parent top — no
// renumbering. The order matches the cycle order declared by
// subsByTop in state.go so the positional codes track the visible
// menu cycle without a second source of truth.
func DefaultScreens() []ScreenDescriptor {
	return []ScreenDescriptor{
		{Code: "11", Route: RouteTasksBoard, TitleKey: "tui.palette.route.tasks_board"},
		{Code: "12", Route: RouteTasksTable, TitleKey: "tui.palette.route.tasks_table"},
		{Code: "13", Route: RouteTasksGraph, TitleKey: "tui.palette.route.tasks_graph"},
		{Code: "14", Route: RouteTasksPlans, TitleKey: "tui.palette.route.tasks_plans"},
		{Code: "21", Route: RouteStatsGeneral, TitleKey: "tui.palette.route.stats_general"},
		{Code: "22", Route: RouteStatsLogs, TitleKey: "tui.palette.route.stats_logs"},
		{Code: "31", Route: RouteSettingsGeneral, TitleKey: "tui.palette.route.settings_general"},
		{Code: "32", Route: RouteSettingsLaws, TitleKey: "tui.palette.route.settings_laws"},
		{Code: "33", Route: RouteSettingsPersonas, TitleKey: "tui.palette.route.settings_personas"},
		{Code: "34", Route: RouteSettingsSkills, TitleKey: "tui.palette.route.settings_skills"},
		{Code: "35", Route: RouteSettingsTemplates, TitleKey: "tui.palette.route.settings_templates"},
		{Code: "36", Route: RouteSettingsTags, TitleKey: "tui.palette.route.settings_tags"},
		{Code: "37", Route: RouteSettingsEffective, TitleKey: "tui.palette.route.settings_effective"},
		{Code: "38", Route: RouteSettingsGuards, TitleKey: "tui.palette.route.settings_guards"},
	}
}

// Registry owns the code→route lookup table the `nav:` handler
// queries. Overrides take precedence over positional defaults so a
// user can rebind the codes their muscle memory expects without
// editing the indexer.
type Registry struct {
	resolved map[string]Route
	screens  []ScreenDescriptor
}

// New builds a Registry from defaults + per-code overrides. The
// registry rejects only structural errors at construction (empty
// default code, malformed default code, unknown default route);
// override mistakes downgrade to Warning so the palette stays
// usable while the user fixes their config. Overrides win every
// time they apply.
//
// Warnings cover: malformed override code, unknown override route,
// positional collision inside DefaultScreens (defensive — the
// shipped slice is gap-free, but any future drift surfaces here
// instead of silently shadowing the prior entry).
func New(defaults []ScreenDescriptor, overrides map[string]Route) (*Registry, []Warning, error) {
	if len(defaults) == 0 {
		return nil, nil, errors.New("palette: registry requires at least one default screen")
	}
	resolved := make(map[string]Route, len(defaults)+len(overrides))
	var warnings []Warning
	for _, d := range defaults {
		if d.Code == "" {
			return nil, nil, fmt.Errorf("palette: default screen %q has empty code", d.Route)
		}
		if !codePattern.MatchString(d.Code) {
			return nil, nil, fmt.Errorf("palette: default screen %q has malformed code %q (want 2 digits in 1-9)", d.Route, d.Code)
		}
		if _, ok := validRoutes[d.Route]; !ok {
			return nil, nil, fmt.Errorf("palette: default screen at code %q references unknown route %q", d.Code, d.Route)
		}
		if existing, dup := resolved[d.Code]; dup {
			warnings = append(warnings, Warning{
				Code:    d.Code,
				Message: fmt.Sprintf("default code %q collides between routes %q and %q; keeping the first", d.Code, existing, d.Route),
			})
			continue
		}
		resolved[d.Code] = d.Route
	}
	for code, route := range overrides {
		if !codePattern.MatchString(code) {
			warnings = append(warnings, Warning{
				Code:    code,
				Message: fmt.Sprintf("override code %q is malformed (want 2 digits in 1-9); skipping", code),
			})
			continue
		}
		if _, ok := validRoutes[route]; !ok {
			warnings = append(warnings, Warning{
				Code:    code,
				Message: fmt.Sprintf("override at code %q references unknown route %q; skipping", code, route),
			})
			continue
		}
		resolved[code] = route
	}
	return &Registry{
		resolved: resolved,
		screens:  defaults,
	}, warnings, nil
}

// Resolve maps a 2-digit code to a Route. The miss signal is the
// usual (zero, false) pair so the caller can distinguish "no
// binding" from a real "empty route" (the empty Route is never a
// valid registration anyway).
func (r *Registry) Resolve(code string) (Route, bool) {
	route, ok := r.resolved[code]
	return route, ok
}

// Codes returns the bound codes in lexicographic order. Used by
// the palette help footer to render the active nav cheatsheet
// deterministically.
func (r *Registry) Codes() []string {
	out := make([]string, 0, len(r.resolved))
	for code := range r.resolved {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
