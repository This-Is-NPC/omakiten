package dropdown

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/components/choice"
)

func TestRowPaintsGlyphLabelAndEachDetailJoin(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	cases := []struct {
		name   string
		marker string
		opt    Option
		join   DetailJoin
		want   string
	}{
		{
			name:   "radio selected",
			marker: "▌",
			opt:    Option{Label: "Active", Selected: true, Mode: choice.Radio},
			want:   "▌ • Active",
		},
		{
			name:   "radio idle",
			marker: " ",
			opt:    Option{Label: "Candidate", Mode: choice.Radio},
			want:   "    Candidate",
		},
		{
			name:   "checkbox selected",
			marker: "▌",
			opt:    Option{Label: "skill", Selected: true, Mode: choice.Checkbox},
			want:   "▌ [x] skill",
		},
		{
			name:   "checkbox idle",
			marker: " ",
			opt:    Option{Label: "skill", Mode: choice.Checkbox},
			want:   "  [ ] skill",
		},
		{
			name:   "detail spaced",
			marker: "▌",
			opt:    Option{Label: "Active", Detail: "current.yaml", Selected: true, Mode: choice.Radio},
			join:   DetailSpaced,
			want:   "▌ • Active  current.yaml",
		},
		{
			name:   "detail dash",
			marker: "▌",
			opt:    Option{Label: "Go", Detail: "Go engineering", Selected: true, Mode: choice.Checkbox},
			join:   DetailDash,
			want:   "▌ [x] Go — Go engineering",
		},
		{
			name:   "detail equal to label is skipped",
			marker: "▌",
			opt:    Option{Label: "task", Detail: "task", Selected: true, Mode: choice.Radio},
			join:   DetailSpaced,
			want:   "▌ • task",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Row(c.marker, c.opt, hint, c.join); got != c.want {
				t.Errorf("Row = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRowAppendsTrailing(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	got := Row(" ", Option{
		Label:    "Candidate",
		Detail:   "candidate.yaml",
		Trailing: "CUSTOM",
		Mode:     choice.Radio,
	}, hint, DetailSpaced)
	want := "    Candidate  candidate.yaml CUSTOM"
	if got != want {
		t.Errorf("Row = %q, want %q", got, want)
	}
}

func TestRowsEmptyOptionsAreAnEmptySlice(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	got := Rows(Spec{Open: true}, "▌", " ", hint)
	if len(got) != 0 {
		t.Errorf("empty options = %#v, want empty slice", got)
	}
}

func TestRowsOpenFalsePaintsTheSelectedOrFirstOption(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	opts := []Option{
		{Label: "Active", Mode: choice.Radio},
		{Label: "Candidate", Selected: true, Mode: choice.Radio},
	}
	got := Rows(Spec{Options: opts, Open: false, Join: DetailSpaced}, "▌", " ", hint)
	if len(got) != 1 {
		t.Fatalf("collapsed len = %d, want 1", len(got))
	}
	if got[0] != "  • Candidate" {
		t.Errorf("collapsed = %q, want the selected option", got[0])
	}
	got = Rows(Spec{Options: []Option{{Label: "Active", Mode: choice.Radio}}, Open: false}, "▌", " ", hint)
	if len(got) != 1 || got[0] != "    Active" {
		t.Errorf("collapsed first = %#v, want the first option", got)
	}
}

func TestRowsFilterPrependsChrome(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	got := Rows(Spec{
		Options: []Option{{Label: "Active", Selected: true, Mode: choice.Radio}},
		Cursor:  0,
		Open:    true,
		Filter:  "act",
	}, "▌", " ", hint)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != "/ act" {
		t.Errorf("filter line = %q, want %q", got[0], "/ act")
	}
	if got[1] != "▌ • Active" {
		t.Errorf("option = %q", got[1])
	}
}

func TestRowsPerRowMode(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	got := Rows(Spec{
		Options: []Option{
			{Label: "Go", Selected: true, Mode: choice.Checkbox},
			{Label: "+ create", Mode: choice.Radio},
		},
		Cursor: 0,
		Open:   true,
		Join:   DetailDash,
	}, "▌", " ", hint)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != "▌ [x] Go" {
		t.Errorf("checkbox row = %q, want %q", got[0], "▌ [x] Go")
	}
	if got[1] != "    + create" {
		t.Errorf("radio create row = %q, want %q", got[1], "    + create")
	}
}

func TestRowsMatchesSettingsPickerBytes(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	got := Rows(Spec{
		Options: []Option{
			{Label: "Active choice", Detail: "active.yaml", Selected: true, Mode: choice.Radio},
			{Label: "Candidate choice", Detail: "candidate.yaml", Trailing: "CUSTOM", Mode: choice.Radio},
		},
		Cursor: 0,
		Open:   true,
		Join:   DetailSpaced,
	}, "▌", " ", hint)
	want := []string{
		"▌ • Active choice  active.yaml",
		"    Candidate choice  candidate.yaml CUSTOM",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRowsMatchesRelationshipPickerBytes(t *testing.T) {
	t.Parallel()
	var hint lipgloss.Style
	got := Rows(Spec{
		Options: []Option{
			{Label: "Go", Detail: "Go engineering", Selected: true, Mode: choice.Checkbox},
			{Label: "SQLite", Detail: "Data persistence", Mode: choice.Checkbox},
			{Label: "+ create new skill (opens $EDITOR)", Mode: choice.Radio},
		},
		Cursor: 0,
		Open:   true,
		Join:   DetailDash,
	}, "▌", " ", hint)
	want := []string{
		"▌ [x] Go — Go engineering",
		"  [ ] SQLite — Data persistence",
		"    + create new skill (opens $EDITOR)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRowSanitizesLabelDetailAndFilterAtTheLeaf(t *testing.T) {
	t.Parallel()
	const hostile = "choice\x1b[31mred\x1b]0;owned\a\x00\u009b31m\u0085漢字"
	rows := Rows(Spec{
		Options: []Option{{Label: hostile, Detail: hostile, Selected: true, Mode: choice.Radio}},
		Open:    true,
		Filter:  hostile,
	}, "▌", " ", lipgloss.NewStyle())
	plain := ansi.Strip(strings.Join(rows, "\n"))
	if len(rows) != 2 || !strings.Contains(plain, "漢字") || strings.Contains(plain, "owned") {
		t.Fatalf("sanitized picker rows = %q", plain)
	}
	for _, r := range plain {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("picker retained control U+%04X: %q", r, plain)
		}
	}
}
