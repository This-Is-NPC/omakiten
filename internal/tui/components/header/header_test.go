package header_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/components/header"
)

func testStyles() header.Styles {
	return header.Styles{
		Title:     lipgloss.NewStyle().Bold(true),
		Nav:       lipgloss.NewStyle(),
		ActiveNav: lipgloss.NewStyle().Bold(true),
		Hint:      lipgloss.NewStyle().Faint(true),
	}
}

func TestRenderFourStates(t *testing.T) {
	t.Parallel()
	styles := testStyles()
	base := header.Options{
		Width:          120,
		Brand:          "omakiten",
		Segment:        "demo",
		SegmentHint:    "local checkpoint",
		HomeLabel:      "00 // HOME",
		CompactHint:    "  tab/1-3 switch zones",
		HomeReturnHint: "  ctrl+h returns here",
		Tops: []header.Item{
			{Label: "01 // TASKS", Active: true},
			{Label: "02 // STATS"},
			{Label: "03 // SETTINGS"},
		},
		Subs: []header.Item{
			{Label: "// BOARD", Active: true},
			{Label: "// TABLE"},
		},
	}

	home := header.Render(styles, with(base, func(o *header.Options) { o.Home = true; o.Segment = "home"; o.SegmentHint = "select a project" }))
	if !strings.Contains(home, "select a project") || !strings.Contains(home, "00 // HOME") {
		t.Fatalf("home state missing title:\n%s", home)
	}
	if strings.Contains(home, "01 // TASKS") {
		t.Fatalf("home state must suppress the project nav strip:\n%s", home)
	}

	overlay := header.Render(styles, with(base, func(o *header.Options) { o.Overlay = true }))
	if strings.Contains(overlay, "01 // TASKS") || strings.Contains(overlay, "\n\n") {
		t.Fatalf("overlay state must be breadcrumb only:\n%s", overlay)
	}

	full := header.Render(styles, base)
	if !strings.Contains(full, "01 // TASKS") || !strings.Contains(full, "// BOARD") {
		t.Fatalf("full state missing strips:\n%s", full)
	}

	compact := header.Render(styles, with(base, func(o *header.Options) { o.Width = 40 }))
	if !strings.Contains(compact, "01 // TASKS") {
		t.Fatalf("compact state must keep the active top:\n%s", compact)
	}
	if strings.Contains(compact, "02 // STATS") {
		t.Fatalf("compact state must drop neighbour tops:\n%s", compact)
	}
	if !strings.Contains(compact, "tab/1-3 switch zones") {
		t.Fatalf("compact state missing hint:\n%s", compact)
	}
}

func TestHeightMatchesRender(t *testing.T) {
	t.Parallel()
	styles := testStyles()
	opts := header.Options{
		Width: 120, Brand: "omakiten", Segment: "demo", SegmentHint: "local checkpoint",
		HomeLabel: "00 // HOME",
		Tops:      []header.Item{{Label: "01 // TASKS", Active: true}},
		Subs:      []header.Item{{Label: "// BOARD", Active: true}, {Label: "// TABLE"}},
	}
	out := header.Render(styles, opts)
	if got, want := header.Height(styles, opts), lipgloss.Height(out); got != want {
		t.Fatalf("Height = %d, Render height = %d", got, want)
	}
}

func TestStackChargesMiddleSlot(t *testing.T) {
	t.Parallel()
	top := "header\nline2"
	middle := "a\nb\nc\nd\ne"
	bottom := "footer"
	// top 2 + bottom 1 = 3; height 5 → middle budget 2.
	out := header.Stack(5, top, middle, bottom)
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("Stack height = %d, want 5:\n%s", len(lines), out)
	}
	if lines[0] != "header" || lines[1] != "line2" {
		t.Fatalf("top not preserved: %v", lines[:2])
	}
	if lines[2] != "a" || lines[3] != "b" {
		t.Fatalf("middle should keep the first two rows, got %v", lines[2:4])
	}
	if lines[4] != "footer" {
		t.Fatalf("bottom not anchored: %q", lines[4])
	}
	// The cut dropped c/d/e — an explicit middle charge, not a silent join chop.
	if strings.Contains(out, "c") {
		t.Fatalf("overdrawing middle rows must be charged away:\n%s", out)
	}
}

func with(base header.Options, edit func(*header.Options)) header.Options {
	edit(&base)
	return base
}

// The header is handed a width. Every row it paints has to fit inside it, at
// every width, in every one of the four states — that is what "handed a width"
// means and it was not true: the breadcrumb was concatenated without measuring,
// and the compact fallback that exists to stop the full strip overflowing
// overflowed on its own.
func TestNoRowEverLeavesTheWidthItWasGiven(t *testing.T) {
	long := "architecture-review-closeout-tui-components"
	tops := []header.Item{{Label: "01 // TASKS", Active: true}, {Label: "02 // STATS"}, {Label: "03 // SETTINGS"}}
	subs := []header.Item{{Label: "// BOARD", Active: true}, {Label: "// TABLE"}}
	hint := "  tab/1-3 switch zones · ,/. switch"
	states := map[string]header.Options{
		"full":    {Brand: "omakiten", Segment: long, SegmentHint: "local checkpoint", Tops: tops, Subs: subs, CompactHint: hint},
		"compact": {Brand: "omakiten", Segment: long, SegmentHint: "local checkpoint", Tops: tops, CompactHint: hint},
		"overlay": {Brand: "omakiten", Segment: long, SegmentHint: "local checkpoint", Overlay: true},
		"home":    {Brand: "omakiten", Segment: long, Home: true, HomeLabel: "00 // HOME", HomeReturnHint: "  ctrl+h returns here"},
	}
	for name, base := range states {
		for width := 12; width <= 140; width += 4 {
			opts := base
			opts.Width = width
			for i, row := range strings.Split(header.Render(testStyles(), opts), "\n") {
				if got := lipgloss.Width(row); got > width {
					t.Errorf("%s at width %d: row %d is %d cells: %q", name, width, i, got, row)
				}
			}
		}
	}
}

func TestRenderSanitizesDynamicTextWithoutMutatingOptions(t *testing.T) {
	hostile := "project 漢字 \x1b[31mred\x1b]0;owned\a C1\u009b31m\u009d"
	tops := []header.Item{{Label: hostile, Active: true}}
	opts := header.Options{
		Width: 80, Brand: hostile, Segment: hostile, SegmentHint: hostile,
		HomeLabel: hostile, CompactHint: hostile, HomeReturnHint: hostile,
		Tops: tops, Subs: []header.Item{{Label: hostile}},
	}
	plain := ansi.Strip(header.Render(testStyles(), opts))
	if strings.Contains(plain, "owned") {
		t.Fatalf("header retained OSC payload: %q", plain)
	}
	if strings.IndexFunc(plain, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) >= 0 {
		t.Fatalf("header retained terminal control: %q", plain)
	}
	if !strings.Contains(plain, "漢字") {
		t.Fatalf("header lost harmless Unicode: %q", plain)
	}
	if opts.Tops[0].Label != hostile || opts.Subs[0].Label != hostile {
		t.Fatal("header sanitization mutated caller-owned option slices")
	}
}
