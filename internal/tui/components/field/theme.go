package field

import (
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/screenkit"
)

// FormTheme is everything a STACK of prompts paints with: the label above each
// row, the box the value sits in, the accent the focused row swaps its border
// for, the hint under the last field, and the multiline Theme the description
// area needs.
//
// It lives here, beside the Theme one Area needs, because it is the same kind
// of value one size up — presentation props for a field, presentation props for
// a form of them. It did not: taskform declared six lipgloss.Style fields of
// its own, which made the screen the author of a style vocabulary rather than a
// consumer of one, and left its fixture free to invent a palette out of hex
// literals that no theme could reach.
type FormTheme struct {
	// Label and LabelActive paint the section label above a row — muted, and
	// in the focus accent for the row that owns the keyboard.
	Label       lipgloss.Style
	LabelActive lipgloss.Style

	// Input is the bordered chrome around a single-line value. Width set on it
	// is ignored: RenderLine and the form both override it from the live row
	// budget.
	Input lipgloss.Style

	// ActiveBorder is the BorderForeground the focused row's Input swaps to.
	// Every other property of Input carries over, so focus stays a
	// single-property delta — the same contract Theme.BorderActive states.
	ActiveBorder lipgloss.TerminalColor

	// Hint is the muted tone for the copy under the form: the parent-lookup
	// message, the inactive priority options.
	Hint lipgloss.Style

	// Cursor is assigned to each single-line input's Cursor.Style so the
	// reverse-video caret renders with an explicit foreground.
	Cursor lipgloss.Style

	// Multiline is the chrome the description area draws for itself.
	Multiline Theme
}

// Form projects the screen styles onto the form vocabulary.
//
// This is the ONE derivation: the root host and every fixture call it, so a
// form recorded in a golden and a form on a user's terminal are the same box
// resolved against two palettes rather than two boxes that happen to agree.
func Form(s screenkit.Styles) FormTheme {
	return FormTheme{
		Label:        s.Hint,
		LabelActive:  s.HintAccent,
		Input:        s.Input,
		ActiveBorder: s.HintAccent.GetForeground(),
		Hint:         s.Hint,
		Cursor:       s.Cursor,
		Multiline:    Multiline(s),
	}
}

// Multiline projects the screen styles onto the chrome RenderArea draws.
//
// One canonical theme drives the task description, the inline comment-add modal
// and the comment-edit overlay, so the three surfaces render identical chrome —
// the prior split between a neutral-bordered form input and an always-accent
// comment input caused visual drift between forms and was the trigger for the
// unification.
func Multiline(s screenkit.Styles) Theme {
	return Theme{
		Border:       s.FormMultiline,
		BorderActive: s.HintAccent.GetForeground(),
		Cursor:       s.Cursor,
	}
}
