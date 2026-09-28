// Package dropdown paints a selectable option list: cursor chrome, control
// glyph, label, optional detail, optional trailing. Screens translate
// domain into Option and DetailJoin; this package does not know what
// the option is.
//
// It is a leaf: presentation props in, bytes out. No config, domain,
// app, sqlite, parent tui import, or list. Cursor chrome is passed in
// (kit.CursorMarker), the same as choice: the package does not import
// how the screen spells the chevron.
//
// choice.Row does not paint Detail — the two picker screens disagree
// on the join — so this package owns the join via DetailJoin.
package dropdown

import (
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/choice"
	"omakiten/internal/tui/components/screenkit"
)

// Option is one selectable row. Screens fill it from domain; this
// package styles Detail and appends Trailing as already-painted bytes.
type Option struct {
	Label    string
	Detail   string // raw; package styles it
	Trailing string // already painted (e.g. custom badge); empty skips
	Selected bool
	Mode     choice.Mode // per-row; Radio vs Checkbox
}

// DetailJoin is how Detail is attached after the label. The two picker
// screens already disagree; a flag lets them stop concatenating.
type DetailJoin int

const (
	DetailSpaced DetailJoin = iota // "  "+hint(detail) — settings picker
	DetailDash                     // " "+hint("— "+detail) — relationship picker
)

// Spec is one dropdown paint. Screens keep Open true and Filter empty;
// the gallery can show a collapsed trigger and a filter row.
type Spec struct {
	Options []Option
	Cursor  int
	Open    bool   // screens always true; gallery can show collapsed
	Filter  string // empty = no filter chrome
	Join    DetailJoin
}

// Row is marker, glyph, label, optional styled Detail, optional Trailing.
// Detail is skipped when empty or equal to Label, matching both pickers.
func Row(marker string, opt Option, hint lipgloss.Style, join DetailJoin) string {
	label := screenkit.Sanitize(opt.Label)
	detail := screenkit.Sanitize(opt.Detail)
	row := marker + " " + choice.Row(opt.Mode, choice.Option{Label: label, Selected: opt.Selected})
	if detail != "" && detail != label {
		switch join {
		case DetailDash:
			row += " " + hint.Render("— "+detail)
		default:
			row += "  " + hint.Render(detail)
		}
	}
	if opt.Trailing != "" {
		row += " " + opt.Trailing
	}
	return row
}

// Rows paints spec. Empty options yield an empty slice, not a placeholder.
// Open false paints a collapsed trigger (selected label, else first option).
// Filter, when non-empty, prepends a chrome line the gallery can show.
func Rows(spec Spec, markerOn, markerOff string, hint lipgloss.Style) []string {
	var out []string
	if spec.Filter != "" {
		out = append(out, hint.Render("/ "+screenkit.Sanitize(spec.Filter)))
	}
	if !spec.Open {
		if opt, ok := trigger(spec.Options); ok {
			out = append(out, Row(markerOff, opt, hint, spec.Join))
		}
		return out
	}
	for i, opt := range spec.Options {
		marker := markerOff
		if i == spec.Cursor {
			marker = markerOn
		}
		out = append(out, Row(marker, opt, hint, spec.Join))
	}
	return out
}

// trigger is the collapsed value: the first Selected option, else the
// first option. Empty options have nothing to collapse to.
func trigger(opts []Option) (Option, bool) {
	for _, opt := range opts {
		if opt.Selected {
			return opt, true
		}
	}
	if len(opts) == 0 {
		return Option{}, false
	}
	return opts[0], true
}
