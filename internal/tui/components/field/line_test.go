package field

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

func plain() screenkit.Styles {
	return screenkit.Styles{
		Input:      lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1),
		HintAccent: lipgloss.NewStyle(),
		Cursor:     lipgloss.NewStyle(),
	}
}

func lineField(value string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.SetValue(value)
	return input
}

func TestThePromptCarriesTheLabelAndTheValue(t *testing.T) {
	got := RenderLine(plain(), "target bucket", lineField("dev"), 20)
	if !strings.Contains(got, "target bucket:") {
		t.Errorf("prompt = %q, want the label and a colon", got)
	}
	if !strings.Contains(got, "dev") {
		t.Errorf("prompt = %q, want the typed value", got)
	}
}

func TestThePromptSanitizesDisplayOnlyLabels(t *testing.T) {
	label := "target 漢字 \x1b[31mbucket\x1b]0;owned\a C1\u009b31m\u009d"
	got := RenderLine(plain(), label, lineField("dev"), 40)
	if strings.Contains(got, "owned") {
		t.Fatalf("prompt retained OSC payload: %q", got)
	}
	if strings.IndexFunc(got, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("prompt retained terminal control: %q", got)
	}
	if !strings.Contains(got, "漢字") {
		t.Fatalf("prompt lost harmless Unicode: %q", got)
	}
}

func TestTheCallersInputIsNotMutated(t *testing.T) {
	// Sizing the caller's model would fight whatever sized it last — the
	// persistent one is calibrated on resize and does not know this label.
	input := lineField("dev")
	input.Width = 99
	RenderLine(plain(), "label", input, 20)
	if input.Width != 99 {
		t.Errorf("RenderLine resized the caller's model to %d — it must size a copy", input.Width)
	}
}

func TestTheRowIsIndentedToMatchTheBodyAroundIt(t *testing.T) {
	for _, row := range strings.Split(RenderLine(plain(), "l", lineField(""), 10), "\n") {
		if row == "" {
			continue
		}
		if !strings.HasPrefix(row, strings.Repeat(" ", Indent)) {
			t.Errorf("row %q is not indented %d columns", row, Indent)
		}
	}
}

func TestTheTypingRoomHasAFloor(t *testing.T) {
	s := plain()
	cases := []struct {
		name                         string
		available, labelWidth, floor int
		want                         int
	}{
		{"room to spare", 80, 10, 8, 80 - 10 - chrome(s)},
		{"exactly at the floor falls back", 21, 10, 8, 8},
		// Below the floor the row is allowed to overflow: an input with no
		// columns cannot be typed into, which is worse than a long line.
		{"narrower than the label", 12, 30, 8, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Width(s, c.available, c.labelWidth, c.floor); got != c.want {
				t.Errorf("Width(%d, %d, %d) = %d, want %d", c.available, c.labelWidth, c.floor, got, c.want)
			}
		})
	}
}

// The chrome has to be MEASURED, not declared: the first version assumed 4 and
// every row overran by three, because the theme pads two columns a side and
// bubbles keeps a cell for the cursor. This pins the row to the budget it was
// given, at a padding the fixture chooses, so a wrong constant cannot pass.
func TestTheRowFitsTheBudgetAtAnyPadding(t *testing.T) {
	for _, padding := range []int{0, 1, 2, 3} {
		s := plain()
		s.Input = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, padding)
		const available, floor = 60, 8
		label := "target bucket"
		width := Width(s, available, lipgloss.Width(label+": "), floor)
		row := lipgloss.Width(strings.Split(RenderLine(s, label, lineField("value"), width), "\n")[0])
		if row != available {
			t.Errorf("padding %d: row is %d columns, want the %d it was given", padding, row, available)
		}
	}
}
