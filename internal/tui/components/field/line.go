package field

import (
	"github.com/charmbracelet/bubbles/textinput"

	"omakiten/internal/tui/components/screenkit"
)

// Indent is the left margin the prompt sits at, matching the body around it.
const Indent = 2

// RenderLine draws `label: <input>` in the accent border.
//
// The input is taken BY VALUE and sized here. A prompt's label is only knowable
// at render time — task detail's comes from a host callback over the task being
// moved — so the persistent model cannot have been sized against it, and sizing
// a throwaway copy is what keeps the row inside the terminal. Mutating the
// caller's model instead would fight whatever sized it last.
//
// The border is always the accent: an input on screen is an input being typed
// into, so the neutral variant would be a focus cue that never fires.
func RenderLine(s screenkit.Styles, label string, input textinput.Model, width int) string {
	label = screenkit.Sanitize(label)
	input.SetValue(screenkit.Sanitize(input.Value()))
	input.Cursor.Style = s.Cursor
	input.Width = width
	style := s.Input.BorderForeground(s.HintAccent.GetForeground())
	return screenkit.Indent(style.Render(label+": "+input.View()), Indent)
}

// Fit resolves one prompt against a width budget, in a stated order of
// sacrifice, and returns what to hand [RenderLine].
//
// The floor documented on [Width] is right about WHAT must survive — an input
// too narrow to type in is useless — and was wrong about what pays for it. Below
// the floor the whole row overflowed, label included, so the first thing to
// leave the terminal was the one part that could have been shortened instead.
// The order, from most disposable to least:
//
//  1. the LABEL truncates, down to a single ellipsis
//  2. the INPUT shrinks, down to floor
//  3. only then does the row overflow, which now takes something genuinely
//     incompressible to reach
//
// Callers that already know their label fits can keep calling [Width] directly;
// this is the path for a prompt whose label comes from data, which is every
// prompt the app actually has.
func Fit(s screenkit.Styles, label string, available, floor int) (string, int) {
	if room := available - labelCells(label) - chrome(s); room >= floor {
		return label, room
	}
	// The label pays first. What it may spend is whatever is left once the box,
	// the separator and the floored input have taken theirs.
	budget := available - chrome(s) - floor - len(labelSep)
	if budget < 1 {
		return screenkit.Truncate(label, 1), floor
	}
	return screenkit.Truncate(label, budget), floor
}

// labelSep is what RenderLine writes between the label and the input.
const labelSep = ": "

// labelCells is the columns a label costs including its separator.
func labelCells(label string) int { return screenkit.VisibleWidth(label) + len(labelSep) }

// Width is the room left for typing once the label and the box have been paid
// for, floored so a terminal too narrow to hold both still leaves something to
// type in.
//
// Below the floor the row is allowed to be the one thing that overflows: a
// zero-width input cannot be typed into, which is worse than a line that runs
// past the edge.
//
// The chrome is MEASURED off the style rather than declared. It was a constant
// 4 first, and the row overran by three columns at every geometry, because the
// theme's input pads two columns a side rather than one and bubbles reserves a
// cell past the value for the cursor. A hardcoded frame size is a number that
// silently stops matching the moment a theme changes its padding.
func Width(s screenkit.Styles, available, labelWidth, floor int) int {
	if room := available - labelWidth - chrome(s); room > floor {
		return room
	}
	return floor
}

// chrome is everything the row spends that is not the label or the value: the
// indent, the style's border and padding, and the cell bubbles keeps past the
// end of the value so the cursor has somewhere to sit.
func chrome(s screenkit.Styles) int {
	return Indent + s.Input.GetHorizontalFrameSize() + cursorCell
}

const cursorCell = 1
