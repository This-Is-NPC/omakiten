package main

import (
	"fmt"
	"strings"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/tokenstrip"
)

// demoCtx is the geometry and theme a demo renders against. Width and Height are
// the SIMULATED FRAME's inner size — not the terminal's — which is what makes the
// frame controls an honest simulation: these components take their geometry as an
// argument, so handing them smaller numbers is what a smaller terminal does.
type demoCtx struct {
	kit   screenkit.Kit
	theme config.Theme
}

// demoTheme is the bridge between the gallery and the components: it holds the
// components' own theme projection and hands out a demoCtx at a geometry.
//
// It lives here, on the subject side, so the chrome never has to name a
// components/ type — see TestTheChromeDoesNotImportWhatItInspects.
type demoTheme struct {
	styles screenkit.Styles
	theme  config.Theme
}

func newDemoTheme(theme config.Theme, styles screenkit.Styles) demoTheme {
	return demoTheme{styles: styles, theme: theme}
}

func (t demoTheme) ctx(width, height int) demoCtx {
	return demoCtx{
		kit: screenkit.Kit{
			Styles: t.styles, Width: width, Height: height,
			Markdown: screenkit.MarkdownTokens{
				ThemeKey: t.theme.Key, Foreground: t.theme.Colors["foreground"],
				Border: t.theme.Colors["border"], Primary: t.theme.Colors["primary"],
				Secondary: t.theme.Colors["secondary"],
			},
		},
		theme: t.theme,
	}
}

// entry is one inspectable thing: what it is, what it is for, how to build it,
// and the scenarios worth putting it in.
type entry struct {
	name string
	// minWidth / minHeight are the declared floor for this entry. A scenario that
	// omits an axis paints at the entry minimum on that axis. Zero is incomplete:
	// the overflow gate refuses an allowlist, so an undeclared floor is a broken
	// entry rather than named debt.
	minWidth, minHeight int
	// pkg is the package the subject lives in. For a component under
	// internal/tui/components it is what the completeness gate matches on, so a
	// typo here reads as a missing entry rather than passing quietly.
	pkg   string
	title string
	desc  string
	// screen is the screenhost id this entry inspects, empty for a component.
	// It is the other half of the completeness gate: every id in the contract
	// needs an entry that claims it.
	screen    string
	new       func(demoCtx) demo
	scenarios []scenario
}

// subject is the line under the title: what this entry is inspecting. A screen
// entry says which surface as well as which package, because "project" the
// package serves two of them and the reader has to know which one is on screen.
func (e entry) subject() string {
	if e.screen == "" {
		return e.pkg
	}
	return e.pkg + "  ·  " + e.screen
}

func entries() []entry {
	return append(componentEntries(), screenEntries()...)
}

func componentEntries() []entry {
	return []entry{
		{
			name:     "shell",
			minWidth: 80, minHeight: 24,
			pkg:   "internal/tui/components/screengrid",
			title: "The grid every screen body is built out of",
			desc: "The base layer, with COLOURED BLOCKS where the components go. Every other entry answers what one widget looks like; this one answers where the widgets end up when the terminal changes — which is what a screen is actually a decision about. " +
				"The scenarios are the app's screens, each at three terminals: SMALL is the 80x24 floor, MEDIUM the 120x40 default, LARGE a 200x50 desktop. They are the same three geometries every screen golden is recorded at, so what you approve here and what the fit gate checks are the same sizes. " +
				"Cells nest: a cell may be another grid, so `rows of cols` gives bands that align across columns and `cols of rows` gives task detail's `[a over b] | c`. Each level is resolved by the arranger, so the breakpoint, the row split and the drop-from-the-bottom work the same at depth three as at depth one. " +
				"A row of columns that stops fitting either STACKS or SLIDES — the second is the board's lane carousel. " +
				"Blocks are solid on purpose: a cell paints every row it was given at the full width it was given, so a row the layout failed to spend is a hole and a row it overspent is pushed past the frame. Nothing to count, no legend to read.",
			new:       newShellDemo,
			scenarios: shellScenarios(),
		},
		{
			name:     "card",
			minWidth: 20, minHeight: 16,
			pkg:   "internal/tui/components/card",
			title: "The bordered box that holds one selectable thing",
			desc: "The kanban Spec is the canonical one, and four surfaces had written it: the board's card, the SAME card reimplemented inside task detail's sub-task lanes, the project card on home and the entity card in settings. " +
				"Kind picks the body: kanban (Render), comment, or system. Comment and System are extra types on the same Painter — a folded authored body and a one-line recorded event, not a Spec with a different shape flag. " +
				"A long comment FOLDS rather than growing the card: the hint is appended after the visible rows. Set the limit to -1 and it never folds — that is the comment-detail screen. " +
				"The cursor chevron is separate from selection because two surfaces disagree ON PURPOSE. Height runs the same line builder Render does.",
			new: newCardDemo,
			scenarios: []scenario{
				{name: "board lane", note: "the card at a lane's width, cursor on it", width: 40,
					props: map[string]string{"box width": "33", "state": "selected", "cursor": "yes"}},
				{name: "idle neighbour", note: "same card without the cursor — two more columns for the title", width: 40,
					props: map[string]string{"box width": "33", "state": "idle", "cursor": "no"}},
				{name: "archived", note: "dimmed and struck through under the A toggle", width: 40,
					props: map[string]string{"box width": "33", "state": "archived", "cursor": "no"}},
				{name: "accent ring", note: "the plan network's critical-path hint, which must not read as the cursor", width: 40,
					props: map[string]string{"box width": "33", "state": "accent", "cursor": "no"}},
				{name: "plan card", note: "metadata rows between the title and the badges", width: 50,
					props: map[string]string{"box width": "44", "meta": "2", "badges": "2"}},
				{name: "project card", note: "no id prefix, no chevron — home marks selection with the border", width: 50,
					props: map[string]string{"box width": "44", "id": "0", "cursor": "no", "state": "selected",
						"title": "omakiten", "meta": "1", "badges": "2"}},
				{name: "overflow · unbreakable title", note: "the defect: one copy of the wrapper let this through the edge", width: 34,
					props: map[string]string{"box width": "26", "badges": "0",
						"title": "/home/howl/Projects/person/omakiten/internal/tui/components/card"}},
				{name: "narrow", note: "prefix, title and badges into almost nothing", width: 20,
					props: map[string]string{"box width": "14"}},
				{name: "empty", note: "nothing but the frame — no id, no title, no meta, no badges. The box still owes its border and its rows", width: 40, height: 16,
					props: map[string]string{"box width": "33", "id": "0", "title": "", "meta": "0", "badges": "0"}},
				{name: "short comment", note: "fits whole, nothing folded", width: 54,
					props: map[string]string{"kind": "comment", "width": "50", "body lines": "2"}},
				{name: "overflow · folded", note: "past the default limit, the hint says how much is hidden", width: 54,
					props: map[string]string{"kind": "comment", "width": "50", "body lines": "20"}},
				{name: "never folds", note: "the comment-detail case — the whole body", width: 54, height: 28,
					props: map[string]string{"kind": "comment", "width": "50", "body lines": "12", "line limit": "-1", "tags": "0"}},
				{name: "empty comment", note: "says so instead of leaving a gap", width: 54,
					props: map[string]string{"kind": "comment", "width": "50", "body lines": "0", "tags": "0"}},
				{name: "no timestamp", note: "the middot goes with it", width: 54,
					props: map[string]string{"kind": "comment", "width": "50", "timestamp": "", "body lines": "1", "tags": "0"}},
				{name: "system event", note: "the other border, one line, no tags", width: 54,
					props: map[string]string{"kind": "system", "width": "50", "author": "task moved backlog → dev"}},
				{name: "narrow comment", note: "everything wraps and the card still fits", width: 24,
					props: map[string]string{"kind": "comment", "width": "20", "body lines": "6", "tags": "3"}},
			},
		},
		{
			name:     "lane",
			minWidth: 30, minHeight: 10,
			pkg:   "internal/tui/components/lane",
			title: "One kanban column: kicker, rule, stacked cards",
			desc: "The board's lane and task-detail's sub-task column: a bordered box, a `// BUCKET · N` kicker, a rule, then list.Cards or the centred empty line. " +
				"Screens still paint each card (card.Painter + pills) and style the header; this package only joins them. " +
				"Empty uses Styles.Empty, never Hint. Overflow is Cards windowing inside a box that holds its height. Narrow is the board's ~30-column floor, where pills wrap and grow the card.",
			new: newLaneDemo,
			scenarios: []scenario{
				{name: "empty", note: "header + rule + centred empty line", width: 32, height: 10,
					props: map[string]string{"items": "0"}},
				{name: "focused", note: "accent kicker, cards with pills, cursor on one", width: 36, height: 24,
					props: map[string]string{"focused": "yes", "items": "5"}},
				{name: "idle neighbour", note: "muted header, no chevron — the adjacent lane", width: 36, height: 24,
					props: map[string]string{"focused": "no", "items": "4", "cursor": "-1"}},
				{name: "overflow", note: "more cards than the viewport; list windows, the box holds height", width: 36, height: 16,
					props: map[string]string{"items": "12", "viewport": "8"}},
				{name: "narrow", note: "the board's ~30-column floor; pills wrap and grow the card", width: 30, height: 24,
					props: map[string]string{"items": "3"}},
			},
		},
		{
			name:     "cardtable",
			minWidth: 32, minHeight: 10,
			pkg:   "internal/tui/components/cardtable",
			title: "Already-painted cards joined into a grid",
			desc: "The entity-list grid: JoinHorizontal of painted cards with a one-column gutter, row height = the tallest card, how many columns fit, and the 1D-to-row map. " +
				"It receives already-painted card strings — the screen still calls card.Painter.Render and owns h/l j/k. " +
				"The demo paints real kanban cards with pills so row height follows wrap. " +
				"Crown is gallery chrome (the entity-list kicker+rule), not a cardtable field. " +
				"Empty is zero cards. Overflow is more cards than the frame; the demo windows to the frame the way the arranger does. Narrow is one column.",
			new: newCardtableDemo,
			scenarios: []scenario{
				{name: "a few columns", note: "three cells of 28 in 90, pills make row heights differ", width: 90, height: 20,
					props: map[string]string{"items": "6"}},
				{name: "one column", note: "a frame that only holds one cell", width: 32, height: 20,
					props: map[string]string{"items": "3"}},
				{name: "empty", note: "zero cards — nothing to join", width: 60, height: 10,
					props: map[string]string{"items": "0"}},
				{name: "overflow", note: "more cards than the frame; the demo windows to the frame", width: 90, height: 16,
					props: map[string]string{"items": "24"}},
				{name: "narrow", note: "one column at the entry floor; pills wrap", width: 32, height: 20,
					props: map[string]string{"items": "3"}},
				{name: "entity grid", note: "demo-paints the entity-list crown above Layout — not a cardtable field", width: 90, height: 22,
					props: map[string]string{"items": "6", "crown": "yes"}},
			},
		},
		{
			name:     "field",
			minWidth: 24, minHeight: 4,
			pkg:   "internal/tui/components/field",
			title: "Typed-in values: a line, or an area",
			desc: "Line is the single-line prompt behind the root's modal input and task detail's bucket-move bar, both of which had written it down to the same two-column indent. " +
				"The border is always the accent: an input on screen is an input being typed into, so the neutral variant would be a focus cue that never fires. " +
				"The model is taken BY VALUE and sized here, because a prompt's label is only knowable at render time. " +
				"Area is the textarea in the canonical bordered chrome — task description, inline comment-add, comment-edit. " +
				"RenderArea takes the textarea BY VALUE and sizes a throwaway copy; the persistent model must be calibrated separately through Resize, or the field goes blank on the first keystroke. " +
				"Shape picks which surface the gallery drives. They are not one tea.Model.",
			new: newFieldDemo,
			scenarios: []scenario{
				{name: "roomy", note: "label and value with space to spare", width: 80,
					props: map[string]string{"shape": "line"}},
				{name: "overflow", note: "the label eats most of the row", width: 60,
					props: map[string]string{"shape": "line", "label": "move #2421 from review to done"}},
				{name: "narrow", note: "too narrow for both — the row overflows on purpose", width: 24,
					props: map[string]string{"shape": "line", "label": "target bucket key"}},
				{name: "empty", note: "nothing typed yet", width: 60,
					props: map[string]string{"shape": "line", "value": ""}},
				{name: "focused", note: "the accent border", width: 70, height: 10,
					props: map[string]string{"shape": "area"}},
				{name: "blurred", note: "the neutral border", width: 70, height: 10,
					props: map[string]string{"shape": "area", "focused": "no"}},
				{name: "placeholder", note: "the placeholder", width: 70, height: 10,
					props: map[string]string{"shape": "area", "content": ""}},
				{name: "wrap floor", note: "wrapping at the floor the package enforces", width: 24, height: 10,
					props: map[string]string{"shape": "area"}},
				{name: "overflow · rows", note: "prose taller than the visible rows: the textarea scrolls, the cell does not grow", width: 60, height: 24,
					props: map[string]string{"shape": "area", "height": "3"}},
			},
		},
		{
			name:     "choice",
			minWidth: 32, minHeight: 4,
			pkg:   "internal/tui/components/choice",
			title: "The control glyph a selectable option wears",
			desc: "Radio, checkbox, and toggle: the three marks a list of choices can wear, extracted from the two picker screens that were painting the bullet and the checkbox themselves. " +
				"Screens translate domain into Option and Mode and own the cursor chevron. This package paints Glyph and Label only — dropdown owns the Detail join because the two pickers already disagree on how. " +
				"Toggle is the third mode the gallery has to show; the pickers do not use it yet. Empty is zero options, which is a blank, not a placeholder.",
			new: newChoiceDemo,
			scenarios: []scenario{
				{name: "radio", note: "one selected, the rest idle — the settings picker mark",
					props: map[string]string{"mode": "radio"}},
				{name: "checkbox", note: "independent marks — the persona-skills picker",
					props: map[string]string{"mode": "checkbox"}},
				{name: "toggle", note: "◉ / ☐ — the third mode; no picker paints it yet",
					props: map[string]string{"mode": "toggle"}},
				{name: "empty", note: "zero options — a blank, not a placeholder",
					props: map[string]string{"options": "0"}},
			},
		},
		{
			name:     "dropdown",
			minWidth: 32, minHeight: 4,
			pkg:   "internal/tui/components/dropdown",
			title: "The paint of a selectable option list",
			desc: "Options, open, filter, selection: the rows the two picker screens were assembling by hand. " +
				"Screens still own payload, list.Picker, screenlayout, save/cancel, and i18n labels; this package paints marker + glyph + label + Detail join + trailing. " +
				"Join is a presentation flag because the pickers already disagree: settings uses two spaces, relationship uses an em dash. Mode is per row so a create option stays Radio inside a checkbox list. " +
				"Open false is a collapsed trigger the gallery can show — the picker screens stay Open. Filter paints a chrome line when set; the pickers do not type-to-filter. Empty is zero options, not a placeholder.",
			new: newDropdownDemo,
			scenarios: []scenario{
				{name: "radio", note: "settings-like: Radio, DetailSpaced, one custom trailing",
					props: map[string]string{"mode": "radio", "join": "spaced"}},
				{name: "checkbox", note: "relationship-like: Checkbox with a Radio create row, DetailDash",
					props: map[string]string{"mode": "mixed", "join": "dash", "options": "3"}},
				{name: "empty", note: "zero options — a blank, not a placeholder",
					props: map[string]string{"options": "0"}},
				{name: "overflow", note: "more options than the frame; the demo windows to the height",
					width: 40, height: 8,
					props: map[string]string{"options": "16"}},
				{name: "narrow", note: "long labels into the floor; CapRows keeps the frame",
					width: 32, height: 8,
					props: map[string]string{"mode": "checkbox", "join": "dash", "options": "3"}},
				{name: "closed", note: "Open false — the selected label as a collapsed trigger",
					props: map[string]string{"open": "no"}},
				{name: "filter", note: "Filter paints a / chrome line above the options",
					width: 40, height: 8,
					props: map[string]string{"filter": "act"}},
			},
		},
		{
			name:     "header",
			minWidth: 48, minHeight: 8,
			pkg:   "internal/tui/components/header",
			title: "Root breadcrumb and zone strip",
			desc: "Four states share one Render: Home (select-a-project + HomeTitle), Overlay (breadcrumb only), Compact (active top + hint when the strip overflows), and Full (HOME │ tops + optional subs with underline rules). " +
				"Height and Width run the same state selection the paint does. Stack spends a terminal height budget across top / middle / bottom slots — the cut that used to be a silent clampViewToHeight chop.",
			new: newHeaderDemo,
			scenarios: []scenario{
				{name: "full strip", note: "HOME │ tops + subs with rules", width: 120},
				{name: "narrow", note: "strip overflows — active top + hint", width: 48,
					props: map[string]string{"state": "compact"}},
				{name: "overlay", note: "nav suppressed while help/stack owns focus", width: 100,
					props: map[string]string{"state": "overlay"}},
				{name: "home", note: "no project strip; HomeTitle takes the row", width: 100,
					props: map[string]string{"state": "home"}},
				{name: "overflow", note: "a segment long enough that the strip cannot hold both it and the nav", width: 60, height: 16,
					props: map[string]string{"segment": "architecture-review-closeout-tui-components", "subs": "yes"}},
			},
		},
		{
			name:     "overlay",
			minWidth: 70, minHeight: 14,
			pkg:   "internal/tui/components/overlay",
			title: "Help, notification chrome, and z-order splice",
			desc: "Three surfaces, not one tea.Model. Help owns the modal body and measured viewport — ViewportRows charges a measured header, a measured FooterHeight, and the leading blank Render emits. " +
				"Card is the notification chrome: size, border, tail, already-typed body, already-painted footer. The host owns config, time and id. " +
				"Place is Overlay(base, over, pos) — the nine-anchor splice palette and the live TUI share. " +
				"Help's floor is 70 (decisão #28); do not lower it. Squeeze the viewport and the last row becomes the scroll hint the host formats.",
			new: newOverlayDemo,
			scenarios: []scenario{
				{name: "current scope", note: "help: global + one surface group", width: 70, height: 20,
					props: map[string]string{"shape": "help"}},
				{name: "all groups", note: "help: extra settings section", width: 70, height: 22,
					props: map[string]string{"shape": "help", "scope": "all"}},
				{name: "help overflow", note: "help: viewport smaller than the body", width: 70, height: 14,
					props: map[string]string{"shape": "help", "viewport": "8", "scroll": "2", "scope": "all"}},
				{name: "help narrow", note: "key column and description competing for a width that holds neither — 70 is this overlay declared floor, and it is a HIGH floor", width: 70, height: 30,
					props: map[string]string{"shape": "help"}},
				{name: "settled confirm", note: "card: already typed, footer on", width: 70, height: 16,
					props: map[string]string{"shape": "card"}},
				{name: "no footer", note: "card: Footer empty", width: 70, height: 14,
					props: map[string]string{"shape": "card", "footer": "no"}},
				{name: "card overflow", note: "card: auto-height bubble with wrapped copy", width: 70, height: 18,
					props: map[string]string{"shape": "card", "text": "Line one.\nLine two is longer and wraps inside the bubble width.\nLine three."}},
				{name: "empty", note: "card: a bubble with no body: only the chrome", width: 70, height: 24,
					props: map[string]string{"shape": "card", "text": ""}},
				{name: "card narrow", note: "the authored box inside a terminal that still meets the help floor", width: 70, height: 24,
					props: map[string]string{"shape": "card"}},
				{name: "place splice", note: "AB/CD on a dotted base at top-left", width: 70, height: 16,
					props: map[string]string{"shape": "place", "anchor": "top-left"}},
			},
		},
		{
			name:     "tokenstrip · chips",
			minWidth: 8, minHeight: 4,
			pkg:   "internal/tui/components/tokenstrip",
			title: "Filter and period choice strips",
			desc: "Not a tokenstrip. Logs filter chips and the Stats period picker are horizontal choice rows that can overflow and must decide what to cut: trailing hint first, then inactive chips farthest from the active one, then truncate the active label. " +
				"Width runs the same cut Render does. BracketActive is the Logs flavour; Stats leaves the active label bare and relies on ActiveNav colour.",
			new: newChipstripDemo,
			scenarios: []scenario{
				{name: "logs filter", note: "bracketed active + (F cycle) hint", width: 80},
				{name: "narrow · logs", note: "hint drops, then distant chips", width: 36,
					props: map[string]string{"width": "36"}},
				{name: "stats period", note: "middot sep, no brackets", width: 40,
					props: map[string]string{"kind": "stats period", "active": "second"}},
				{name: "narrow · stats", note: "periods cut to the active one", width: 8,
					props: map[string]string{"kind": "stats period", "width": "8"}},
				{name: "overflow", note: "more chips than the budget holds", width: 30, height: 16,
					props: map[string]string{"width": "14"}},
			},
		},
		{
			name:     "panel",
			minWidth: 30, minHeight: 8,
			pkg:   "internal/tui/components/panel",
			title: "The framed body, the rule, and the chevron",
			desc: "Every screen body sits inside a panel: leading blank, indent, bordered box — the shape Kit.Panel paints, shown here as the wrap scenario. Four surfaces had each written some of this — the root's renderPanel / hRule / renderFixedBox, stats' and logs' assemblers, and the plan network's cursorChevron. " +
				"FixedBox is the manually-bordered rail whose height is always content+2; HRule refuses a non-positive width so strings.Repeat cannot panic at six subtraction sites; Chevron is the › glyph the plan network wants, distinct from CursorMarker's ▌. " +
				"FixedBoxHeight runs the same formula FixedBox draws, so a rail budget and a rail paint cannot drift.",
			new: newPanelDemo,
			scenarios: []scenario{
				{name: "wrapped body", note: "leading blank + indent + border", width: 60, height: 12,
					props: map[string]string{"kind": "wrap", "body lines": "4"}},
				{name: "fixed box", note: "height is always content+2", width: 40, height: 10,
					props: map[string]string{"kind": "fixed box", "body lines": "3", "width": "28"}},
				{name: "rule", note: "empty when the width is non-positive", width: 40,
					props: map[string]string{"kind": "rule", "width": "32"}},
				{name: "chevron on", note: "› and a trailing space", width: 30,
					props: map[string]string{"kind": "chevron", "selected": "yes"}},
				{name: "chevron off", note: "renders nothing", width: 30,
					props: map[string]string{"kind": "chevron", "selected": "no"}},
				{name: "narrow", note: "the rule and the box at a width that barely holds the chrome", width: 30, height: 20,
					props: map[string]string{"width": "18"}},
			},
		},
		{
			name:     "markdown",
			minWidth: 40, minHeight: 12,
			pkg:   "internal/tui/components/markdown",
			title: "Bodies through glamour, profile pinned",
			desc: "Task descriptions, comment bodies, entity prose and plan goals all render through this. Six surfaces used to inject a RenderBody callback into the root, and five golden suites invented a hard-wrap stand-in because they believed glamour's output depended on the terminal colour profile. " +
				"It does not: every TermRenderer is built with termenv.TrueColor, so the same body at the same width is the same ANSI under TERM=dumb and under a truecolor terminal. Goldens strip the ANSI and record the structure glamour actually emits. " +
				"Body honours the M toggle; Lines runs Body, so a height asked before a paint cannot disagree with the paint.",
			new: newMarkdownDemo,
			scenarios: []scenario{
				{name: "heading list", note: "rendered structure: heading, strong, bullets", width: 60,
					props: map[string]string{"mode": "rendered", "body": "heading list", "width": "48"}},
				{name: "raw source", note: "the M toggle — no glamour", width: 60,
					props: map[string]string{"mode": "raw", "body": "heading list", "width": "48"}},
				{name: "wrapping paragraph", note: "word wrap, not byte cut", width: 40,
					props: map[string]string{"mode": "rendered", "body": "paragraph", "width": "32"}},
				{name: "code fence", note: "inline code and a fenced block", width: 60,
					props: map[string]string{"mode": "rendered", "body": "code", "width": "48"}},
				{name: "narrow", note: "glamour wrapping into a column too tight for its own indent", width: 40, height: 24,
					props: map[string]string{"width": "28"}},
			},
		},
		{
			name:     "tokenstrip · pills",
			minWidth: 20, minHeight: 8,
			pkg:   "internal/tui/components/tokenstrip",
			title: "The pills a card carries, and how they pack",
			desc: "Every card in the app ends in a badge line, and until this package there were six implementations of the pills and four of the packer. " +
				"The pills take a COLOUR TOKEN, not a style: `config.priorities[].color` is one of four semantic names, so recolouring a priority in YAML repaints every surface at once and an unknown token falls back to the neutral pill rather than emitting unstyled text. " +
				"The packer is greedy and never truncates — a pill wider than the budget takes a row of its own, because a clipped pill reads as a different pill. " +
				"The two rules mark the budget: anything crossing them is overflow. " +
				"Wrap and Lines walk the SAME packer, which is the reason the package exists — the board counts a card's height before it has the width to render it, and the measurer that used to do that counting was a second copy of the loop kept honest by a comment. The status line reports both numbers and says so when they disagree.",
			new: newBadgeDemo,
			scenarios: []scenario{
				{name: "board card", note: "priority + counts, one row on a normal lane", width: 34,
					props: map[string]string{"tags": "0", "entity": "none"}},
				{name: "narrow", note: "the same three badges, now two rows", width: 20,
					props: map[string]string{"tags": "0", "entity": "none"}},
				{name: "tagged comment", note: "#tag pills after the counts", width: 44,
					props: map[string]string{"priority": "none", "blockers": "0", "comments": "0", "subtasks": "0", "tags": "6"}},
				{name: "law entity", note: "severity, scope, token band and the trailing markers", width: 60,
					props: map[string]string{"priority": "error", "blockers": "0", "comments": "0", "subtasks": "0", "tags": "0", "entity": "law"}},
				{name: "token bands", note: "past the alarm threshold, the spend pill goes red", width: 60,
					props: map[string]string{"priority": "none", "blockers": "0", "comments": "0", "subtasks": "0", "tags": "0", "entity": "persona", "tokens": "5200"}},
				{name: "narrow · budget of one", height: 26, note: "every pill overflows and every pill survives whole", width: 60,
					props: map[string]string{"width": "1"}},
				{name: "empty", note: "no badges is no row, not an empty row",
					props: map[string]string{"priority": "none", "blockers": "0", "comments": "0", "subtasks": "0", "tags": "0", "entity": "none"}},
				{name: "overflow", note: "every pill at once against a budget that holds a fraction of them", width: 30, height: 24,
					props: map[string]string{"tags": "12", "blockers": "99", "comments": "99", "subtasks": "99", "width": "20"}},
			},
		},
		{
			name:     "screenbody",
			minWidth: 56, minHeight: 14,
			pkg:   "internal/tui/components/screenbody",
			title: "Compose once, slice on every frame",
			desc: "The pushed-body contract. A screen composes when the payload or the geometry changes and View only slices. " +
				"The status line is the proof: compose calls stay at 1 while j/k move the offset. Resize the frame and the hook runs again, because width changed.",
			new: newScreenbodyDemo,
			scenarios: []scenario{
				{name: "composed", note: "40 rows stored; View slices", width: 80, height: 24},
				{name: "scrolled", note: "offset moved, compose calls still 1", width: 80, height: 24,
					props: map[string]string{"lines": "80"}},
				{name: "narrow", note: "resize recomposes at the new canvas width", width: 56, height: 18},
			},
		},
		{
			name:     "screenlayout",
			minWidth: 56, minHeight: 14,
			pkg:   "internal/tui/components/screenlayout",
			title: "The arranger a screen declares sections to",
			desc: "This is the standardisation layer. A screen does not compute a width or a row budget — it declares sections and the arranger hands both back, along with the scroll offset, the visible range and the terminal line the cursor landed on. " +
				"Narrow the frame and watch the BREAKPOINT: the body flips from side-by-side columns to a stack when any column's declared MinWidth stops fitting, and sections are dropped from the bottom rather than rendered into a budget they do not have. " +
				"Occupancy is measured from the strings a section actually produced, never declared — a header that wraps costs the rows it wraps to.",
			new: newScreenlayoutDemo,
			scenarios: []scenario{
				{name: "wide desktop", note: "both columns fit, activity takes 45%", width: 120, height: 30},
				{name: "narrow", note: "MinWidth stops fitting, the body stacks", width: 56, height: 30},
				{name: "overflow", note: "not enough rows for every minimum", width: 56, height: 14},
				{name: "why did it do that", note: "the resolved placements instead of the body",
					props: map[string]string{"body": "placements"}},
				{name: "no breakpoint", note: "the nineteen screens that never opt in",
					props: map[string]string{"columns": "never"}},
			},
		},
		{
			name:     "screenkit budgets",
			minWidth: 80, minHeight: 24,
			pkg:   "internal/tui/components/screenkit",
			title: "Every budget derived from one geometry",
			desc: "The Kit is the single derivation of the numbers six screens used to re-derive as `AvailableWidth() - chrome`, where six of thirteen chrome constants were wrong and every one of them under-charged. " +
				"ViewportRows returning 0 is not a failure — it is the documented \"this terminal is too small, render everything and let the host clamp\" answer. " +
				"Chrome is the measured tally: it renders the panel around a probe row and charges the difference, so a border change moves the budget with it instead of stranding a constant.",
			new: newScreenkitDemo,
			scenarios: []scenario{
				{name: "bare screen", note: "no host chrome above the body", height: 30},
				{name: "under a header", note: "the host spends 6 rows before the body starts",
					height: 30, props: map[string]string{"chrome rows": "6"}},
				// Frame stays tall enough for the number table; chrome rows force
				// ViewportRows to the documented zero without clipping the demo.
				{name: "narrow", note: "ViewportRows collapses to its documented zero", height: 30,
					props: map[string]string{"chrome rows": "25"}},
				{name: "with the tally", note: "the measured PanelChrome rows", height: 34,
					props: map[string]string{"tally": "yes"}},
				{name: "overflow", note: "host chrome taller than the terminal — the budget floors at zero rather than going negative", width: 80, height: 28,
					props: map[string]string{"chrome rows": "40"}},
			},
		},
		// `layout budgets` was the 30th component entry and it demoed
		// internal/tui/layout — TaskViewBudget, the hand-rolled row cascade for
		// task detail. The entry was deleted with the package (decision #23):
		// nothing in production called it any more. screenlayout resolves that
		// cascade now, from the sections the screen declares, and `shell` and
		// `Tasks › Detail` are where you watch it happen.
		{
			name:     "scrollwindow",
			minWidth: 40, minHeight: 18,
			pkg:   "internal/tui/components/scrollwindow",
			title: "The slice arithmetic every list shares",
			desc: "One derivation of the window for every list, grid and line viewport in the TUI. It runs against VARIABLE item heights, which is where a line offset and an item index stop being the same number — the confusion that produced the offset defects. " +
				"Resync is the invariant cardlist, linelist and the arranger all route through: it moves cursor and offset together so neither can drift. PartialRows is why an edge item is previewed instead of leaving the column short.",
			new: newScrollwindowDemo,
			scenarios: []scenario{
				{name: "roomy", note: "the whole band fits, nothing is hidden", height: 30},
				{name: "narrow", note: "hints reserved inside the budget",
					props: map[string]string{"viewport": "8", "hints": "HintsSplit"}},
				{name: "caller draws hints", note: "no rows reserved inside the window",
					props: map[string]string{"viewport": "8", "hints": "HintsNone"}},
				{name: "overflow", note: "offset past the top, both hints showing",
					props: map[string]string{"viewport": "8", "offset": "5", "cursor": "7"}},
			},
		},
		{
			name:     "list",
			minWidth: 30, minHeight: 8,
			pkg:   "internal/tui/components/list",
			title: "Items, cursor, window — four surfaces over scrollwindow",
			desc: "Cards, Window, Picker and Viewport are the same concept — items, a cursor or a scroll, a visible window, partials at the edges — and not the same state machine. " +
				"Cards starts with no selection; Window starts at 0 and leaves chrome to the parent; Picker exports Cursor/Scroll and dispatches keys; Viewport has no cursor, only document scroll plus Fit. " +
				"scrollwindow stays the math leaf underneath. Shape picks which surface the gallery drives.",
			new: newListDemo,
			scenarios: []scenario{
				{name: "board lane", note: "a narrow column of Painter cards with pills", width: 30, height: 24},
				{name: "empty", note: "Cards.View returns nothing at all",
					props: map[string]string{"shape": "cards", "items": "0"}},
				{name: "overflow", note: "cards grow to two and three rows",
					width: 30, props: map[string]string{"shape": "cards", "label": "resolve the layout exactly once per keystroke"}},
				{name: "narrow", note: "partial cards at both edges", width: 30, height: 12,
					props: map[string]string{"shape": "cards"}},
				{name: "single-row items", note: "one selectable unit per terminal row — the shape package linelist used to hold", width: 60, height: 24,
					props: map[string]string{"shape": "cards", "units": "lines", "items": "18"}},
				{name: "tight titles", note: "cards folded into a lane too tight for their titles", width: 30, height: 24,
					props: map[string]string{"shape": "cards", "items": "12"}},
				{name: "fits", note: "every Window row visible, no scrolling", height: 24,
					props: map[string]string{"shape": "rows", "items": "12"}},
				{name: "rows overflow", note: "more rows than the Window", height: 12,
					props: map[string]string{"shape": "rows", "items": "60"}},
				{name: "rows empty", note: "Window with no items at all",
					props: map[string]string{"shape": "rows", "items": "0"}},
				{name: "single", note: "Picker enter confirms the highlighted row", width: 40, height: 14,
					props: map[string]string{"shape": "picker"}},
				{name: "multi", note: "Picker space toggles, ctrl+s confirms", width: 40, height: 14,
					props: map[string]string{"shape": "picker", "mode": "multi"}},
				{name: "tall list", note: "Picker viewport smaller than the item count", width: 40, height: 12,
					props: map[string]string{"shape": "picker", "items": "20", "viewport": "5"}},
				{name: "fits, no footer", note: "Viewport content shorter than the window", height: 24,
					props: map[string]string{"shape": "viewport", "lines": "6", "viewport": "20"}},
				{name: "viewport overflow", note: "the combined footer hint appears", height: 24,
					props: map[string]string{"shape": "viewport", "lines": "40", "viewport": "12"}},
				{name: "scrolled", note: "Viewport hidden in both directions", height: 24,
					props: map[string]string{"shape": "viewport", "lines": "40", "scroll": "14", "viewport": "12"}},
				{name: "viewport empty", note: "no lines at all — the hint must not claim there is anything above or below", width: 60, height: 24,
					props: map[string]string{"shape": "viewport", "lines": "0"}},
				{name: "viewport narrow", note: "the hint has to fit the width too; this is where it used to overflow", width: 40, height: 24,
					props: map[string]string{"shape": "viewport", "lines": "40", "viewport": "20"}},
			},
		},
		{
			name:     "selectlist",
			minWidth: 40, minHeight: 8,
			pkg:   "internal/tui/components/selectlist",
			title: "Framed one-line selection list: box, kicker, › rows",
			desc: "The chrome list.Window leaves to its parent. Studio's left column on workflow, commands, personas and hooks is the same organism: a bordered box filling the cell, a kicker, a rule, then one-line rows with a › chevron and optional right-aligned trailing copy. " +
				"Screens still produce the row text and own scroll via screengrid + ScrollItems; this package only paints. " +
				"Height, when set, clips or pads the item window so the box holds its rows — overflow in the gallery, not a second scroll engine. " +
				"Narrow is Studio's 40-column list floor. The studio-column scenario is the left pane at ~48, the width a 120×40 terminal actually assigns beside the inspector.",
			new: newSelectlistDemo,
			scenarios: []scenario{
				{name: "idle", note: "kicker, rule, a few rows, cursor on the first", width: 44, height: 16,
					props: map[string]string{"items": "6", "cursor": "0"}},
				{name: "cursor row", note: "› on a mid-list row — the focused selection", width: 44, height: 16,
					props: map[string]string{"items": "6", "cursor": "2"}},
				{name: "overflow", note: "more rows than the box; Height clips, the frame holds", width: 44, height: 10,
					props: map[string]string{"items": "20", "height": "10", "cursor": "0"}},
				{name: "empty", note: "header + rule + Hint placeholder, no chevron", width: 44, height: 8,
					props: map[string]string{"items": "0"}},
				{name: "narrow", note: "Studio's 40-column list floor", width: 40, height: 12,
					props: map[string]string{"items": "4", "width": "36"}},
				{name: "studio column", note: "the left pane at ~48 — what 120×40 assigns beside the inspector", width: 48, height: 20,
					props: map[string]string{"items": "8", "width": "44", "trailing": "yes"}},
				{name: "workflow tones", note: "Warning tags, Success open, Hint muted — theme tokens, not hex", width: 48, height: 20,
					props: map[string]string{
						"items": "8", "width": "44", "trailing": "yes", "tones": "theme", "cursor": "4",
						"kicker trail": "omakase · clean",
						"subtitle":     "4 buckets · 7 guards · clean",
					}},
			},
		},
		{
			name:     "gridtable",
			minWidth: 40, minHeight: 24,
			pkg:   "internal/tui/components/gridtable",
			title: "Bordered tables: cells, summaries, detail, matrix",
			desc: "The leaf that owns Render, Summaries and Detail. Render paints a [][]string through shared junction glyphs — a single-cell row in a multi-column table is a SPANNED row whose dividers drop the internal junction. " +
				"Summaries is the side-by-side / stacked / merge-narrow policy Logs, Stats and Studio used to copy; Width runs the same sizing. " +
				"Detail is the fluent two-column builder the task, comment and entity views share — the gallery holds a viewport beside it the way every screen does, so a translated label like // COMENTÁRIOS never wraps and never shifts the outer panel. " +
				"A matrix is just cells: Empty, Disallowed, or a slug. RenderWithLayout reports which rendered line each input row landed on, because that mapping is not a formula.",
			new: newGridtableDemo,
			scenarios: []scenario{
				{name: "label / value", note: "the plain two-column table"},
				{name: "spanned rows", note: "single-cell rows span the full width",
					props: map[string]string{"rows": "spanned"}},
				{name: "overflow", note: "a cell too long for its column",
					width: 60, props: map[string]string{"rows": "wrapping"}},
				{name: "lopsided columns", note: "a label column wider than the value",
					props: map[string]string{"label width": "40"}},
				{name: "narrow", note: "both columns at their floor, with the frame narrower than their sum", width: 40, height: 24,
					props: map[string]string{"label width": "8", "value width": "12"}},
				{name: "side by side", note: "wide enough for the horizontal summaries layout", width: 120,
					props: map[string]string{"shape": "summaries", "label width": "13", "value width": "27"}},
				{name: "stacked", note: "room for one summary table, not two", width: 56,
					props: map[string]string{"shape": "summaries", "layout": "stacked", "label width": "13", "value width": "27"}},
				{name: "merge", note: "too narrow even for one — rows fold into a single table", width: 40,
					props: map[string]string{"shape": "summaries", "layout": "merge", "label width": "13", "value width": "27"}},
				{name: "auto labels", note: "label column grows to the widest kicker", width: 80,
					props: map[string]string{"shape": "summaries", "auto": "yes", "label width": "13", "value width": "27"}},
				{name: "short body", note: "detail fits, no footer", width: 50,
					props: map[string]string{"shape": "detail", "body lines": "1"}},
				{name: "detail overflow", note: "the viewport takes over — scroll stays in the demo", width: 50,
					props: map[string]string{"shape": "detail", "body lines": "24"}},
				{name: "translated labels", note: "the case that forces the label column to grow", width: 50,
					props: map[string]string{"shape": "detail", "labels": "translated"}},
				{name: "empty", note: "labels with no body under them", width: 80, height: 24,
					props: map[string]string{"shape": "detail", "body lines": "0"}},
				{name: "with guards", note: "three-bucket matrix: Empty / Disallowed / slug", width: 70, height: 24,
					props: map[string]string{"shape": "matrix"}},
				{name: "failure · nil snapshot", note: "allowed edges paint Empty", width: 70, height: 24,
					props: map[string]string{"shape": "matrix", "snapshot": "nil snapshot"}},
				{name: "matrix narrow", note: "the matrix at a width that cannot hold every column", width: 40, height: 24,
					props: map[string]string{"shape": "matrix"}},
			},
		},
		{
			name:     "tokenstrip · keys",
			minWidth: 40, minHeight: 6,
			pkg:   "internal/tui/components/tokenstrip",
			title: "The keybinding footer",
			desc: "Shared by the main chrome and by overlays. Only the first MaxPrimaries tokens get the accent — that budget is what keeps the row readable when a screen declares a dozen bindings. " +
				"RenderWrapped never truncates a token: one wider than the row is emitted on a line of its own.",
			new: newKeyfooterDemo,
			scenarios: []scenario{
				{name: "one row", note: "everything fits on a wide terminal", width: 110},
				{name: "overflow", note: "the same tokens on a narrow one", width: 40},
				{name: "centered", note: "alignment applied to every wrapped row",
					width: 40, props: map[string]string{"align": "center"}},
				{name: "narrow", note: "every primary keeps the accent, and the row stops chunking",
					width: 60, props: map[string]string{"max primaries": "8"}},
				{name: "empty", note: "no bindings — the footer owes zero rows, not one blank one", width: 80, height: 16,
					props: map[string]string{"tokens": "0"}},
			},
		},
		{
			name:     "screenstate",
			minWidth: 60, minHeight: 12,
			pkg:   "internal/tui/components/screenstate",
			title: "Loading, failed and empty — and which one wins",
			desc: "The three states a screen is in before it has a body. Every screen had written them itself and the copies disagreed four ways: some checked loading before err and some err before loading; seven painted the failure as the bare line kit.Panel(kit.Styles.Error.Render(s.err.Error())), a raw Go error with no screen identity above it; the empty state was painted through Styles.Empty, Styles.Hint, Styles.HintBox and twice with no style at all. " +
				"The precedence is Failed > Loading > Vazio and it lives in the component: turn two of the three flags on and watch the resolver, not the reading order, decide. " +
				"The kicker is structurally required — a State is only reachable through For(kicker), and Resolve skips any candidate that has none, so an unattributed panel is unwritable rather than merely discouraged. " +
				"Loading is STATIC: no spinner, no tea.Tick, no command in the event loop. There is no in-panel recovery hint either — the host already paints a footer carrying this screen's bindings.",
			new: newScreenstateDemo,
			scenarios: []scenario{
				{name: "loading", note: "kicker, blank, one Hint line — nothing animates", width: 60, height: 12,
					props: map[string]string{"failed": "no", "loading": "yes", "empty": "no"}},
				{name: "failure", note: "operator copy in Error, raw err.Error() under it in Hint", width: 76, height: 12,
					props: map[string]string{"failed": "yes", "loading": "no", "empty": "no"}},
				{name: "empty", note: "Empty tone without the kanban lane's width and centring", width: 60, height: 12,
					props: map[string]string{"failed": "no", "loading": "no", "empty": "yes",
						"hint": "Create one with `okt plan create <slug>`."}},
				{name: "precedence", note: "all three live — Failed wins, whatever order the screen listed them in", width: 76, height: 12,
					props: map[string]string{"failed": "yes", "loading": "yes", "empty": "yes"}},
				{name: "no state", note: "nothing live — the screen paints its real body", width: 60, height: 12,
					props: map[string]string{"failed": "no", "loading": "no", "empty": "no"}},
				{name: "narrow", note: "the long raw error folds inside the panel at 60 columns", width: 60, height: 14,
					props: map[string]string{"failed": "yes", "loading": "no", "empty": "no"}},
				{name: "overflow", note: "an error long enough to wrap several times inside a narrow body", width: 60, height: 24,
					props: map[string]string{"failed": "yes", "error": "open catalog: /home/howl/.local/share/omakiten/catalog.db: database is locked after 5 retries"}},
			},
		},
	}
}

// ---- fixture data ---------------------------------------------------------

const sampleProse = "The arranger resolves one layout per keystroke.\nSections declare what they need; the budget is not theirs to guess."

// samplePills is the badge line a board card carries: priority, then blocker /
// comment / sub-task counts, then tags. Shared by the card, lane, list and
// cardtable demos so none of them invents a fake pill.
func samplePills(c demoCtx, n int) []string {
	s := c.kit.Styles
	all := []string{
		tokenstrip.Pill(s, "error", "high"),
		tokenstrip.Count(s.BadgeBlocker, badgeText, 2, "tui.badge.blocker", "tui.badge.blockers"),
		tokenstrip.Count(s.BadgeComment, badgeText, 1, "tui.badge.comment", "tui.badge.comments"),
		tokenstrip.Count(s.BadgeSubtask, badgeText, 4, "tui.badge.subtask", "tui.badge.subtasks"),
		tokenstrip.Tag(s, "tui"), tokenstrip.Tag(s, "layout"), tokenstrip.Tag(s, "regression"), tokenstrip.Tag(s, "arch"),
	}
	return all[:clamp(n, 0, len(all))]
}

// sampleCards builds the stand-in cards the list and lane demos scroll through,
// sized to the width the surface around them actually has, painted through
// card.Painter with pills — the same construction the board uses.
//
// `width` is the CONTENT columns the card may occupy, borders included. A card
// told to draw at 28 inside a lane of 16 is doing exactly what it was asked:
// the card component's width floor protects against a DEGENERATE width, not a
// WRONG one.
func sampleCards(c demoCtx, cursor, count, width int, label string) []list.Item {
	return sampleKanbanItems(c, cursor, count, width, label, true)
}

// samplePaintedCards is sampleKanbanItems flattened to strings for cardtable,
// which joins already-painted cells and does not own a cursor chevron.
func samplePaintedCards(c demoCtx, count, width int) []string {
	items := sampleKanbanItems(c, 0, count, width, "resolve layout once", false)
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Content
	}
	return out
}

func sampleKanbanItems(c demoCtx, cursor, count, width int, label string, withCursor bool) []list.Item {
	if label == "" {
		label = "resolve layout once"
	}
	box := maxInt(width-2, 6)
	inner := maxInt(box-2, 1)
	painter := card.Painter{Styles: c.kit.Styles}
	items := make([]list.Item, maxInt(count, 0))
	for i := range items {
		nPills := 1 + i%3
		if width < 28 {
			nPills = 3
		}
		items[i] = list.NewItem(painter.Render(card.Spec{
			ID:         int64(400 + i),
			Title:      label,
			Badges:     samplePills(c, nPills),
			Selected:   i == cursor,
			Cursor:     withCursor && i == cursor,
			BoxWidth:   box,
			InnerWidth: inner,
		}))
	}
	return items
}

func sampleRows(c demoCtx, cursor, count int) []string {
	events := []string{
		"task.created", "task.moved", "comment.added", "guard.violated",
		"task.moved", "plan.wave.claimed", "task.done", "comment.edited",
		"task.created", "tool.call", "audit.snapshot", "task.moved",
		"comment.added", "guard.violated", "task.done", "plan.continued",
		"task.archived", "task.created",
	}
	lines := make([]string, maxInt(count, 0))
	for i := range lines {
		body := fmt.Sprintf("%-18s 14:%02d", events[i%len(events)], i*3%60)
		if i == cursor {
			body = c.kit.Styles.HintAccent.Render(body)
		} else {
			body = c.kit.Styles.Hint.Render(body)
		}
		lines[i] = c.kit.CursorMarker(i == cursor) + " " + body
	}
	return lines
}

// proseLines are the numbered sample rows the viewport demo scrolls.
//
// They are TRUNCATED to the frame, because a viewport windows rows vertically
// and does not wrap them: a line longer than the frame is a line that leaves it,
// and the demo was painting 79-column prose inside a 40-column frame.
func proseLines(c demoCtx, n int) []string {
	lines := plainProse(n)
	for i, line := range lines {
		row := fmt.Sprintf("%2d  %s", i+1, line)
		if c.kit.Width > 0 {
			row = screenkit.Truncate(row, c.kit.Width)
		}
		lines[i] = c.kit.Styles.Hint.Render(row)
	}
	return lines
}

func plainProse(n int) []string {
	source := []string{
		"A component held a private copy of a number another component already knew.",
		"The two disagreed, and the disagreement only showed up at one terminal width.",
		"Every fix in the wave had the same shape: delete the copy, ask the owner.",
		"Sections declare what they need instead of assembling a body string by hand.",
		"The offset never crosses the public API, so callers cannot pass the wrong unit.",
	}
	lines := make([]string, maxInt(n, 0))
	for i := range lines {
		lines[i] = source[i%len(source)]
	}
	return lines
}

func joinProse(n int) string { return strings.Join(plainProse(n), "\n") }

func labelValueRows(demoCtx) [][]gridtable.Cell {
	return [][]gridtable.Cell{
		{gridtable.Raw("// STATUS"), gridtable.Raw("in progress")},
		{gridtable.Raw("// OWNER"), gridtable.Raw("howl")},
		{gridtable.Raw("// UPDATED"), gridtable.Raw("2 hours ago")},
	}
}

func spannedRows(c demoCtx) [][]gridtable.Cell {
	return [][]gridtable.Cell{
		{gridtable.Styled(c.kit.Styles.Kicker("task"))},
		{gridtable.Raw("// STATUS"), gridtable.Raw("in progress")},
		{gridtable.Raw("// OWNER"), gridtable.Raw("howl")},
		{gridtable.Styled(c.kit.Styles.KickerCount("blockers", 2))},
		{gridtable.Raw("// #412"), gridtable.Raw("waiting on the migration")},
	}
}

func wrappingRows(demoCtx) [][]gridtable.Cell {
	return [][]gridtable.Cell{
		{gridtable.Raw("// NOTE"), gridtable.Raw(strings.Join(plainProse(3), " "))},
		{gridtable.Raw("// SHORT"), gridtable.Raw("fits")},
	}
}

func footerTokens() []tokenstrip.Key {
	return []tokenstrip.Key{
		{Key: "j/k", Label: "move", Primary: true},
		{Key: "enter", Label: "open", Primary: true},
		{Key: "n", Label: "new", Primary: true},
		{Key: "e", Label: "edit"},
		{Key: "d", Label: "done"},
		{Key: "/", Label: "search"},
		{Key: "tab", Label: "next pane"},
		{Key: "q", Label: "quit"},
	}
}

func detailValueWidth(c demoCtx) int {
	return maxInt(c.kit.Width-gridtable.LabelWidth-3, 8)
}
