package tui

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	for _, name := range themeYAMLNames(t) {
		t.Run(name, func(t *testing.T) {
			assertThemeCategoryTokens(t, name)
		})
	}
}

func themeYAMLNames(t *testing.T) []string {
	t.Helper()
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
	return yamls
}

func assertThemeCategoryTokens(t *testing.T, name string) {
	t.Helper()
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
}

// A screen that holds only a Kit must be able to paint every badge the root can.
// It could not: BadgeHigh, BadgeComment and BadgeSubtask had no projection, so
// the contract carried three of the four priorities and a caller outside the
// event loop could render LOW but not HIGH.
//
// The assertion is on the resolved colours rather than on Render output: lipgloss
// degrades to a colourless profile in a test process, so two badges that differ
// only in tone render byte-identical here and the comparison would pass on a
// token wired to the wrong source.
func TestEveryBadgeReachesTheScreenContract(t *testing.T) {
	theme := config.Theme{Colors: map[string]string{
		"primary": "#39FF14", "secondary": "#8FAE9A", "border": "#494543",
		"foreground": "#E5E2E1", "success": "#86D27A", "warning": "#FFB347",
		"error": "#FF5544", "badge_fg": "#1A1A1A",
	}}
	root := newStyles(theme)
	contract := ScreenStyles(theme)

	for name, pair := range map[string][2]lipgloss.Style{
		"info":         {root.badgeInfo, contract.BadgeInfo},
		"low":          {root.badgeLow, contract.BadgeLow},
		"normal":       {root.badgeNormal, contract.BadgeNormal},
		"high":         {root.badgeHigh, contract.BadgeHigh},
		"blocker":      {root.badgeBlocker, contract.BadgeBlocker},
		"comment":      {root.badgeComment, contract.BadgeComment},
		"subtask":      {root.badgeSubtask, contract.BadgeSubtask},
		"scope":        {root.badgeScope, contract.BadgeScope},
		"fix":          {root.badgeFix, contract.BadgeFix},
		"active":       {root.badgeActive, contract.BadgeActive},
		"token green":  {root.badgeTokenGreen, contract.TokenGreen},
		"token yellow": {root.badgeTokenYellow, contract.TokenYellow},
		"token red":    {root.badgeTokenRed, contract.TokenRed},
	} {
		if want, got := pair[0].GetBackground(), pair[1].GetBackground(); want != got {
			t.Errorf("%s badge: contract background %v, root has %v", name, got, want)
		}
		if want, got := pair[0].GetForeground(), pair[1].GetForeground(); want != got {
			t.Errorf("%s badge: contract foreground %v, root has %v", name, got, want)
		}
	}
}

// The four priority tones have to be distinguishable, or the badge stops
// carrying the information it exists for.
func TestThePriorityBadgesAreDistinct(t *testing.T) {
	theme := config.Theme{Colors: map[string]string{
		"secondary": "#8FAE9A", "success": "#86D27A", "error": "#FF5544", "badge_fg": "#1A1A1A",
	}}
	contract := ScreenStyles(theme)
	seen := map[string]string{}
	for name, style := range map[string]lipgloss.Style{
		"low":    contract.BadgeLow,
		"normal": contract.BadgeNormal,
		"high":   contract.BadgeHigh,
	} {
		tone := fmt.Sprintf("%v", style.GetBackground())
		if other, clash := seen[tone]; clash {
			t.Errorf("%s and %s resolve to the same background %s", name, other, tone)
		}
		seen[tone] = name
	}
}

const hostileGlobalStatus = "failure diagnostic\x1b[31m red\x1b[0m\x1b]2;owned\a c0\r\n\t\x00del\x7fc1\u0085\u009b31m\u009dtitle"

func assertHostileStatusIsSafe(t *testing.T, rendered string) {
	t.Helper()
	for _, r := range rendered {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("rendered view retained control U+%04X in %q", r, rendered)
		}
	}
	for _, forbidden := range []string{"[31m", "]2;owned", "owned"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered view retained terminal payload %q in %q", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, "failure diagnostic") {
		t.Fatalf("rendered view lost meaningful failure copy: %q", rendered)
	}
}

func TestStatusBadgeSanitizesClassifiesAndBoundsText(t *testing.T) {
	t.Parallel()
	styles := newStyles(config.Theme{})

	for name, tc := range map[string]struct {
		message string
		level   string
	}{
		"info":                          {message: "refreshed successfully", level: "[INFO]"},
		"warn":                          {message: "confirmation pending", level: "[WARN]"},
		"error":                         {message: hostileGlobalStatus, level: "[ERROR]"},
		"stripped payload is not error": {message: "healthy\x1b]2;failure\a", level: "[INFO]"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ansi.Strip(styles.statusBadge(tc.message))
			if !strings.HasPrefix(got, tc.level+" ") {
				t.Fatalf("statusBadge(%q) = %q, want %s classification", tc.message, got, tc.level)
			}
			if name == "error" {
				assertHostileStatusIsSafe(t, got)
			}
		})
	}

	zeroWidthFlood := "failure diagnostic " + strings.Repeat("\u0301", 10_000)
	bounded := ansi.Strip(styles.statusBadge(zeroWidthFlood))
	text := strings.TrimPrefix(bounded, "[ERROR] ")
	if got := utf8.RuneCountInString(text); got > maxStatusTextRunes {
		t.Fatalf("status text retained %d runes from a zero-width flood, want at most %d", got, maxStatusTextRunes)
	}
	if got := ansi.StringWidth(text); got > maxStatusTextCells {
		t.Fatalf("status text is %d cells wide, want at most %d", got, maxStatusTextCells)
	}
}

func TestStatusBadgePreservesControlWhitespaceBoundaries(t *testing.T) {
	t.Parallel()
	styles := newStyles(config.Theme{})

	for name, tc := range map[string]struct {
		message string
		want    string
	}{
		"readable boundary":           {message: "load\nfailed", want: "[ERROR] load failed"},
		"does not synthesize keyword": {message: "fa\nil", want: "[INFO] fa il"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ansi.Strip(styles.statusBadge(tc.message)); got != tc.want {
				t.Fatalf("statusBadge(%q) = %q, want %q", tc.message, got, tc.want)
			}
		})
	}
}
