package overlay

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/scrollwindow"
)

// Styles are the tones the overlay paints with. They mirror the root theme
// projection without importing it.
type Styles struct {
	Info      lipgloss.Style
	Hint      lipgloss.Style
	Key       lipgloss.Style
	Footer    lipgloss.Style
	Separator lipgloss.Style
}

// Binding is one key + description row inside a Group.
type Binding struct {
	Key, Desc string
}

// Group is one titled section of bindings.
type Group struct {
	Title    string
	Bindings []Binding
}

// Options configures Render. FormatScrollHint is called when the body is
// truncated; the host supplies it so the hint stays on the root's catalog
// path (viewportFooterHint).
type Options struct {
	Title, ScopeHint string
	Groups           []Group
	Scroll           int
	Viewport         int
	KeyColumn        int
	FormatScrollHint func(above, below int) string
}

const defaultKeyColumn = 34

// leadingBlankRows is the blank line Render puts above the indented body so
// the overlay sits one row below the header. Charged against the viewport
// budget alongside header and footer heights.
const leadingBlankRows = 1

// Render paints the help body. When Viewport > 0 and the body overflows, the
// last row of the viewport is reserved for FormatScrollHint.
func Render(styles Styles, opts Options) string {
	lines := Lines(styles, opts)
	viewport := opts.Viewport
	if viewport > 0 && len(lines) > viewport {
		visible, above, below := scrollwindow.SliceLines(lines, opts.Scroll, viewport-1)
		hint := ""
		if opts.FormatScrollHint != nil {
			hint = opts.FormatScrollHint(above, below)
		}
		body := strings.Join(visible, "\n")
		if hint != "" {
			body += "\n" + hint
		}
		return "\n" + screenkit.Indent(body, 2)
	}
	return "\n" + screenkit.Indent(strings.Join(lines, "\n"), 2)
}

// Lines builds the unwrapped help body (title, scope hint, groups) without
// the leading blank or scroll window. Exported so a gallery demo can count
// rows the same way Render does.
func Lines(styles Styles, opts Options) []string {
	keyW := opts.KeyColumn
	if keyW <= 0 {
		keyW = defaultKeyColumn
	}
	var lines []string
	lines = append(lines,
		screenkit.Kicker(styles.Info, opts.Title),
		styles.Hint.Render(opts.ScopeHint),
		"",
	)
	for _, g := range opts.Groups {
		lines = append(lines, screenkit.Kicker(styles.Info, g.Title))
		lines = append(lines, panel.HRule(styles.Separator, keyW+24))
		lines = append(lines, bindingRows(styles.Key, keyW, g.Bindings)...)
		lines = append(lines, "")
	}
	return lines
}

// bindingRows is one group's `key   description` rows.
//
// The key column is painted ONCE for the group, not once per binding: the keys
// go into a single block and come back out of one Render, which is where the
// group's box boundary actually is. The description and the column pad are
// concatenated afterwards, so a row carries exactly the bytes the per-binding
// paint produced.
func bindingRows(key lipgloss.Style, keyW int, bindings []Binding) []string {
	if len(bindings) == 0 {
		return nil
	}
	keys := make([]string, len(bindings))
	pads := make([]int, len(bindings))
	for i, b := range bindings {
		keys[i] = b.Key
		pads[i] = max(1, keyW-lipgloss.Width(b.Key))
	}
	painted := paintColumn(key, keys)
	rows := make([]string, len(bindings))
	for i, b := range bindings {
		rows[i] = painted[i] + strings.Repeat(" ", pads[i]) + b.Desc
	}
	return rows
}

// paintColumn paints every entry through style in ONE Render call.
//
// lipgloss resolves a style into terminal codes per Render call and then
// applies them line by line, so a column painted as one block carries exactly
// the codes N separate calls would have produced — at one resolution for the
// whole box instead of one per entry. The block form also right-pads every
// line out to the widest one, because lipgloss aligns any multi-line render;
// that pad is the one thing to undo, which is why entries here are labels and
// glyphs that never end in a space.
func paintColumn(style lipgloss.Style, entries []string) []string {
	if len(entries) == 0 {
		return nil
	}
	painted := strings.Split(style.Render(strings.Join(entries, "\n")), "\n")
	for i := range painted {
		painted[i] = strings.TrimRight(painted[i], " ")
	}
	return painted
}

// Footer paints the help footer's indented catalog line.
func Footer(styles Styles, text string) string {
	return screenkit.Indent(styles.Footer.Render(text), 2)
}

// FooterHeight is the rows one Footer call would occupy.
func FooterHeight(styles Styles, text string) int {
	return lipgloss.Height(Footer(styles, text))
}

// ViewportRows is the line budget for the help body given a measured header
// and footer. The leading blank Render emits is charged here so Stack's
// middle slot and the scroll window agree. Returns 0 on tiny terminals so
// callers fall back to painting everything (Stack then truncates).
func ViewportRows(termHeight, headerRows, footerRows int) int {
	if termHeight <= 0 {
		return 0
	}
	rows := termHeight - headerRows - footerRows - leadingBlankRows
	if rows < 8 {
		return 0
	}
	return rows
}
