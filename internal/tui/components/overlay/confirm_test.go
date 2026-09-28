package overlay

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

func testConfirmStyles() Styles {
	return Styles{
		Info:      lipgloss.NewStyle(),
		Hint:      lipgloss.NewStyle(),
		Key:       lipgloss.NewStyle(),
		Footer:    lipgloss.NewStyle(),
		Separator: lipgloss.NewStyle(),
	}
}

func TestRenderConfirm_hostsFieldsWithoutNotificationBubble(t *testing.T) {
	t.Parallel()
	got := RenderConfirm(Confirm{
		Width:  48,
		Styles: testConfirmStyles(),
		Kicker: "// APPLY CANDIDATE",
		Hint:   "ctrl+s applies · esc discards",
		Fields: []ConfirmField{{Label: "validation", Value: "none"}},
		Footer: "ctrl+s apply   esc discard",
	})
	if strings.Contains(got, "“") || strings.Contains(got, "”") {
		t.Fatalf("confirm leaf must not use Card's notification bubble quotes:\n%s", got)
	}
	if !strings.Contains(got, "// VALIDATION") {
		t.Fatalf("confirm leaf must paint candidate field labels:\n%s", got)
	}
}

func TestRenderConfirmSanitizesListAndMessageText(t *testing.T) {
	hostile := "warning 漢字 \x1b[31mred\x1b]0;owned\a C1\u009b31m\u009d"
	got := RenderConfirm(Confirm{
		Width: 64, Kicker: "// APPLY", Hint: hostile,
		Styles:    testConfirmStyles(),
		Fields:    []ConfirmField{{Label: hostile, Value: hostile}},
		ListTitle: hostile, ListItems: []string{hostile}, Message: hostile, Footer: hostile,
	})
	if strings.Contains(got, "owned") {
		t.Fatalf("confirm retained OSC payload: %q", got)
	}
	if strings.IndexFunc(got, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("confirm retained terminal control: %q", got)
	}
	if !strings.Contains(got, "漢字") {
		t.Fatalf("confirm lost harmless Unicode: %q", got)
	}
}
