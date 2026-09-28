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

// Registry owns the code→route lookup table the `nav:` handler
// queries. Overrides take precedence over positional defaults so a
// user can rebind the codes their muscle memory expects without
// editing the indexer.
type Registry struct {
	resolved map[string]Route
}

// New builds a Registry from defaults + per-code overrides. The
// registry rejects only structural errors at construction (empty or malformed
// default code, empty route, or empty title key);
// override mistakes downgrade to Warning so the palette stays
// usable while the user fixes their config. Overrides win every
// time they apply.
//
// Warnings cover malformed override codes, unknown override routes, and
// positional collisions inside the caller-supplied descriptor projection.
// The palette deliberately owns no default screen list; the TUI host projects
// it from the authoritative screen registry.
func New(defaults []ScreenDescriptor, overrides map[string]Route) (*Registry, []Warning, error) {
	if len(defaults) == 0 {
		return nil, nil, errors.New("palette: registry requires at least one default screen")
	}
	resolved := make(map[string]Route, len(defaults)+len(overrides))
	validRoutes := make(map[Route]struct{}, len(defaults))
	var warnings []Warning
	for _, d := range defaults {
		warning, err := registerDefault(d, resolved, validRoutes)
		if err != nil {
			return nil, nil, err
		}
		if warning.Code != "" {
			warnings = append(warnings, warning)
		}
	}
	for code, route := range overrides {
		if warning, ok := registerOverride(code, route, resolved, validRoutes); ok {
			warnings = append(warnings, warning)
		}
	}
	return &Registry{resolved: resolved}, warnings, nil
}

func registerDefault(d ScreenDescriptor, resolved map[string]Route, validRoutes map[Route]struct{}) (Warning, error) {
	if d.Code == "" {
		return Warning{}, fmt.Errorf("palette: default screen %q has empty code", d.Route)
	}
	if !codePattern.MatchString(d.Code) {
		return Warning{}, fmt.Errorf("palette: default screen %q has malformed code %q (want 2 digits in 1-9)", d.Route, d.Code)
	}
	if d.Route == "" {
		return Warning{}, fmt.Errorf("palette: default screen at code %q has empty route", d.Code)
	}
	if d.TitleKey == "" {
		return Warning{}, fmt.Errorf("palette: default screen %q has empty title key", d.Route)
	}
	validRoutes[d.Route] = struct{}{}
	if existing, dup := resolved[d.Code]; dup {
		return Warning{
			Code:    d.Code,
			Message: fmt.Sprintf("default code %q collides between routes %q and %q; keeping the first", d.Code, existing, d.Route),
		}, nil
	}
	resolved[d.Code] = d.Route
	return Warning{}, nil
}

func registerOverride(code string, route Route, resolved map[string]Route, validRoutes map[Route]struct{}) (Warning, bool) {
	if !codePattern.MatchString(code) {
		return Warning{
			Code:    code,
			Message: fmt.Sprintf("override code %q is malformed (want 2 digits in 1-9); skipping", code),
		}, true
	}
	if _, ok := validRoutes[route]; !ok {
		return Warning{
			Code:    code,
			Message: fmt.Sprintf("override at code %q references unknown route %q; skipping", code, route),
		}, true
	}
	resolved[code] = route
	return Warning{}, false
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
