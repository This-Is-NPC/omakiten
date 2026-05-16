package config

import (
	"regexp"
	"strings"
)

// Surface enumerates the catalog read paths so a single Snapshot can
// hand out independent catalogs per delivery layer. CLI drives cobra
// help/usage chrome; TUI drives terminal-UI labels and screens. New
// surfaces (notifications, MCP) reuse one of these today rather than
// adding fragmentation.
type Surface int

const (
	// SurfaceCLI selects the catalog resolved against languages.cli.
	SurfaceCLI Surface = iota
	// SurfaceTUI selects the catalog resolved against languages.tui.
	SurfaceTUI
)

// Language describes one localized string pack discovered by the bundle
// loader (bundled in defaults/languages or user-authored under
// <root>/languages/custom). The Code is the lookup key used by the
// languages.{cli,tui} config fields; Name and Native are display labels
// for CLI/TUI surfaces that render language pickers.
type Language struct {
	Code       string
	Name       string
	Native     string
	Keys       map[string]string
	SourcePath string
	IsCustom   bool
}

// Catalog resolves catalog keys against an active language with a
// baseline (en) fallback. A nil Catalog is safe to call and behaves as
// if no keys are known: Get returns the requested key and Resolve
// leaves every token verbatim. This lets early-boot or test paths
// construct a CLI tree before the bundle Snapshot is wired.
type Catalog struct {
	active   *Language
	baseline *Language
}

// NewCatalog wraps the active and baseline languages. Either may be nil:
// a nil active falls through to baseline; a nil baseline falls through
// to the key literal.
func NewCatalog(active, baseline *Language) *Catalog {
	return &Catalog{active: active, baseline: baseline}
}

// Get returns the localized string for key with the fallback chain
// active → baseline → key. Missing keys never panic and never return
// empty strings: the key itself is the last-resort literal so unresolved
// references stay visible and grep-friendly in the rendered output.
func (c *Catalog) Get(key string) string {
	if c == nil {
		return key
	}
	if c.active != nil {
		if v, ok := c.active.Keys[key]; ok {
			return v
		}
	}
	if c.baseline != nil {
		if v, ok := c.baseline.Keys[key]; ok {
			return v
		}
	}
	return key
}

// tokenPattern matches optional `$` escape + `${{namespace:key}}`.
// Group 1: escape prefix (empty or "$"). Group 2: namespace
// ([a-zA-Z][a-zA-Z0-9_-]*). Group 3: key ([a-zA-Z0-9._-]+).
// Malformed tokens (missing closing braces, empty namespace or key,
// whitespace inside the key) never match and stay verbatim.
var tokenPattern = regexp.MustCompile(`(\$?)\$\{\{([a-zA-Z][a-zA-Z0-9_-]*):([a-zA-Z0-9._-]+)\}\}`)

// Resolve expands `${{intl:KEY}}` tokens inside s. See the catalog
// resolver edge cases in task #82 §7 for the full behavior matrix:
// single-pass (catalog values are not re-scanned), mixed content
// supported, `$${{...}}` escape produces a literal `${{...}}`, unknown
// namespaces and malformed tokens are left verbatim.
func (c *Catalog) Resolve(s string) string {
	if c == nil || !strings.Contains(s, "${{") {
		return s
	}
	matches := tokenPattern.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		b.WriteString(s[last:fullStart])
		escaped := m[2] != m[3]
		namespace := s[m[4]:m[5]]
		key := s[m[6]:m[7]]
		switch {
		case escaped:
			b.WriteString(s[fullStart+1 : fullEnd])
		case namespace == "intl":
			b.WriteString(c.Get(key))
		default:
			b.WriteString(s[fullStart:fullEnd])
		}
		last = fullEnd
	}
	b.WriteString(s[last:])
	return b.String()
}
