package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/scrollwindow"
)

func (m Model) renderTaskCommentsCell(taskID int64) string {
	events := m.activityForTaskInView(taskID)

	header := m.styles.kickerCount(m.t("tui.kicker.activity"), len(events))
	if m.taskFocus == taskFocusActivity {
		header = m.styles.kickerCountFocused(m.t("tui.kicker.activity"), len(events))
	}
	lines := []string{header}

	if len(events) == 0 {
		lines = append(lines, "", m.styles.hint.Render(m.t("tui.empty.activity")), m.styles.hint.Render(m.t("tui.empty.activity_hint")))
	} else {
		cards := m.activityRowsForRender(events)
		// Build the full activity body as a flat line list so pagination is
		// line-based (not card-based). Expanded comments grow the body in
		// place; the viewport keeps the focused card visible without the
		// outer task scroll having to compensate. Hints are split (top/bottom)
		// rather than a single footer because the activity panel sits next to
		// the form column and a footer-only indicator would float far below
		// the cards it's describing.
		body := flattenActivityCards(cards)
		// Each body entry is one terminal line, so heights are uniformly
		// 1 — the same scroll math services this surface as the board /
		// entity grid (split ▲/▼ hints reserved inside viewport).
		heights := make([]int, len(body))
		for i := range heights {
			heights[i] = 1
		}
		lines = append(lines, m.renderScrollWindowSplit(body, heights, m.activityScroll, m.activityViewportLines())...)
	}
	if m.isEmbeddedCommentInput() && m.taskID == taskID {
		lines = append(lines, "", m.renderCommentInput())
	}
	return indentBlock(strings.Join(lines, "\n"), 2)
}

// flattenActivityCards splits each rendered card into its lines and joins
// them with a single blank separator between cards. The result is a flat
// []string the activity viewport slices line-by-line.
func flattenActivityCards(cards []string) []string {
	out := []string{}
	for i, card := range cards {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, strings.Split(card, "\n")...)
	}
	// Add a leading blank so cards visually breathe under the kicker; matches
	// the original "" + card spacing the previous card-based pagination used.
	if len(out) > 0 {
		out = append([]string{""}, out...)
	}
	return out
}

// cardLineRanges reports the line where each rendered card starts and how
// many lines it spans inside the flattened body produced by
// flattenActivityCards. Used by syncActivityScrollToCursor to scroll the
// focused card fully into view even when it has been expanded.
func cardLineRanges(cards []string) []struct{ start, height int } {
	out := make([]struct{ start, height int }, len(cards))
	cursor := 1 // skip the leading blank line
	for i, card := range cards {
		if i > 0 {
			cursor++ // blank separator
		}
		h := len(strings.Split(card, "\n"))
		out[i] = struct{ start, height int }{start: cursor, height: h}
		cursor += h
	}
	return out
}

// activityForTaskInView returns the loaded activity feed when the task
// detail view is showing the same task. When m.activity hasn't been loaded
// yet (or belongs to a different task), it falls back to projecting from
// m.comments so the panel still surfaces something useful instead of going
// blank during the initial render.
func (m Model) activityForTaskInView(taskID int64) []domain.Event {
	if m.activityForTask == taskID {
		return m.activity
	}
	comments := m.commentsForTask(taskID)
	out := make([]domain.Event, len(comments))
	for i, c := range comments {
		out[i] = domain.Event{
			ID:         c.ID,
			EntityType: domain.EventEntityTask,
			EntityID:   c.TaskID,
			ProjectID:  c.ProjectID,
			EventType:  domain.EventTypeComment,
			Body:       c.Body,
			AuthorType: c.AuthorType,
			CreatedAt:  c.CreatedAt,
			Tags:       c.Tags,
		}
	}
	return out
}

// activityRowsForRender renders each event card up front so pagination and
// overflow accounting work on a stable list. Comments reuse the existing
// commentCard (author + body + tags); system events use the same border color
// as comments so the activity column reads as one cohesive stack. The focused
// card (activityCursor) gets an accent border so card navigation is discoverable.
func (m Model) activityRowsForRender(events []domain.Event) []string {
	rows := make([]string, 0, len(events))
	for i, ev := range events {
		focused := i == m.activityCursor
		if ev.EventType == domain.EventTypeComment {
			rows = append(rows, m.renderCommentCardSelected(eventToComment(ev), focused))
			continue
		}
		rows = append(rows, m.renderSystemEventCard(ev, focused))
	}
	return rows
}

// renderSystemEventCard formats task.created/moved/completed in a card that
// matches the comment card geometry but reads as metadata: dimmer border,
// no author header, single-line label + timestamp. Boxed (vs. the previous
// borderless variant) so the activity column stays visually consistent.
func (m Model) renderSystemEventCard(ev domain.Event, focused bool) string {
	label := m.systemEventLabel(ev)
	timestamp := strings.TrimSpace(ev.CreatedAt)
	width := m.commentCardContentWidth()
	if width < 8 {
		width = 8
	}
	line := m.styles.muted.Render(label)
	if timestamp != "" {
		line += m.styles.hint.Render(" · " + timestamp)
	}
	// Wrap to the same content width as comments so long event labels (e.g.
	// "task moved review → done · 2026-05-06 03:17:47") don't run past the
	// panel border.
	wrapped := gridtable.WrapLines([]string{line}, width)
	body := strings.Join(wrapped, "\n")
	style := m.styles.systemEventCard.Width(m.commentCardWidth())
	if focused {
		style = style.BorderForeground(m.styles.hintAccent.GetForeground())
	}
	return style.Render(body)
}

// eventToComment narrows a comment-typed Event back into the Comment
// shape that renderCommentCard expects. Lets the comment renderer stay
// untouched while the activity feed funnels through Event.
func eventToComment(ev domain.Event) domain.Comment {
	return domain.Comment{
		ID:         ev.ID,
		ProjectID:  ev.ProjectID,
		TaskID:     ev.EntityID,
		Body:       ev.Body,
		AuthorType: ev.AuthorType,
		CreatedAt:  ev.CreatedAt,
		Tags:       ev.Tags,
	}
}

// systemEventLabel renders task.* events as human-readable strings using
// the payload's `from`/`to`/`bucket` fields. Falls back to the bare event
// type when payload is missing or malformed — defensive because old rows
// that pre-date the migration carry an empty payload string.
func (m Model) systemEventLabel(ev domain.Event) string {
	switch ev.EventType {
	case domain.EventTypeTaskCreated:
		bucket := payloadField(ev.Payload, "bucket")
		if bucket != "" {
			return fmt.Sprintf(m.t("tui.event.task_created_in_fmt"), bucket)
		}
		return m.t("tui.event.task_created")
	case domain.EventTypeTaskMoved:
		from := payloadField(ev.Payload, "from")
		to := payloadField(ev.Payload, "to")
		if from != "" && to != "" {
			return fmt.Sprintf(m.t("tui.event.task_moved_from_to_fmt"), from, to)
		}
		if to != "" {
			return fmt.Sprintf(m.t("tui.event.task_moved_to_fmt"), to)
		}
		return m.t("tui.event.task_moved")
	case domain.EventTypeTaskCompleted:
		bucket := payloadField(ev.Payload, "bucket")
		if bucket != "" {
			return fmt.Sprintf(m.t("tui.event.task_completed_in_fmt"), bucket)
		}
		return m.t("tui.event.task_completed")
	}
	return ev.EventType
}

// payloadField extracts a top-level string field from the Event.Payload JSON.
// Tolerant of empty/malformed payloads — returns "" instead of erroring so
// rendering never breaks on partial data.
func payloadField(payload, key string) string {
	if payload == "" || payload == "{}" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return ""
	}
	if v, ok := data[key].(string); ok {
		return v
	}
	return ""
}

// scrollActivityLines nudges activityScroll by a raw line delta and clamps
// to valid range. Lets pgup/pgdn page the activity body independently of
// the cursor — useful when a single expanded card is taller than the
// viewport and the user wants to read past its first screenful.
func (m *Model) scrollActivityLines(delta int) {
	m.activityScroll += delta
	m.clampActivityScroll()
}

// clampActivityScroll keeps activityScroll inside [0, max], where max is
// the offset that keeps the LAST body line visible inside the scrollwindow
// budget. The +1 accounts for the row renderScrollWindowSplit reserves
// for the "▲ N above" hint whenever offset > 0 — without it, G / sustained
// pgdown left the final card's tail cropped behind a "▼ N below" that
// lied about still-reachable rows. Computes total by re-rendering cards,
// which is cheap and avoids the caller having to thread the body length
// through.
func (m *Model) clampActivityScroll() {
	events := m.activityForTaskInView(m.taskID)
	body := flattenActivityCards(m.activityRowsForRender(events))
	viewport := m.activityViewportLines()
	if viewport <= 0 || len(body) <= viewport {
		m.activityScroll = 0
		return
	}
	maxScroll := activityMaxScroll(len(body), viewport)
	if m.activityScroll < 0 {
		m.activityScroll = 0
	}
	if m.activityScroll > maxScroll {
		m.activityScroll = maxScroll
	}
}

// activityMaxScroll is the largest offset that still renders the last body
// line inside the activity viewport given the split-hint reservation
// renderScrollWindowSplit applies. When offset > 0 the renderer eats one
// row for the "▲ N above" hint; the +1 compensates so the last line is
// reachable. Floored at 0 — callers should early-return when the body
// fits without scroll, but defending here keeps the helper composable.
func activityMaxScroll(bodyLen, viewport int) int {
	max := bodyLen - viewport + 1
	if max < 0 {
		return 0
	}
	return max
}

// toggleTaskFocus flips which column inside the task detail screen owns
// j/k/enter. Re-entering activity focus auto-lands the cursor on a card so
// the first navigation key always moves something visible — the user gets
// instant feedback instead of pressing j a few times into the void.
//
// We also reset taskViewScroll when leaving the form so the activity column
// renders from the top of the joined output; the activity panel manages its
// own internal viewport and shouldn't be at the mercy of the form's scroll
// state.
func (m *Model) toggleTaskFocus() {
	if m.taskFocus == taskFocusForm {
		m.taskFocus = taskFocusActivity
		m.taskView.Viewport.Scroll = 0
		if m.activityCursor < 0 {
			rows := len(m.activityForTaskInView(m.taskID))
			if rows > 0 {
				m.activityCursor = 0
				m.syncActivityScrollToCursor()
			}
		}
		return
	}
	m.taskFocus = taskFocusForm
	m.activityCursor = -1
}

// moveActivityCursor advances the focus to the previous/next event card and
// auto-scrolls so the focused card stays inside the viewport. Wraps from
// "no selection" (-1) to the first or last card depending on direction so a
// single keypress always lands on a real row.
//
// When pgup/pgdn has scrolled the cursor off-screen (the cursor is decoupled
// from the body scroll by design), the next j/k anchors the cursor to the
// first/last visible card instead of applying delta blindly. Without this,
// syncActivityScrollToCursor would snap the viewport back to the old cursor
// position and throw away the user's page-scroll work — the symptom filed as
// "activity column navigation snaps back after pgdown".
func (m *Model) moveActivityCursor(delta int) {
	rows := len(m.activityForTaskInView(m.taskID))
	if rows == 0 {
		m.activityCursor = -1
		return
	}
	if m.activityCursor < 0 {
		if delta > 0 {
			m.activityCursor = 0
		} else {
			m.activityCursor = rows - 1
		}
		m.syncActivityScrollToCursor()
		return
	}
	if first, last, ok := m.visibleActivityCardRange(); ok && (m.activityCursor < first || m.activityCursor > last) {
		if delta > 0 {
			m.activityCursor = first
		} else {
			m.activityCursor = last
		}
		m.syncActivityScrollToCursor()
		return
	}
	next := m.activityCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= rows {
		next = rows - 1
	}
	m.activityCursor = next
	m.syncActivityScrollToCursor()
}

// visibleActivityCardRange returns the inclusive [first, last] card indices
// whose start line falls inside the current activity viewport window. ok=false
// when the feed is empty or no card start sits in the visible band (a tall
// card whose top scrolled past the viewport edge counts as out of range —
// callers can still anchor onto neighbours).
func (m Model) visibleActivityCardRange() (int, int, bool) {
	events := m.activityForTaskInView(m.taskID)
	if len(events) == 0 {
		return 0, 0, false
	}
	ranges := cardLineRanges(m.activityRowsForRender(events))
	viewport := m.activityViewportLines()
	if viewport <= 0 {
		return 0, 0, false
	}
	top := m.activityScroll
	bottom := m.activityScroll + viewport
	first, last := -1, -1
	for i, r := range ranges {
		if r.start >= top && r.start < bottom {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return 0, 0, false
	}
	return first, last, true
}

// syncActivityScrollToCursor positions activityScroll (a LINE offset, not
// a card index) so the focused card's body is visible inside the viewport.
// Delegates the slice math to scrollwindow.Follow with HintsSplit so the
// hint-row reservation is identical to what renderScrollWindowSplit will
// consume — without that the bottom of the card silently lands behind
// the "▼ N below" row and stays invisible. Tall expanded cards prefer
// top-aligned so the header is reachable; the user can keep pressing j
// to step into the rest.
func (m *Model) syncActivityScrollToCursor() {
	if m.activityCursor < 0 {
		return
	}
	events := m.activityForTaskInView(m.taskID)
	if m.activityCursor >= len(events) {
		return
	}
	cards := m.activityRowsForRender(events)
	body := flattenActivityCards(cards)
	ranges := cardLineRanges(cards)
	viewport := m.activityViewportLines()
	if viewport <= 0 || len(body) <= viewport {
		m.activityScroll = 0
		return
	}
	heights := make([]int, len(body))
	for i := range heights {
		heights[i] = 1
	}
	r := ranges[m.activityCursor]
	cardTop := r.start
	cardLast := r.start + r.height - 1
	scroll := scrollwindow.Follow(m.activityScroll, cardLast, heights, viewport, scrollwindow.HintsSplit)
	if scroll > cardTop {
		scroll = cardTop
	}
	if scroll < 0 {
		scroll = 0
	}
	if maxScroll := activityMaxScroll(len(body), viewport); scroll > maxScroll {
		scroll = maxScroll
	}
	m.activityScroll = scroll
}

// activityViewportLines is the maximum number of LINES the activity column
// renders before pagination kicks in. Sized to consume the outer
// `taskViewportHeight` budget so the column grows with the terminal — the
// previous static chrome=12 left ~3 unused rows on every height because it
// double-counted the screen header/footer the outer viewport already owns.
//
// The reserved chrome rows inside the panel are:
//   - 2 for the screen header (renderHeader)
//   - 1 for the leading blank applyTaskViewScroll prepends
//   - 2 for the screen footer (separator + keybindings)
//   - 1 each for the activity panel's box top + bottom border
//   - 1 for the kicker row ("// ACTIVITY · N") inside the panel
//   - 1 trailing margin so the bottom border is never the last row written
//     to the alt-screen (terminals that reserve a row for the cursor or a
//     status line otherwise clip the border)
//   - 1 extra when m.status renders an inline badge row
//   - 9 extra when an embedded comment input is open (header + 5 input
//     rows + hint + padding)
func (m Model) activityViewportLines() int {
	if m.height <= 0 {
		return 12
	}
	chrome := 9
	if m.status != "" {
		chrome++
	}
	if m.isEmbeddedCommentInput() {
		chrome += 9
	}
	rows := m.height - chrome
	if rows < 6 {
		rows = 6
	}
	return rows
}
