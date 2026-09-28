package screenkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestAvailableWidthFallsBackAndReportsHonestly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		width int
		want  int
	}{
		{"unmeasured terminal assumes 120", 0, 116},
		{"negative width assumes 120", -3, 116},
		{"narrow terminal reports the real width", 20, 16},
		{"width below the gutters reports zero", 3, 0},
		{"exactly the gutters reports zero", 4, 0},
		{"former floor boundary is no longer special", 28, 24},
		{"wide terminal subtracts the gutters", 120, 116},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := (Kit{Width: tc.width}).AvailableWidth(); got != tc.want {
				t.Fatalf("AvailableWidth(width=%d) = %d, want %d", tc.width, got, tc.want)
			}
		})
	}
}

func TestAvailableWidthNeverExceedsTerminalWidth(t *testing.T) {
	t.Parallel()
	for width := 10; width <= 200; width++ {
		got := (Kit{Width: width}).AvailableWidth()
		if got > width {
			t.Fatalf("AvailableWidth(width=%d) = %d, which over-reports the terminal by %d columns", width, got, got-width)
		}
		if got < 0 {
			t.Fatalf("AvailableWidth(width=%d) = %d, want a non-negative width", width, got)
		}
	}
}

func TestViewportRowsSubtractsHostChrome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		height     int
		chromeRows int
		bodyChrome int
		want       int
	}{
		// height - (chrome + leadingBlank(1) + bodyChrome + footer(2))
		{"roomy terminal", 50, 7, 5, 35},
		{"status badge costs two rows", 50, 9, 5, 33},
		{"unmeasured height yields no budget", 0, 7, 5, 0},
		{"tiny terminal yields no budget", 12, 7, 5, 0},
		{"exactly at the four-row floor", 19, 7, 5, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kit := Kit{Height: tc.height, ChromeRows: tc.chromeRows}
			if got := kit.ViewportRows(tc.bodyChrome); got != tc.want {
				t.Fatalf("ViewportRows(%d) = %d, want %d", tc.bodyChrome, got, tc.want)
			}
			if got := kit.PanelViewportRows(tc.bodyChrome); got != tc.want {
				t.Fatalf("PanelViewportRows(%d) = %d, want %d", tc.bodyChrome, got, tc.want)
			}
		})
	}
}

func TestViewportRowsNeverReturnsNegative(t *testing.T) {
	t.Parallel()
	for height := 1; height < 40; height++ {
		kit := Kit{Height: height, ChromeRows: 9}
		if got := kit.ViewportRows(7); got < 0 {
			t.Fatalf("ViewportRows on height=%d returned %d", height, got)
		}
	}
}

func TestTResolvesThroughCatalogAndDegradesToKey(t *testing.T) {
	t.Parallel()
	if got := (Kit{}).T("tui.kicker.stats"); got != "tui.kicker.stats" {
		t.Fatalf("T without resolver = %q, want the key", got)
	}
	kit := Kit{Text: func(key string) string { return "resolved:" + key }}
	if got := kit.T("tui.kicker.stats"); got != "resolved:tui.kicker.stats" {
		t.Fatalf("T = %q, want the resolved literal", got)
	}
}

func TestTSanitizesCatalogControlsAndPreservesUnicodeAndNewlines(t *testing.T) {
	t.Parallel()
	kit := Kit{Text: func(string) string {
		return "警告\nred\x1b[31m\x1b]0;owned\a\u009b8m"
	}}
	if got, want := kit.T("tui.test"), "警告\nred"; got != want {
		t.Fatalf("T = %q, want %q", got, want)
	}
}

func TestSanitizeMultilinePreservesLFAndStripsTerminalControls(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ input, want string }{
		"C0 and LF":           {"first\nsec\x00ond\t", "first\nsecond"},
		"C1 CSI and OSC":      {"safe\u009b31mred\u009dtitle", "safered"},
		"raw C1 CSI and OSC":  {string([]byte("safe\x9b31mred\x9dtitle\x9cend")), "saferedend"},
		"ESC CSI and OSC BEL": {"safe\x1b[31mred\x1b]0;owned\aend", "saferedend"},
		"OSC preserves LF":    {"safe\x1b]0;owned\nstill-owned\aend", "safe\nend"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertSanitized(t, tc.input, tc.want)
		})
	}
}

func TestSanitizeDropsEscapeCharsetSequences(t *testing.T) {
	if got := Sanitize("safe\x1b(0text"); got != "safetext" {
		t.Fatalf("Sanitize ESC charset = %q, want safetext", got)
	}
}

func TestSanitizeMultilinePreservesTextAfterBareEscape(t *testing.T) {
	for name, input := range map[string]string{
		"unicode": "safe\x1b漢字",
		"newline": "safe\x1b\nnext",
	} {
		t.Run(name, func(t *testing.T) {
			want := strings.ReplaceAll(input, "\x1b", "")
			if got := SanitizeMultiline(input); got != want {
				t.Fatalf("SanitizeMultiline(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func assertSanitized(t *testing.T, input, want string) {
	got := SanitizeMultiline(input)
	if got != want {
		t.Fatalf("SanitizeMultiline(%q) = %q, want %q", input, got, want)
	}
	if strings.Count(got, "\n") != strings.Count(input, "\n") {
		t.Fatalf("SanitizeMultiline(%q) = %q, LF count changed", input, got)
	}
	for _, r := range got {
		if r != '\n' && (r < 0x20 || r >= 0x7f && r <= 0x9f) {
			t.Fatalf("SanitizeMultiline(%q) retained control U+%04X in %q", input, r, got)
		}
	}
}

func TestPanelAppliesLeadingBlankAndIndent(t *testing.T) {
	t.Parallel()
	kit := Kit{Styles: Styles{Panel: lipgloss.NewStyle()}}
	out := kit.Panel("body")
	if !strings.HasPrefix(out, "\n") {
		t.Fatalf("panel = %q, want a leading blank row", out)
	}
	for _, line := range strings.Split(strings.TrimPrefix(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("panel line %q is not indented two cells", line)
		}
	}
}

func TestHRuleRendersExactWidth(t *testing.T) {
	t.Parallel()
	kit := Kit{}
	if got := lipgloss.Width(kit.HRule(17)); got != 17 {
		t.Fatalf("HRule(17) width = %d, want 17", got)
	}
	if got := kit.HRule(0); got != "" {
		t.Fatalf("HRule(0) = %q, want empty", got)
	}
	// Surfaces derive the rule width as AvailableWidth()-chrome; on a terminal
	// narrower than the chrome that goes negative, and strings.Repeat panics.
	for width := -8; width < 0; width++ {
		if got := kit.HRule(width); got != "" {
			t.Fatalf("HRule(%d) = %q, want empty", width, got)
		}
	}
}

// TestHRuleSurvivesEveryTerminalWidth pins the interaction between the honest
// AvailableWidth and the six surfaces that subtract panel chrome from it: on a
// terminal too narrow to hold the chrome the subtraction is negative, and the
// rule must render nothing rather than panic.
func TestHRuleSurvivesEveryTerminalWidth(t *testing.T) {
	t.Parallel()
	for width := 1; width <= 200; width++ {
		kit := Kit{Width: width}
		for _, chrome := range []int{4, 8, 10} {
			rule := kit.HRule(kit.AvailableWidth() - chrome)
			if got := lipgloss.Width(rule); got > width {
				t.Fatalf("HRule at terminal width %d chrome %d rendered %d columns", width, chrome, got)
			}
		}
	}
}

func TestCursorMarkerSwitchesOnSelection(t *testing.T) {
	t.Parallel()
	kit := Kit{}
	if got := kit.CursorMarker(true); got != selectionMarker {
		t.Fatalf("selected marker = %q, want %q", got, selectionMarker)
	}
	if got := kit.CursorMarker(false); got != normalMarker {
		t.Fatalf("unselected marker = %q, want %q", got, normalMarker)
	}
	if lipgloss.Width(kit.CursorMarker(true)) != lipgloss.Width(kit.CursorMarker(false)) {
		t.Fatal("selected and unselected markers must occupy the same cell width")
	}
}

func TestScrollWindowSplitHintsOnlyWhenContentIsHidden(t *testing.T) {
	t.Parallel()
	items := []string{"a", "b", "c", "d", "e", "f"}
	heights := []int{1, 1, 1, 1, 1, 1}
	kit := Kit{}

	if got := kit.ScrollWindowSplit(items, heights, 0, 0); len(got) != len(items) {
		t.Fatalf("zero viewport must pass content through, got %d rows", len(got))
	}
	if got := kit.ScrollWindowSplit(items, heights, 0, 99); len(got) != len(items) {
		t.Fatalf("viewport larger than content must pass through, got %d rows", len(got))
	}
	if got := kit.ScrollWindowSplit(items, []int{1}, 0, 3); len(got) != len(items) {
		t.Fatalf("mismatched heights must pass through, got %d rows", len(got))
	}

	got := kit.ScrollWindowSplit(items, heights, 2, 4)
	if !strings.Contains(got[0], "above") {
		t.Fatalf("expected an above hint as the first row, got %q", got[0])
	}
	if !strings.Contains(got[len(got)-1], "below") {
		t.Fatalf("expected a below hint as the last row, got %q", got[len(got)-1])
	}
}

func TestScrollDataRowsReservesHintRows(t *testing.T) {
	t.Parallel()
	cases := map[int]int{0: 1, 1: 1, 2: 1, 3: 1, 10: 8}
	for viewport, want := range cases {
		if got := ScrollDataRows(viewport); got != want {
			t.Fatalf("ScrollDataRows(%d) = %d, want %d", viewport, got, want)
		}
	}
}

func TestKickerHelpers(t *testing.T) {
	t.Parallel()
	styles := Styles{}
	if got := styles.Kicker("stats"); got != "// STATS" {
		t.Fatalf("Kicker = %q", got)
	}
	if got := styles.KickerCount("activity", 6); got != "// ACTIVITY · 6" {
		t.Fatalf("KickerCount = %q", got)
	}
}

func TestTextHelpers(t *testing.T) {
	t.Parallel()
	if got := Sanitize("a\x1b[31mb\x00c"); got != "abc" {
		t.Fatalf("Sanitize = %q, want abc", got)
	}
	if got := Truncate("abcdef", 0); got != "" {
		t.Fatalf("Truncate to 0 = %q", got)
	}
	if got := Truncate("abc", 10); got != "abc" {
		t.Fatalf("Truncate no-op = %q", got)
	}
	if got := Truncate("abcdef", 4); got != "abc…" {
		t.Fatalf("Truncate = %q, want abc…", got)
	}
	if got := Truncate("日本語テキスト", 5); lipgloss.Width(got) > 5 {
		t.Fatalf("Truncate on wide glyphs produced %d cells (%q)", lipgloss.Width(got), got)
	}
	if got := Clamp(9, 1, 5); got != 5 {
		t.Fatalf("Clamp high = %d", got)
	}
	if got := Clamp(0, 1, 5); got != 1 {
		t.Fatalf("Clamp low = %d", got)
	}
	if got := Clamp(9, 5, 1); got != 5 {
		t.Fatalf("Clamp inverted range = %d, want the floor", got)
	}
	if got := Indent("a\n\nb", 2); got != "  a\n\n  b" {
		t.Fatalf("Indent = %q", got)
	}
	if got := PadRight("ab", 5); got != "ab   " {
		t.Fatalf("PadRight = %q", got)
	}
	if got := PadRight("abcdef", 3); got != "abcdef" {
		t.Fatalf("PadRight must never truncate, got %q", got)
	}
	if got := PadRight(lipgloss.NewStyle().Bold(true).Render("ab"), 5); lipgloss.Width(got) != 5 {
		t.Fatalf("PadRight must measure visible width, got %d cells", lipgloss.Width(got))
	}
	if got := PageStep(2); got != 4 {
		t.Fatalf("PageStep floor = %d, want 4", got)
	}
	if got := PageStep(30); got != 15 {
		t.Fatalf("PageStep = %d, want 15", got)
	}
}
