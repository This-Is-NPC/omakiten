// Package header owns the root TUI chrome above every screen body: the
// breadcrumb, the zone/sub navigation strip, the Home title, the modal
// input bar and the keybinding footer.
//
// It exists because those surfaces lived as methods on the root Model and
// could not be inspected without booting the whole app. The four header
// states — Home, Overlay, Compact fallback, Full strip — share one Render
// so a terminal that forces the compact form cannot paint a different
// shape from the one Width measured. Stack spends a terminal height
// budget across named slots instead of joining everything and silently
// chopping the tail.
//
// The package is a leaf: it imports field, tokenstrip and lipgloss only.
package header

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
)

// Styles are the four tones the header paints with. They mirror the root
// theme projection (title / nav / activeNav / hint) without importing it.
type Styles struct {
	Title     lipgloss.Style
	Nav       lipgloss.Style
	ActiveNav lipgloss.Style
	Hint      lipgloss.Style
}

// Item is one labelled entry on a nav strip (a top zone or a sub).
type Item struct {
	Label  string
	Active bool
}

// Options configures Render. Home and Overlay select the first two states;
// when neither is set, Render compares the full strip against Width and
// picks Compact or Full.
type Options struct {
	Width int
	// Brand is the leading breadcrumb token (always "omakiten" today).
	Brand string
	// Segment is the second breadcrumb token: "home" or the project slug.
	Segment string
	// SegmentHint trails the segment after a middot ("select a project" /
	// "local checkpoint").
	SegmentHint string

	Home    bool
	Overlay bool

	// HomeLabel is the "00 // HOME" tile folded into the top strip.
	HomeLabel string
	Tops      []Item
	Subs      []Item
	// CompactHint is the fallback copy when the full strip overflows Width.
	CompactHint string
	// HomeReturnHint trails the Home kicker ("ctrl+h returns here…").
	HomeReturnHint string
}

const navGap = "   "

// Render paints the header in one of four states.
//
//  1. Home — breadcrumb with the select-a-project hint, then HomeTitle.
//  2. Overlay — breadcrumb only; nav strip suppressed so an overlay owns focus.
//  3. Compact — active top only + CompactHint, when the full strip overflows.
//  4. Full — HOME │ tops with underline rules, plus the sub strip when present.
func Render(styles Styles, opts Options) string {
	opts = sanitizeOptions(opts)
	var sb strings.Builder
	sb.WriteString("\n  ")
	sb.WriteString(breadcrumb(styles, opts))

	if opts.Home {
		sb.WriteString("\n\n  ")
		// The title is written AT the indent, so the indent is not its to spend.
		// Handing it the full width padded the rule row two columns past the
		// terminal at every width the cap did not already clamp.
		sb.WriteString(HomeTitle(styles, opts.Width-indent, opts.HomeLabel, opts.HomeReturnHint))
		return sb.String()
	}
	if opts.Overlay {
		return sb.String()
	}

	sb.WriteString("\n\n  ")
	if stripWidth(styles, opts) > opts.Width {
		sb.WriteString(compactStrip(styles, opts))
		return sb.String()
	}

	items, rules := topStrip(styles, opts)
	sb.WriteString(items)
	sb.WriteString("\n  ")
	sb.WriteString(rules)
	if len(opts.Subs) > 1 {
		subItems, subRules := navStrip(styles, opts.Subs)
		sb.WriteString("\n  ")
		sb.WriteString(subItems)
		sb.WriteString("\n  ")
		sb.WriteString(subRules)
	}
	return sb.String()
}

func sanitizeOptions(opts Options) Options {
	if opts.Tops != nil {
		opts.Tops = append([]Item(nil), opts.Tops...)
	}
	if opts.Subs != nil {
		opts.Subs = append([]Item(nil), opts.Subs...)
	}
	opts.Brand = screenkit.Sanitize(opts.Brand)
	opts.Segment = screenkit.Sanitize(opts.Segment)
	opts.SegmentHint = screenkit.Sanitize(opts.SegmentHint)
	opts.HomeLabel = screenkit.Sanitize(opts.HomeLabel)
	opts.CompactHint = screenkit.Sanitize(opts.CompactHint)
	opts.HomeReturnHint = screenkit.Sanitize(opts.HomeReturnHint)
	for i := range opts.Tops {
		opts.Tops[i].Label = screenkit.Sanitize(opts.Tops[i].Label)
	}
	for i := range opts.Subs {
		opts.Subs[i].Label = screenkit.Sanitize(opts.Subs[i].Label)
	}
	return opts
}

// indent is the two columns every header row is written at, and the only part
// of the budget no row may spend.
const indent = 2

// breadcrumb is the first row: brand, segment, and the optional segment hint.
//
// It spends opts.Width instead of assuming it, with a stated order of loss —
// the hint goes first because it is advisory, then the segment truncates
// because it is data, and the brand never moves because it is how the reader
// knows which app they are looking at.
//
// It used to spend nothing: the row was concatenated and returned, so a long
// segment ran off the terminal. The compact fallback below did not save it —
// that one only replaces the NAV strip, and by then this row is already
// written.
func breadcrumb(styles Styles, opts Options) string {
	budget := opts.Width - indent
	brand := styles.Title.Render(opts.Brand) + styles.Hint.Render(" › ")
	spent := screenkit.VisibleWidth(opts.Brand) + 3

	// Last resort: a terminal too narrow for the brand and its separator. The
	// brand is the last thing to give, not an exception to the budget — a row
	// that cannot hold it paints what it can rather than running past the edge.
	if budget > 0 && spent > budget {
		return styles.Title.Render(screenkit.Truncate(opts.Brand, budget))
	}

	if hint := opts.SegmentHint; hint != "" {
		full := spent + screenkit.VisibleWidth(opts.Segment) + 3 + screenkit.VisibleWidth(hint)
		if budget <= 0 || full <= budget {
			return brand + styles.Nav.Render(opts.Segment) + styles.Hint.Render(" · "+hint)
		}
	}
	if budget <= 0 {
		return brand + styles.Nav.Render(opts.Segment)
	}
	return brand + styles.Nav.Render(screenkit.Truncate(opts.Segment, maxInt(budget-spent, 1)))
}

// compactStrip is the fallback nav row: the active zone plus the hint that says
// how to reach the others.
//
// The fallback existed to stop the FULL strip overflowing and then overflowed
// itself, because nothing measured what it wrote. Same order of loss as the
// breadcrumb: the hint is advisory and gives way first, and the active zone —
// the one thing that tells the reader where they are — truncates last.
func compactStrip(styles Styles, opts Options) string {
	active := activeTopLabel(opts)
	budget := opts.Width - indent
	if budget <= 0 {
		return styles.ActiveNav.Render(active)
	}
	if screenkit.VisibleWidth(active)+screenkit.VisibleWidth(opts.CompactHint) <= budget {
		return styles.ActiveNav.Render(active) + styles.Hint.Render(opts.CompactHint)
	}
	if room := budget - screenkit.VisibleWidth(active); room > 0 {
		return styles.ActiveNav.Render(active) + styles.Hint.Render(screenkit.Truncate(opts.CompactHint, room))
	}
	return styles.ActiveNav.Render(screenkit.Truncate(active, budget))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Width is the columns one Render call would occupy at opts.Width — the
// widest painted line. It runs the same state selection Render does.
func Width(styles Styles, opts Options) int {
	out := Render(styles, opts)
	widest := 0
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
	}
	return widest
}

// Height is the rows one Render call would occupy. Same constructor as Render.
func Height(styles Styles, opts Options) int {
	return lipgloss.Height(Render(styles, opts))
}

// HomeTitle paints the Home kicker, return hint and underline rule. Width is
// capped at 78 so a very wide terminal does not stretch the rule across the
// whole frame.
func HomeTitle(styles Styles, width int, homeLabel, returnHint string) string {
	if width > 78 {
		width = 78
	}
	// The kicker row is `label + hint` and nothing measured it, so on a narrow
	// terminal the Home state painted a 33-column row inside a 12-column frame.
	// Same order of loss as everywhere else in this package: the hint is
	// advisory and gives way first, the label that names the surface goes last.
	label, hint := homeLabel, returnHint
	if width > 0 {
		if screenkit.VisibleWidth(label)+screenkit.VisibleWidth(hint) > width {
			hint = screenkit.Truncate(hint, maxInt(width-screenkit.VisibleWidth(label), 0))
		}
		if screenkit.VisibleWidth(label) > width {
			label, hint = screenkit.Truncate(label, width), ""
		}
	}
	kicker := styles.ActiveNav.Render(label)
	if hint != "" {
		kicker += styles.Hint.Render(hint)
	}
	rule := styles.ActiveNav.Render(strings.Repeat("─", lipgloss.Width(label)))
	pad := width - lipgloss.Width(rule)
	if pad < 0 {
		pad = 0
	}
	return kicker + "\n  " + rule + strings.Repeat(" ", pad)
}

// Input draws the modal single-line prompt. Delegates to field.RenderLine so the
// root chrome and task-detail's move bar cannot drift.
func Input(s screenkit.Styles, label string, input textinput.Model, width int) string {
	return field.RenderLine(s, label, input, width)
}

// Footer paints the keybinding row with the leading blank + two-column indent
// the rest of the chrome uses.
func Footer(tokens []tokenstrip.Key, styles tokenstrip.KeyStyles) string {
	return "\n" + screenkit.Indent(tokenstrip.Keys(tokens, styles), 2)
}

func stripWidth(styles Styles, opts Options) int {
	items, _ := topStrip(styles, opts)
	return lipgloss.Width(items)
}

func topStrip(styles Styles, opts Options) (items, rules string) {
	homeItem := styles.Nav.Render(opts.HomeLabel)
	homeRule := strings.Repeat(" ", lipgloss.Width(opts.HomeLabel))
	divider := styles.Hint.Render("│")

	topItems, topRules := navStrip(styles, opts.Tops)
	items = homeItem + navGap + divider + navGap + topItems
	rules = homeRule + navGap + " " + navGap + topRules
	return items, rules
}

// navStrip paints one nav strip — the labels row and the rule row that
// underlines the active entries. The top and the sub strip are the same shape
// and now the same code; they used to be two copies of the loop below.
//
// A strip is three tone columns, not n painted entries: the active labels, the
// plain labels, and the underline rules of the active ones. Each column is
// gathered and painted once — see paintColumn — instead of the two Render calls
// per entry the loop used to make. An inactive entry's rule is blank, so it
// costs no paint at all, which is why there is no fourth column.
func navStrip(styles Styles, entries []Item) (items, rules string) {
	if len(entries) == 0 {
		return "", ""
	}
	active := make([]string, 0, len(entries))
	plain := make([]string, 0, len(entries))
	underlines := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.Active {
			plain = append(plain, e.Label)
			continue
		}
		active = append(active, e.Label)
		underlines = append(underlines, strings.Repeat("─", lipgloss.Width(e.Label)))
	}
	active = paintColumn(styles.ActiveNav, active)
	plain = paintColumn(styles.Nav, plain)
	underlines = paintColumn(styles.ActiveNav, underlines)

	var itemRow, ruleRow strings.Builder
	next, other := 0, 0
	for i, e := range entries {
		if i > 0 {
			itemRow.WriteString(navGap)
			ruleRow.WriteString(navGap)
		}
		if e.Active {
			itemRow.WriteString(active[next])
			ruleRow.WriteString(underlines[next])
			next++
			continue
		}
		itemRow.WriteString(plain[other])
		ruleRow.WriteString(strings.Repeat(" ", lipgloss.Width(e.Label)))
		other++
	}
	return itemRow.String(), ruleRow.String()
}

// paintColumn paints every entry through style in ONE Render call, in place.
//
// lipgloss resolves a style into terminal codes per Render call and then
// applies them line by line, so a column painted as one block carries exactly
// the codes N separate calls would have produced — at one resolution for the
// whole strip instead of one per entry. The block form also right-pads every
// line out to the widest one, because lipgloss aligns any multi-line render;
// that pad is the one thing to undo, which is why entries here are nav labels
// and rules that never end in a space.
func paintColumn(style lipgloss.Style, entries []string) []string {
	switch len(entries) {
	case 0:
		return nil
	case 1:
		entries[0] = style.Render(entries[0])
		return entries
	}
	block := style.Render(strings.Join(entries, "\n"))
	for i := range entries {
		line := block
		if cut := strings.IndexByte(block, '\n'); cut >= 0 {
			line, block = block[:cut], block[cut+1:]
		}
		entries[i] = strings.TrimRight(line, " ")
	}
	return entries
}

func activeTopLabel(opts Options) string {
	for _, t := range opts.Tops {
		if t.Active {
			return t.Label
		}
	}
	if len(opts.Tops) > 0 {
		return opts.Tops[0].Label
	}
	return opts.HomeLabel
}
