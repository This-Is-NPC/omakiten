package tui

import (
	"strings"
	"testing"

	"omakiten/internal/config"
)

// TestSettingsFooterTokensMatchHandlerKeys is the regression guard for
// task #348: every key advertised in the Settings sub-tab footers must
// either be wired in the sub's dedicated handler or be a global
// navigation key. This catches both flavors of drift the original
// audit found:
//
//  1. Footer advertises a key the handler ignores ("false claim") —
//     e.g. Guards previously claimed `enter=open`/`n=new`/`m=move`,
//     Laws + Skills claimed `p=skills/persona`, General claimed
//     `r=refresh`.
//  2. Handler binds a key the footer omits — e.g. General's
//     `s=subtask-kit` was reachable but invisible until this fix.
//
// Strategy: for each Settings sub, instantiate a Model wired to the
// shared en catalog so `m.t` resolves real labels, enumerate
// `footerTokens()`, and assert every key fragment is either:
//
//   - a global-nav key (cycle subs / tabs / back / help / quit),
//   - a scroll key (the handler ignores them quietly so they are
//     harmless to advertise), or
//   - a key bound in the sub's own handler.
//
// Multi-key tokens (e.g. "j/k", "up/down", "pgup/pgdn") are split on
// "/" so each fragment is checked independently.
//
// The test enumerates the inverse direction too (handler key not in
// footer) only for *primary* actions — scroll/global keys live on the
// handler without footer hints by design.
func TestSettingsFooterTokensMatchHandlerKeys(t *testing.T) {
	catalog := newTestCatalog(t)

	// Global-nav keys handled by handleCommonKey — every footer is
	// free to advertise these regardless of the active sub.
	globalNav := stringSet(
		"tab", "shift+tab",
		",", "/", ".",
		"ctrl+o", "ctrl+h",
		"esc", "?", "q",
		"1", "2", "3",
	)

	// Scroll-style keys are read-only motion. Handlers may bind some
	// of them; the renderer is allowed to surface the full motion
	// vocabulary without breaking parity.
	scrollKeys := stringSet(
		"j", "k", "up", "down",
		"pgup", "pgdn", "pgdown",
		"g", "G", "home", "end",
		"ctrl+u", "ctrl+d",
		"left", "right",
	)

	cases := []struct {
		name     string
		sub      subID
		kind     entityKind
		bindings map[string]bool
		// primaryRequired lists keys the handler binds that MUST also
		// appear in the footer (because the audit found these to be
		// invisible primary actions).
		primaryRequired []string
	}{
		{
			name: "general",
			sub:  subSettingsGeneral,
			bindings: stringSet(
				"t", "c", "s", "e",
				"down", "j", "up", "k",
				"pgdown", "ctrl+d", "pgup", "ctrl+u",
				"home", "g", "end", "G",
			),
			primaryRequired: []string{"t", "c", "s", "e"},
		},
		{
			name: "guards",
			sub:  subSettingsGuards,
			bindings: stringSet(
				"t", "c", "s", "e",
				"down", "j", "up", "k",
				"pgdown", "ctrl+d", "pgup", "ctrl+u",
				"home", "g", "end", "G",
			),
			primaryRequired: []string{"t", "c", "s", "e"},
		},
		{
			name:     "laws",
			sub:      subSettingsLaws,
			kind:     entityKindLaw,
			bindings: configHandlerBindings(entityKindLaw),
			// `p` MUST NOT be advertised (gated to Persona only) —
			// enforced below via forbiddenKeys.
		},
		{
			name:     "personas",
			sub:      subSettingsPersonas,
			kind:     entityKindPersona,
			bindings: configHandlerBindings(entityKindPersona),
			// `p` IS bound here and IS advertised.
			primaryRequired: []string{"p"},
		},
		{
			name:     "skills",
			sub:      subSettingsSkills,
			kind:     entityKindSkill,
			bindings: configHandlerBindings(entityKindSkill),
		},
		{
			name:     "templates",
			sub:      subSettingsTemplates,
			kind:     entityKindTemplate,
			bindings: configHandlerBindings(entityKindTemplate),
			// `e` was previously missing — pin so it stays advertised.
			primaryRequired: []string{"e"},
		},
		{
			name:     "tags",
			sub:      subSettingsTags,
			kind:     entityKindTag,
			bindings: configHandlerBindings(entityKindTag),
		},
	}

	forbiddenForLawsAndSkills := stringSet("p")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{
				styles:     newStyles(config.Theme{}),
				width:      160,
				height:     40,
				top:        topSettings,
				sub:        tc.sub,
				entityKind: tc.kind,
				languages:  config.LanguageSettings{CLI: "en", TUI: "en"},
				repos:      Repositories{Catalog: catalog},
			}

			tokens := m.footerTokens()
			if len(tokens) == 0 {
				t.Fatalf("%s: footerTokens returned empty list", tc.name)
			}

			footerKeys := map[string]struct{}{}
			for _, tok := range tokens {
				for _, frag := range splitKeyToken(tok.key) {
					footerKeys[frag] = struct{}{}
					if _, ok := globalNav[frag]; ok {
						continue
					}
					if _, ok := scrollKeys[frag]; ok {
						continue
					}
					if _, ok := tc.bindings[frag]; ok {
						continue
					}
					t.Errorf("%s footer advertises %q (token %q label %q) that has no handler binding", tc.name, frag, tok.key, tok.label)
				}
			}

			for _, want := range tc.primaryRequired {
				if _, ok := footerKeys[want]; !ok {
					t.Errorf("%s footer is missing required primary key %q", tc.name, want)
				}
			}

			if tc.sub == subSettingsLaws || tc.sub == subSettingsSkills {
				for forbidden := range forbiddenForLawsAndSkills {
					if _, ok := footerKeys[forbidden]; ok {
						t.Errorf("%s footer must not advertise %q — handler gates it to Persona only", tc.name, forbidden)
					}
				}
			}
		})
	}
}

// configHandlerBindings returns the key set that `handleConfigKey`
// accepts for the given entity kind. Mirrors the switch in
// `internal/tui/entity.go:23` exactly so a future handler change
// surfaces as a parity test failure.
func configHandlerBindings(kind entityKind) map[string]bool {
	keys := stringSet("esc", "up", "k", "down", "j", "t", "c")
	// `D` (orphan-delete) is tag-only.
	if kind == entityKindTag {
		keys["D"] = true
	}
	// `enter` opens entity view for everything except tag.
	if kind != entityKindTag {
		keys["enter"] = true
	}
	// `n` creates a new entity for everything except tag; template
	// emits a status hint instead of an editor, but the key is bound.
	if kind != entityKindTag {
		keys["n"] = true
	}
	// `e` opens $EDITOR for everything except tag.
	if kind != entityKindTag {
		keys["e"] = true
	}
	// `d` is bound for every kind (tag/template emit special handling).
	keys["d"] = true
	// `p` is gated to Persona only.
	if kind == entityKindPersona {
		keys["p"] = true
	}
	// `a` sets template default — template only.
	if kind == entityKindTemplate {
		keys["a"] = true
	}
	return keys
}

// splitKeyToken splits a footer key token like "j/k" or "up/down" into
// its individual key fragments. Tokens that already represent a single
// chord (e.g. "ctrl+s", "shift+tab") are returned as-is.
func splitKeyToken(key string) []string {
	// Normalize the "←/→" arrow token to the underlying "left/right"
	// fragments the handler binds.
	switch key {
	case "←/→":
		return []string{"left", "right"}
	case "↑/↓":
		return []string{"up", "down"}
	}
	parts := strings.Split(key, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return []string{key}
	}
	return out
}

// stringSet builds a set from string args. Keeps the test fixtures
// terse without dragging in a generic Set type.
func stringSet(keys ...string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

