package tui

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"omakiten/defaults"
	"omakiten/internal/config"
)

// categoryTokens is the canonical set of theme keys the Logs event inspector
// reads. Every shipped theme YAML must populate every entry; missing keys
// must fall back to the generic `hint` style without panicking.
//
// Keep in sync with the `hint*` fields on the `styles` struct and the
// `categoryColor` resolver in newStyles. When you add a category, append
// it here AND populate every defaults/themes/*.yaml — the parity test
// fails until both ends move together.
var categoryTokens = []string{
	"category.tasks",
	"category.comment",
	"category.plan",
	"category.audit",
	"category.guard",
	"category.trick",
	"category.tool_call",
}

// rgba is a small alias for the RGBA-based comparison used across the
// category-style tests. Comparing lipgloss.TerminalColor instances directly
// is brittle (interface identity); their RGBA tuples are stable.
func rgba(c lipgloss.TerminalColor) (uint32, uint32, uint32, uint32) {
	if c == nil {
		return 0, 0, 0, 0
	}
	return c.RGBA()
}

// TestCategoryStylesResolveFromTheme asserts every new category style picks
// up the explicit token from the theme — proves the wiring in newStyles
// reads each `category.*` key into the matching `hint*` field.
func TestCategoryStylesResolveFromTheme(t *testing.T) {
	colors := map[string]string{
		"category.tasks":     "#112233",
		"category.comment":   "#223344",
		"category.plan":      "#334455",
		"category.audit":     "#445566",
		"category.guard":     "#556677",
		"category.trick":     "#667788",
		"category.tool_call": "#778899",
	}
	theme := config.Theme{Version: 1, Key: "test", Name: "Test", Colors: colors}
	s := newStyles(theme)

	cases := map[string]struct {
		got  lipgloss.TerminalColor
		want string
	}{
		"hintTasks":    {s.hintTasks.GetForeground(), colors["category.tasks"]},
		"hintComment":  {s.hintComment.GetForeground(), colors["category.comment"]},
		"hintPlan":     {s.hintPlan.GetForeground(), colors["category.plan"]},
		"hintAudit":    {s.hintAudit.GetForeground(), colors["category.audit"]},
		"hintGuard":    {s.hintGuard.GetForeground(), colors["category.guard"]},
		"hintTrick":    {s.hintTrick.GetForeground(), colors["category.trick"]},
		"hintToolCall": {s.hintToolCall.GetForeground(), colors["category.tool_call"]},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gotR, gotG, gotB, _ := rgba(tc.got)
			wantR, wantG, wantB, _ := rgba(lipgloss.Color(tc.want))
			if gotR != wantR || gotG != wantG || gotB != wantB {
				t.Fatalf("%s foreground RGBA: got (%d,%d,%d), want (%d,%d,%d) for %s",
					name, gotR, gotG, gotB, wantR, wantG, wantB, tc.want)
			}
		})
	}
}

// TestCategoryStylesFallBackToHint pins the no-panic / no-blank contract:
// a theme that omits every category token must still produce styles whose
// foreground matches the generic `hint` style. AC #3 / DoD bullet 4.
func TestCategoryStylesFallBackToHint(t *testing.T) {
	// Theme with only the baseline tokens — none of the category.* keys
	// present. Mirrors a custom or pre-Logs theme on disk.
	theme := config.Theme{
		Version: 1,
		Key:     "fallback",
		Name:    "Fallback",
		Colors: map[string]string{
			"border": "#ABCDEF",
		},
	}

	s := newStyles(theme)
	wantR, wantG, wantB, _ := rgba(s.hint.GetForeground())

	cases := map[string]lipgloss.Style{
		"hintTasks":    s.hintTasks,
		"hintComment":  s.hintComment,
		"hintPlan":     s.hintPlan,
		"hintAudit":    s.hintAudit,
		"hintGuard":    s.hintGuard,
		"hintTrick":    s.hintTrick,
		"hintToolCall": s.hintToolCall,
	}

	for name, style := range cases {
		t.Run(name, func(t *testing.T) {
			gotR, gotG, gotB, _ := rgba(style.GetForeground())
			if gotR != wantR || gotG != wantG || gotB != wantB {
				t.Fatalf("%s fallback RGBA: got (%d,%d,%d), want hint (%d,%d,%d)",
					name, gotR, gotG, gotB, wantR, wantG, wantB)
			}
		})
	}
}

// TestNewStylesNoPanicOnEmptyTheme guards AC #4: any combination of theme
// tokens — including the empty theme — must produce a valid styles struct.
func TestNewStylesNoPanicOnEmptyTheme(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("newStyles panicked on empty theme: %v", r)
		}
	}()
	_ = newStyles(config.Theme{})
}

// TestThemesPopulateEveryCategoryToken is the parity gate: every shipped
// theme YAML under defaults/themes/ must declare every entry in
// categoryTokens. Adding a new category without populating the YAMLs (or
// adding a YAML that forgets the new color) fails this test by name.
func TestThemesPopulateEveryCategoryToken(t *testing.T) {
	entries, err := fs.ReadDir(defaults.FS, "themes")
	if err != nil {
		t.Fatalf("read embedded themes/: %v", err)
	}

	var yamls []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		yamls = append(yamls, e.Name())
	}
	if len(yamls) == 0 {
		t.Fatalf("no theme YAMLs embedded under defaults/themes")
	}

	for _, name := range yamls {
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(defaults.FS, "themes/"+name)
			if err != nil {
				t.Fatalf("read themes/%s: %v", name, err)
			}
			var theme config.Theme
			if err := yaml.Unmarshal(data, &theme); err != nil {
				t.Fatalf("parse themes/%s: %v", name, err)
			}
			for _, token := range categoryTokens {
				if value := strings.TrimSpace(theme.Colors[token]); value == "" {
					t.Errorf("themes/%s: missing color %q (add it to keep the Logs event inspector tonally on-brand; fallback would be the muted `border` color)", name, token)
				}
			}
		})
	}
}
