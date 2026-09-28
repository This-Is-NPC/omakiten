package screenlayout

import (
	"omakiten/internal/keynav"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/scrollwindow"
)

// Action is one thing the standard keys do to the focused section.
type Action int

const (
	ActionNone Action = iota
	ActionUp
	ActionDown
	ActionPageUp
	ActionPageDown
	ActionFirst
	ActionLast
	ActionNextSection
	ActionPrevSection
)

func (a Action) String() string {
	switch a {
	case ActionUp:
		return "Up"
	case ActionDown:
		return "Down"
	case ActionPageUp:
		return "PageUp"
	case ActionPageDown:
		return "PageDown"
	case ActionFirst:
		return "First"
	case ActionLast:
		return "Last"
	case ActionNextSection:
		return "NextSection"
	case ActionPrevSection:
		return "PrevSection"
	}
	return "None"
}

// Binding is one action, the key spellings that trigger it, and — for the
// actions worth advertising — how the footer names it.
//
// Footer and LabelKey live on the SAME rows [State.HandleKey] dispatches from,
// which is the point: a screen's footer and the keys the arranger actually
// accepts are one table, so a footer cannot advertise a key that does nothing
// and a key cannot work without appearing anywhere.
type Binding struct {
	Action Action
	Keys   []string
	// Footer is the key spelling shown to the user, empty for an action folded
	// into a sibling's hint (up is shown as part of "j/k").
	Footer string
	// LabelKey is the i18n catalog key for the hint's label. Empty exactly when
	// Footer is.
	LabelKey string
}

// standard is the one table. The key spellings are the ones the rest of the TUI
// already uses (board, entitylist, graph, the detail screens), so a screen that
// migrates onto this package keeps the keys its users know rather than
// acquiring a private dialect.
var standard = []Binding{
	{Action: ActionUp, Keys: []string{"up", "k"}},
	{Action: ActionDown, Keys: []string{"down", "j"}, Footer: "j/k", LabelKey: "tui.footer.scroll"},
	{Action: ActionPageUp, Keys: []string{"pgup", "ctrl+u"}},
	{Action: ActionPageDown, Keys: []string{"pgdown", "pgdn", "ctrl+d"}, Footer: "pgup/pgdn", LabelKey: "tui.footer.page"},
	{Action: ActionFirst, Keys: []string{"home", "g"}},
	{Action: ActionLast, Keys: []string{"end", "G"}, Footer: "g/G", LabelKey: "tui.footer.top_bottom"},
	{Action: ActionNextSection, Keys: keynav.Default.Zones.CloneKeys(), Footer: keynav.Default.Zones.Primary(), LabelKey: "tui.footer.focus"},
	// ActionPrevSection has no spelling: the zone ring only advances.
	//
	// CloneKeys/Primary are captured when this table is initialised. A
	// later config overlay of keynav.Default does not reach these rows
	// until the table is rebuilt — that rebuild belongs to the bundle
	// wiring step, not here.
}

// StandardBindings is the key table every scrollable section is routed by. A
// screen does not construct it, opt into it, or copy it.
func StandardBindings() []Binding {
	out := make([]Binding, len(standard))
	copy(out, standard)
	return out
}

// KeyHints is what the screen puts in its footer and help: the advertised
// subset of the standard table, for the sections actually present.
//
// Empty when nothing scrolls. Section cycling appears only when there are two
// sections to cycle between, so a single-section screen keeps `tab` for itself.
func KeyHints(sections ...Section) []Binding {
	scrollable := 0
	for _, s := range sections {
		if s.Spec().Scroll.Scrolls() {
			scrollable++
		}
	}
	if scrollable == 0 {
		return nil
	}
	var out []Binding
	for _, b := range standard {
		if b.LabelKey == "" {
			continue
		}
		if b.Action == ActionNextSection && scrollable < 2 {
			continue
		}
		out = append(out, b)
	}
	return out
}

// lookup maps a tea.KeyMsg.String() spelling onto an action.
func lookup(key string) (Action, bool) {
	for _, b := range standard {
		for _, k := range b.Keys {
			if k == key {
				return b.Action, true
			}
		}
	}
	return ActionNone, false
}

// HandleKey routes one keystroke to the focused section and returns the next
// state plus whether the arranger consumed it.
//
// The key is the `tea.KeyMsg.String()` spelling, taken as a plain string so this
// package stays a leaf with no bubbletea dependency of its own.
//
// It resolves the CURRENT geometry before applying anything, so a stale offset —
// the frame after a resize the Update path has not seen — corrects itself on the
// next keystroke instead of compounding. What it persists is always a clamped,
// paintable pair.
//
// Returns handled=false for a key the table does not own and for a body with no
// scrollable section, leaving the screen's own keys untouched.
//
// # One resolve per keystroke, and it is the one that paints
//
// This used to end in a second full [State.Resync], which re-rendered every
// section to settle the one pair the keystroke had just moved. Together with the
// frame that follows, that made a keystroke cost THREE renders of every section
// — a multiplier no screen declared and none could see, and one that matters as
// soon as a section's Render is genuinely expensive (Task Detail's detail block
// renders markdown, #2447).
//
// The second resolve went first (#2446): a keystroke changes exactly one
// section's cursor and offset and changes nothing a re-render would tell us that
// the resolve before it has not already measured. [reclamp] settles the pair
// from those, through the SAME scrollwindow.Resync the resolver runs, so there
// is still one implementation of "where does this cursor land".
//
// The first went with #2448, and this is where. A keystroke needs the
// MEASUREMENT of the body it is moving within, not its lines — the widths and
// rows come from the static specs and the geometry, and the focused section's
// item heights, item viewport and cursor statement are numbers. So the
// measurement is carried: [State.frameFor] hands back the one the last resolve
// took whenever every input this package can observe still describes it, and a
// fresh full resolve whenever it does not. What is left per keystroke is the
// resolve that PAINTS, which is the one a screen was always going to pay for.
//
// Two things keep that from being a cache with a correctness hole in it. The
// carried measurement is never consulted by [Arrange], so nothing it remembers
// can reach the screen; and a section whose body READ the cursor has its
// measurement retaken here the moment its cursor moves, because such a body may
// render differently for a different selection. What remains is the case this
// package already documents — a section whose heights follow its cursor settles
// against the heights measured before the move — and it self-corrects on the
// very next frame, because [Arrange] re-resolves and re-clamps whatever it is
// handed and cannot overdraw. Pinned by
// TestWhatAKeystrokePersistsIsAlreadyResynced.
func (s State) HandleKey(kit screenkit.Kit, key string, sections ...Section) (State, bool) {
	return s.HandleKeyIn(kit, HostBox(kit), key, sections...)
}

// HandleKeyIn is [State.HandleKey] for a caller that already has its box — the
// same reason [ArrangeIn] exists.
func (s State) HandleKeyIn(kit screenkit.Kit, box Box, key string, sections ...Section) (State, bool) {
	action, ok := lookup(key)
	if !ok {
		return s, false
	}
	body := s.frameFor(kit, box, sections)

	focusable := scrollableSections(body.sections)
	if len(focusable) == 0 {
		return s, false
	}
	focus := s.focus
	if indexOf(focusable, focus) < 0 {
		focus = focusable[0]
	}

	if action == ActionNextSection || action == ActionPrevSection {
		if len(focusable) < 2 {
			return s, false
		}
		return s.handleSectionMotion(action, focusable, focus, body)
	}

	next := s.absorb(body.sections).WithFocus(focus)
	for i, m := range body.sections {
		if m.spec.ID != focus {
			continue
		}
		if m.stale {
			// This body reads the cursor and the cursor has moved since it was
			// last measured, so what it would paint now is not what was
			// measured then. One section is re-measured; the rest are not
			// implicated, because a keystroke moves one cursor.
			m = renderSection(kit, m.spec, m.width, m.rows, next, sections[i], body.at(i)).measured
			body = body.with(i, m)
		}
		next = reclamp(apply(next, m, action), m)
		break
	}
	return next.remember(body.settledBy(next)), true
}

func scrollableSections(sections []measured) []ID {
	var focusable []ID
	for _, m := range sections {
		if !m.dropped && m.spec.Scroll.Scrolls() {
			focusable = append(focusable, m.spec.ID)
		}
	}
	return focusable
}

func (s State) handleSectionMotion(action Action, focusable []ID, focus ID, body *frame) (State, bool) {
	step := 1
	if action == ActionPrevSection {
		step = -1
	}
	at := (indexOf(focusable, focus) + step + len(focusable)) % len(focusable)
	cycled := s.absorb(body.sections).WithFocus(focusable[at])
	return cycled.remember(body.settledBy(cycled)), true
}

// reclamp settles the focused section's cursor and offset after a key moved
// one of them, out of the frame this keystroke already resolved.
//
// It is [renderSection]'s own cursor pipeline, in the same order, against the
// same measured heights and item viewport: re-seed a section that declared
// SelectFirst and has been driven off its list, fold the Block's cursor
// statement over the result, clamp it to the items that exist, snap it onto the
// section's selectable mask, then let scrollwindow.Resync bring the offset back
// into agreement with it. Only a scrollable section can hold focus, so there is
// no non-scrolling branch.
func reclamp(state State, m measured) State {
	seed := state.Cursor(m.spec.ID)
	if seed < 0 && m.spec.SelectFirst {
		seed = 0
	}
	cursor, offset := m.resyncPair(
		maskFloor(clampCursor(m.declared.resolve(seed), len(m.items)), m.selectable),
		state.Offset(m.spec.ID))
	return state.mutate(m.spec.ID, func(e *entry) {
		e.cursor, e.offset = cursor, offset
	})
}

// apply moves the focused section's cursor — or, when the section has no
// selection, its scroll offset.
//
// Which of the two happens is decided from the section's RESOLVED cursor, not
// from anything the screen declares. A body-scroll surface (a rendered
// description, an ascii graph) says nothing extra and gets offset movement; a
// list says nothing extra and gets cursor movement. There is no third field to
// set inconsistently.
func apply(state State, m measured, action Action) State {
	if m.cursor >= 0 {
		return state.mutate(m.spec.ID, func(e *entry) {
			e.cursor = clampCursor(cursorTarget(m, action), len(m.items))
		})
	}
	return state.mutate(m.spec.ID, func(e *entry) { e.offset = offsetTarget(m, action) })
}

func cursorTarget(r measured, action Action) int {
	if len(r.selectable) > 0 {
		return maskTarget(r, action)
	}
	// A page is counted in LINES and spent in ITEMS, so a grid pages by whole
	// rows of cards and lands on the same column it left. `perRow` is 1 for
	// every section that lays one item to a line, where this is the expression
	// that shipped.
	page := pageStep(r) * r.perRow
	switch action {
	case ActionUp:
		return r.cursor - 1
	case ActionDown:
		return r.cursor + 1
	case ActionPageUp:
		return r.cursor - page
	case ActionPageDown:
		return r.cursor + page
	case ActionFirst:
		return 0
	case ActionLast:
		return len(r.items) - 1
	}
	return r.cursor
}

func offsetTarget(r measured, action Action) int {
	// The offset moves a LINE at a time and is held as the first ITEM of that
	// line, so every step here is scaled by perRow — 1 for every section that
	// lays one item to a line, where this is the expression that shipped.
	heights := r.windowHeights()
	switch action {
	case ActionUp:
		return max(0, r.offset-r.perRow)
	case ActionDown:
		return r.offset + r.perRow
	case ActionPageUp:
		return max(0, r.offset-pageItems(r)*r.perRow)
	case ActionPageDown:
		return r.offset + pageItems(r)*r.perRow
	case ActionFirst:
		return 0
	case ActionLast:
		// Follow, not `total - viewport`: the naive bound ignores the row the
		// renderer spends on the "▼ N below" hint and leaves the last items
		// permanently unreachable, which is the defect scrollwindow.MaxOffset
		// was extracted to close. Follow is exact for variable heights too.
		return r.item(scrollwindow.Follow(r.line(r.offset), len(heights)-1, heights, r.itemViewport, scrollwindow.HintsSplit))
	}
	return r.offset
}

// pageItems is one page of a body-scroll surface, counted in ITEMS.
//
// The offset is an item index, so a page has to be a number of items. It used to
// be `itemViewport`, which is a ROW count — and a section whose window spends
// rows on the "▲ N above" / "▼ N below" hints shows FEWER items than the
// viewport has rows, so a page stepped clean over the items those hints
// displaced. On a short section that stranded them: paging is the key a user
// reaches for on a surface with no cursor, and no page ever landed on them
// (#2425, found by Project's migration).
//
// The rows a page may cover are [screenkit.ScrollDataRows] — the same author
// every other paging surface in the tree reads, and the very expression
// Project's own pre-migration pageStep used with the comment "paging by the full
// budget would step clean over the lines those hints displaced". The items are
// then counted off the MEASURED heights, so a body of multi-line cards pages by
// cards rather than by an average.
//
// It is deliberately independent of whether a hint is showing RIGHT NOW: a step
// that shrank at the top of the list would make pgup and pgdown different sizes
// and a page down followed by a page up would not return.
func pageItems(r measured) int {
	rows := screenkit.ScrollDataRows(r.itemViewport)
	heights := r.windowHeights()
	if r.itemViewport <= 0 || len(heights) == 0 {
		return 1
	}
	count, used, from := 0, 0, max(0, min(r.line(r.offset), len(heights)-1))
	for i := from; i < len(heights); i++ {
		if used+heights[i] > rows {
			break
		}
		used += heights[i]
		count++
	}
	return max(1, count)
}

// pageStep is half a viewport measured in ITEMS, derived from the measured item
// heights rather than from a per-surface constant.
//
// linelist pages by viewport/2 because its items are one row; cardlist pages by
// viewport/8 because it assumes four-row cards. Both are the same expression
// with a different guess at the average item height — and here the average is
// not a guess, because the arranger has already measured every item.
func pageStep(r measured) int {
	heights := r.windowHeights()
	if r.itemViewport <= 0 || len(heights) == 0 {
		return 2
	}
	total := 0
	for _, h := range heights {
		total += h
	}
	average := max(1, total/len(heights))
	return max(2, r.itemViewport/(2*average))
}

func indexOf(ids []ID, id ID) int {
	for i, candidate := range ids {
		if candidate == id {
			return i
		}
	}
	return -1
}
