package screenkit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Sanitize strips terminal control sequences from untrusted text so a
// payload cannot repaint, reposition or reprogram the terminal from inside a
// rendered cell.
//
// The scanner handles both 7-bit ESC and C1 sequence introducers. This matters
// even when the input contains UTF-8 encoded C1 runes: a terminal decodes those
// bytes before interpreting them, while a byte-only filter would leave the
// sequence payload behind.
func Sanitize(s string) string {
	if !hasControl(s) {
		return s
	}
	return stripTerminalSequences(s, false)
}

// SanitizeMultiline strips terminal controls while preserving LF separators.
// It is intended for untrusted prose such as Markdown previews where flattening
// the content would destroy useful structure.
func SanitizeMultiline(s string) string {
	if !hasControlExceptLF(s) {
		return s
	}
	return stripTerminalSequences(s, true)
}

func stripTerminalSequences(s string, preserveLF bool) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		// C1 controls are normally single-byte terminal input. Handle them
		// before UTF-8 decoding too, because hostile byte strings need not be
		// valid UTF-8 to carry a terminal sequence.
		if next, handled := skipRawC1(s, i, preserveLF, &out); handled {
			i = next
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if next, handled := skipRuneControl(s, i, r, size, preserveLF, &out); handled {
			i = next
			continue
		}
		if r == '\n' && preserveLF {
			out.WriteByte('\n')
		} else if size == 1 && r == utf8.RuneError {
			// Drop malformed bytes rather than turning them into visible
			// replacement glyphs in a terminal-bound string.
		} else if !unicode.IsControl(r) && r != '\u007f' {
			out.WriteString(s[i : i+size])
		}
		i += size
	}
	return out.String()
}

func skipRawC1(s string, i int, preserveLF bool, out *strings.Builder) (int, bool) {
	if s[i] < 0x80 || s[i] > 0x9f {
		return i, false
	}
	switch s[i] {
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
		return skipStringControl(s, i+1, preserveLF, out), true
	case 0x9b:
		return skipCSI(s, i+1, preserveLF, out), true
	default:
		return i + 1, true
	}
}

func skipRuneControl(s string, i int, r rune, size int, preserveLF bool, out *strings.Builder) (int, bool) {
	switch r {
	case '\x1b':
		return skipEscape(s, i+size, preserveLF, out), true
	case '\u0090', '\u0098', '\u009d', '\u009e', '\u009f':
		return skipStringControl(s, i+size, preserveLF, out), true
	case '\u009b':
		return skipCSI(s, i+size, preserveLF, out), true
	case '\u009c':
		return i + size, true
	default:
		return i, false
	}
}

func skipEscape(s string, i int, preserveLF bool, out *strings.Builder) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		return skipCSI(s, i+1, preserveLF, out)
	case ']', 'P', '^', '_':
		return skipStringControl(s, i+1, preserveLF, out)
	}

	// ESC sequences have zero or more ASCII intermediates (0x20-0x2f) and
	// one ASCII final (0x30-0x7e). Do not consume a Unicode rune or a newline
	// merely because it followed a malformed/bare ESC.
	j := i
	for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
		j++
	}
	if j > i {
		if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
			return j + 1
		}
		return j
	}
	if s[i] >= 0x30 && s[i] <= 0x7e {
		return i + 1
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if r == '\n' && preserveLF {
		out.WriteByte('\n')
		return i + size
	}
	if r == utf8.RuneError && size == 1 {
		return i + size
	}
	// Leave non-ASCII text for the main scanner, which will preserve valid
	// Unicode and remove malformed bytes according to the normal rules.
	return i
}

func skipCSI(s string, i int, preserveLF bool, out *strings.Builder) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r >= 0x40 && r <= 0x7e {
			return i + size
		}
		if r == '\n' && preserveLF {
			out.WriteByte('\n')
		}
		i += size
	}
	return i
}

func skipStringControl(s string, i int, preserveLF bool, out *strings.Builder) int {
	for i < len(s) {
		if next, terminated := stringControlTerminator(s, i); terminated {
			return next
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' && preserveLF {
			out.WriteByte('\n')
		}
		i += size
	}
	return i
}

func stringControlTerminator(s string, i int) (int, bool) {
	if s[i] == 0x9c {
		return i + 1, true
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	switch r {
	case '\a', '\u009c':
		return i + size, true
	case '\x1b':
		next := i + size
		if next < len(s) && s[next] == '\\' {
			return next + 1, true
		}
	}
	return i, false
}

func hasControl(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u007f' || r >= '\u0080' && r <= '\u009f' {
			return true
		}
	}
	return false
}

func hasControlExceptLF(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r != '\n' && (unicode.IsControl(r) || r == '\u007f' || r >= '\u0080' && r <= '\u009f') {
			return true
		}
	}
	return false
}

// Truncate caps s to a max VISIBLE cell budget, not a rune count.
//
// PLAIN TEXT ONLY. The rune walk charges every rune ansi.StringWidth, which
// reports 1 for the printable ASCII inside an escape sequence — so a styled
// string is billed for its escapes as though they were content, and the cut can
// land inside a sequence and leave the style open. Every caller here passes
// prose that has been through Sanitize or arrives unstyled. A rendered row that
// carries styling needs ansi.Truncate instead; see TruncateStyled.
// A CJK ideograph or emoji occupies two terminal cells, so a rune-count cut
// would let a string with wide glyphs render at up to twice its budget and tip
// past the panel edge. Width is measured with ansi.StringWidth (display cells)
// and the cut walks runes while tracking accumulated cell width, reserving one
// cell for the trailing ellipsis so the result never exceeds max cells.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= max {
		return s
	}
	budget := max - 1
	width := 0
	var b strings.Builder
	for _, r := range s {
		w := ansi.StringWidth(string(r))
		if width+w > budget {
			break
		}
		b.WriteRune(r)
		width += w
	}
	return b.String() + "…"
}

// WrapWords breaks s into lines where the first line is constrained to
// firstWidth and every line after it to restWidth.
//
// The two widths exist because a card hangs its title under an `#id ` prefix:
// the first line pays for the prefix, the rest pay for the indent that lines up
// under it. Passing the same number twice is the plain case.
//
// It keeps whole words, but a single word wider than the active limit is
// HARD-WRAPPED by visible cell width, so an unbroken token — a long URL, a path
// with no spaces, a run of CJK — can never produce a line that overflows the
// column. That is the half a screen used to be able to skip: three copies of
// this function existed and one of them packed words without the hard wrap, so
// a sub-task titled with one long token painted straight through its card edge.
//
// Widths are measured in display cells so a wide glyph is split at the correct
// boundary. Never returns an empty slice: an empty input is one empty line, so
// a caller can index the first line without checking.
func WrapWords(s string, firstWidth, restWidth int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := ""
	for _, word := range words {
		lines, current = appendWord(lines, current, word, firstWidth, restWidth)
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func appendWord(lines []string, current, word string, firstWidth, restWidth int) ([]string, string) {
	limit := firstWidth
	if len(lines) > 0 {
		limit = restWidth
	}
	flush := func() { lines = append(lines, current); current = "" }
	if lipgloss.Width(word) > limit {
		if current != "" {
			flush()
		}
		frags := hardWrapToken(word, limit)
		current = frags[0]
		flush()
		if rest := strings.Join(frags[1:], ""); rest != "" {
			for _, frag := range hardWrapToken(rest, restWidth) {
				current = frag
				flush()
			}
		}
		return lines, current
	}
	if current == "" {
		return lines, word
	}
	if lipgloss.Width(current+" "+word) <= limit {
		return lines, current + " " + word
	}
	flush()
	return lines, word
}

// hardWrapToken splits one unbroken token into fragments at most width visible
// cells wide, cutting on cell boundaries so a wide glyph is never split in half.
func hardWrapToken(token string, width int) []string {
	if width < 1 {
		width = 1
	}
	if lipgloss.Width(token) <= width {
		return []string{token}
	}
	var frags []string
	var b strings.Builder
	cur := 0
	for _, r := range token {
		rw := lipgloss.Width(string(r))
		if cur+rw > width && cur > 0 {
			frags = append(frags, b.String())
			b.Reset()
			cur = 0
		}
		b.WriteRune(r)
		cur += rw
	}
	if b.Len() > 0 {
		frags = append(frags, b.String())
	}
	return frags
}

// TruncatePath shortens a filesystem path from the LEFT, keeping the tail.
//
// Truncate would keep the head, which is the wrong half: `/home/howl/Proj…` says
// nothing a user can act on, while `…/person/omakiten` identifies the project.
// The cut lands on a separator wherever one fits, so the result still reads as a
// path rather than as a string that was chopped.
//
// Falls back to cutting inside the final segment when even that segment does not
// fit, because a name half-shown beats no name at all.
func TruncatePath(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return "…"
	}
	parts := strings.Split(value, "/")
	tail := parts[len(parts)-1]
	if lipgloss.Width(tail)+2 > width {
		return "…" + tailByWidth(tail, width-1)
	}
	for i := len(parts) - 2; i >= 0; i-- {
		if candidate := "…/" + strings.Join(parts[i:], "/"); lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return "…/" + tail
}

// tailByWidth returns the last `width` visible cells of value, cutting on a cell
// boundary so a wide glyph is never split.
func tailByWidth(value string, width int) string {
	runes := []rune(value)
	used, start := 0, len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		cell := lipgloss.Width(string(runes[i]))
		if used+cell > width {
			break
		}
		used += cell
		start = i
	}
	return string(runes[start:])
}

// VisibleWidth is the terminal columns a rendered block occupies — its widest
// line, counting what the terminal draws rather than what the string stores.
//
// It exists so nothing above this package has to reach for lipgloss to ask a
// question about a string. A screen that imports lipgloss to measure is one
// import away from importing it to STYLE, and the whole point of the component
// boundary is that appearance decisions live in one layer. The measurement is
// the same one PadRight and the arranger already make; naming it here means the
// callers can be held to a rule instead of a habit.
//
// Styled input is the reason this is not len(): ANSI escape bytes are stored
// and not drawn, so a byte count over-reports a coloured cell by the length of
// its escape sequence and every column built on it drifts right.
func VisibleWidth(block string) int { return lipgloss.Width(block) }

// BlockRows is the terminal rows a rendered block occupies.
//
// The counterpart of VisibleWidth, and the same argument: a row budget is the
// arranger's to hand out, but MEASURING what a section produced is an ordinary
// question about a string and should not require the styling library.
//
// Note the boundary this does not cross: this reports what a block COSTS, never
// what it was ALLOWED. The allowance is [screenlayout.Canvas], and a caller that
// derives one from the other is writing the private-copy defect the arranger
// exists to make inexpressible.
func BlockRows(block string) int { return lipgloss.Height(block) }

// Clamp bounds value into [minValue, maxValue]. An inverted range collapses to
// minValue so callers never receive a value from outside the intended floor.
func Clamp(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// Indent prefixes every non-empty line of block with `spaces` blanks. Empty
// lines stay empty so the indent never introduces trailing whitespace.
func Indent(block string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// PadRight right-pads s with spaces so its visible width is at least width.
// Unlike `fmt.Sprintf("%-*s", w, s)`, ANSI-styled inputs would double-count the
// escape bytes — PadRight measures visible width with lipgloss so styled values
// still align across rows.
func PadRight(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// PageStep is the canonical half-page scroll step for a viewport of the given
// row budget, floored at 4 rows so pgup/pgdn still move on tiny terminals.
func PageStep(viewport int) int {
	step := viewport / 2
	if step < 4 {
		return 4
	}
	return step
}
