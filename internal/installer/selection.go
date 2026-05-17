package installer

import (
	"fmt"
	"strconv"
	"strings"

	"omakiten/internal/agentsetup"
	"omakiten/internal/config"
)

// SelectionStatus mirrors the four exit codes the bash
// parse_harness_selection emits to drive the install.sh retry loop.
// Mapping is intentionally 1:1 so the new Go path and the legacy shell
// path can share scripts/installer_select_test.sh expectations: both
// the new code and the old code recognise the same four shapes of
// user input.
//
//	StatusOK      — at least one valid entry parsed (rc 0)
//	StatusInvalid — every token failed to parse (rc 1)
//	StatusSkip    — the explicit "0" / "skip" / "none" sentinel won (rc 2)
//	StatusEmpty   — the input contained no tokens (rc 3)
type SelectionStatus int

const (
	StatusOK SelectionStatus = iota
	StatusInvalid
	StatusSkip
	StatusEmpty
)

// SupportedHarnesses returns the canonical MCP-harness slug list the
// installer offers in the multi-select. Order matters — the numeric
// "1,3,5" shorthand input format the bash installer accepts indexes
// into this slice 1-based, so adding a harness at the front would shift
// every numeric mapping users learned. The Go installer goes through
// the same slice for the same reason.
//
// The list is sourced from internal/agentsetup so the picker and the
// MCP-setup writer agree on which harnesses exist; drift here would
// either surface harnesses the writer cannot configure, or hide
// harnesses that ship with a working writer.
func SupportedHarnesses() []string {
	return agentsetup.SupportedHarnesses()
}

// SupportedPresets returns the official preset names in menu order.
// Thin re-export of config.ListPresets to keep the installer
// self-contained — callers only need to import this package.
func SupportedPresets() []string {
	presets := config.ListPresets()
	out := make([]string, len(presets))
	for i, p := range presets {
		out[i] = p.Name
	}
	return out
}

// DefaultPreset is the preset selected on empty input and when an
// unknown OKT_PRESET= override is supplied. Kept aligned with
// install.sh's DEFAULT_PRESET literal and the .mise.toml
// `OKT_PRESET = "omakase"` default in the dev:install task.
const DefaultPreset = "omakase"

// ParseHarnessSelection mirrors install.sh's parse_harness_selection.
// raw is a free-form string of harness numbers and/or names separated
// by comma / whitespace / newlines; the returned slice carries the
// canonical names in the order they were specified, and Status reports
// which of the four exit shapes applied. Unknown tokens are collected
// into Warnings so the caller can echo them on stderr without having
// to re-parse.
//
// Numeric tokens are 1-based and resolved against SupportedHarnesses().
// Out-of-range numerics and unknown names contribute a warning but do
// not abort the parse — a valid token still produces StatusOK alongside
// the warning, matching the bash behaviour where "1,bogus" yields one
// harness on stdout plus a stderr warning.
//
// The "0" / "skip" / "none" sentinel (case-insensitive) wins over every
// other token in the same input — even one valid entry — and produces
// StatusSkip with an empty slice. install.sh's contract treats this as
// "user explicitly chose to wire up nothing".
func ParseHarnessSelection(raw string) (harnesses []string, status SelectionStatus, warnings []string) {
	supported := SupportedHarnesses()
	tokens := splitSelectionTokens(raw)
	if len(tokens) == 0 {
		return nil, StatusEmpty, nil
	}

	// First pass: detect the skip sentinel. install.sh exits with rc=2
	// the moment it sees "0"/"skip"/"none", so any matching token wins
	// over earlier valid entries in the same input ("1,0" → skip).
	for _, tok := range tokens {
		if isSkipSentinel(tok) {
			return nil, StatusSkip, nil
		}
	}

	for _, tok := range tokens {
		if name, ok := matchHarnessName(tok, supported); ok {
			harnesses = append(harnesses, name)
			continue
		}
		if idx, ok := parseIndex(tok); ok {
			if idx >= 1 && idx <= len(supported) {
				harnesses = append(harnesses, supported[idx-1])
				continue
			}
			warnings = append(warnings, fmt.Sprintf("index %s out of range", tok))
			continue
		}
		warnings = append(warnings, fmt.Sprintf("ignoring unknown harness %q", tok))
	}
	if len(harnesses) == 0 {
		return nil, StatusInvalid, warnings
	}
	return harnesses, StatusOK, warnings
}

// ResolvePreset turns a free-form preset selection (env var value or
// picker input) into a canonical preset name, falling back to
// DefaultPreset on empty input and on names that are not in the
// supported list. Returns the resolved name plus a `fellback` flag so
// the caller can surface a warning when an unknown OKT_PRESET= value
// was silently coerced.
//
// Mirrors install.sh's select_preset:
//   - empty input → DefaultPreset, fellback=false (this is the
//     documented default-on-enter behaviour, not a coercion);
//   - numeric tokens are 1-based into SupportedPresets;
//   - unknown name → DefaultPreset, fellback=true so the caller can
//     emit the same `warn: OKT_PRESET=… is not a supported preset`
//     line the bash script prints today.
func ResolvePreset(raw string) (name string, fellback bool) {
	supported := SupportedPresets()
	token := strings.TrimSpace(raw)
	if token == "" {
		return DefaultPreset, false
	}
	if idx, ok := parseIndex(token); ok {
		if idx >= 1 && idx <= len(supported) {
			return supported[idx-1], false
		}
		return DefaultPreset, true
	}
	lower := strings.ToLower(token)
	for _, p := range supported {
		if p == lower {
			return p, false
		}
	}
	return DefaultPreset, true
}

// splitSelectionTokens splits on the same set of separators bash's
// `IFS=$',\t \n'; set -- $raw` recognises: comma, tab, space, newline.
// Empty tokens (runs of separators, leading/trailing whitespace) are
// dropped so "  1, ,3 " → ["1","3"].
func splitSelectionTokens(raw string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(raw, isSelectionSeparator) {
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func isSelectionSeparator(r rune) bool {
	switch r {
	case ',', '\t', ' ', '\n', '\r':
		return true
	}
	return false
}

func isSkipSentinel(tok string) bool {
	switch strings.ToLower(tok) {
	case "0", "skip", "none":
		return true
	}
	return false
}

func matchHarnessName(tok string, supported []string) (string, bool) {
	for _, name := range supported {
		if name == tok {
			return name, true
		}
	}
	return "", false
}

func parseIndex(tok string) (int, bool) {
	idx, err := strconv.Atoi(tok)
	if err != nil {
		return 0, false
	}
	return idx, true
}
