package choice

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func TestGlyphsMatchWhatThePickersPaint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		mode     Mode
		selected bool
		want     string
	}{
		{"radio selected", Radio, true, "•"},
		{"radio idle", Radio, false, " "},
		{"checkbox selected", Checkbox, true, "[x]"},
		{"checkbox idle", Checkbox, false, "[ ]"},
		{"toggle selected", Toggle, true, "◉"},
		{"toggle idle", Toggle, false, "☐"},
		{"unknown mode", Mode(99), true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Glyph(c.mode, c.selected); got != c.want {
				t.Errorf("Glyph(%v, %v) = %q, want %q", c.mode, c.selected, got, c.want)
			}
		})
	}
}

func TestRowJoinsGlyphAndLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mode Mode
		opt  Option
		want string
	}{
		{"radio selected", Radio, Option{Label: "Active", Selected: true}, "• Active"},
		{"radio idle", Radio, Option{Label: "Candidate"}, "  Candidate"},
		{"checkbox selected", Checkbox, Option{Label: "skill", Selected: true}, "[x] skill"},
		{"checkbox idle", Checkbox, Option{Label: "skill"}, "[ ] skill"},
		{"toggle selected", Toggle, Option{Label: "on", Selected: true}, "◉ on"},
		{"toggle idle", Toggle, Option{Label: "off"}, "☐ off"},
		{"empty option", Radio, Option{}, "  "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Row(c.mode, c.opt); got != c.want {
				t.Errorf("Row = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRowDoesNotPaintDetail(t *testing.T) {
	t.Parallel()
	opt := Option{Label: "Active", Detail: "current.yaml", Selected: true}
	got := Row(Radio, opt)
	if strings.Contains(got, "current.yaml") {
		t.Errorf("Row painted Detail: %q", got)
	}
	if got != "• Active" {
		t.Errorf("Row = %q, want %q", got, "• Active")
	}
}

func TestRowSanitizesLabelWithoutChangingSelectionGlyph(t *testing.T) {
	t.Parallel()
	const hostile = "selected\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	got := Row(Checkbox, Option{Label: hostile, Selected: true})
	plain := ansi.Strip(got)
	if !strings.HasPrefix(plain, "[x] ") {
		t.Fatalf("selection glyph changed: %q", plain)
	}
	if strings.Contains(plain, "owned") || !strings.Contains(plain, "漢字") {
		t.Fatalf("label boundary failed: %q", plain)
	}
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("label retained control U+%04X: %q", r, plain)
		}
	}
}
