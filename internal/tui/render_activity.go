package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/gridtable"
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
		lines = append(lines, m.renderScrollWindowSplit(body, heights, m.activityLines.Scroll(), m.activityViewportLines())...)
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
		out[i] = commentToEvent(c)
	}
	return out
}

// commentToEvent projects a domain.Comment into the domain.Event shape the
// activity renderers consume. Scope-aware: the comment's Scope (task |
// project | universal) selects entity_type, and EntityID is the comment's
// owning entity id — the task id for task-scoped rows, the project id for
// project-scoped rows, 0 for universal. A blank Scope (legacy/in-memory
// task comments) defaults to task so the existing task feed is unchanged.
func commentToEvent(c domain.Comment) domain.Event {
	scope := c.Scope
	if scope == "" {
		scope = domain.CommentScopeTask
	}
	entityID := c.TaskID
	switch scope {
	case domain.CommentScopeProject:
		entityID = c.ProjectID
	case domain.CommentScopeUniversal:
		entityID = 0
	}
	return domain.Event{
		ID:         c.ID,
		EntityType: scope,
		EntityID:   entityID,
		ProjectID:  c.ProjectID,
		EventType:  domain.EventTypeComment,
		Body:       c.Body,
		AuthorType: c.AuthorType,
		CreatedAt:  c.CreatedAt,
		Tags:       c.Tags,
	}
}

// commentsForProjectScope fetches project- and universal-scoped comment
// events for the current project, ready to feed the same activity/comment
// renderers the task feed uses. It is the reusable data seam #390's
// project-view screen will consume; the optional filter narrows by
// kind/tag/FTS/pinned without forcing callers to assemble a CommentFilter.
//
// The store's QueryComments matches universal rows (project_id NULL) only
// when ProjectID is 0, so a project-scoped read and the cross-project
// universal read are two separate queries here, then merged. A caller that
// pins filter.Scope to a single scope (project OR universal) short-circuits
// to the one matching query.
//
// The merged slice is pinned-first (pinned comments lead, then the store's
// natural created_at order) so a cover-sheet style read surfaces flagged
// handoffs at the top.
//
// Consumes app.CommentService.Query → store.QueryComments; it does not touch
// store internals or add a new store signature.
func (m Model) commentsForProjectScope(filter domain.CommentFilter) ([]domain.Event, error) {
	// Guard a nil Comments repo (e.g. boot before wiring, or a degraded
	// config). A nil-interface QueryComments call would panic, so short
	// out to an empty feed instead — mirrors the Plans-repo guards in
	// openPlanNetwork / openPlanGoalScreen.
	if m.repos.Comments == nil {
		return nil, nil
	}
	svc := app.NewCommentService(m.repos.Comments, m.repos.activeSnapshot())

	merged := make([]domain.Comment, 0)

	// Project-scoped rows for this project. Skipped when the caller pinned the
	// filter to universal only.
	if filter.Scope != domain.CommentScopeUniversal {
		pf := filter
		pf.ProjectID = m.project.ID
		if pf.Scope == "" {
			pf.Scope = domain.CommentScopeProject
		}
		comments, err := svc.Query(m.ctx, m.project, pf)
		if err != nil {
			return nil, err
		}
		merged = append(merged, comments...)
	}

	// Universal rows (cross-project, project_id NULL → ProjectID must be 0).
	// Skipped when the caller pinned the filter to project only.
	if filter.Scope != domain.CommentScopeProject {
		uf := filter
		uf.ProjectID = 0
		uf.Scope = domain.CommentScopeUniversal
		comments, err := svc.Query(m.ctx, m.project, uf)
		if err != nil {
			return nil, err
		}
		merged = append(merged, comments...)
	}

	// Stable pinned-first sort: preserves the store order within each group.
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Pinned && !merged[j].Pinned
	})
	out := make([]domain.Event, len(merged))
	for i, c := range merged {
		out[i] = commentToEvent(c)
	}
	return out, nil
}

// activityRowsForRender returns the rendered event card slice for the
// activity panel. The hot path on a keystroke previously called this
// from clampActivityScroll, visibleActivityCardRange,
// syncActivityScrollToCursor AND the renderTaskCommentsCell view pass
// — every call walked every event through renderCommentCardSelected
// or renderSystemEventCard (lipgloss border + body wrap). The result
// is now memoised on m.activityCardsCache; the value receiver reads
// from the cache when the fingerprint matches, otherwise it builds
// fresh cards but cannot persist them. *Model handlers that need to
// repopulate the cache call cachedActivityRowsForRender below.
func (m Model) activityRowsForRender(events []domain.Event) []string {
	if m.activityCardsCache.valid && m.activityCardsCache.key == m.activityRowsForRenderKey(events) {
		return m.activityCardsCache.cards
	}
	return m.activityRowsForRenderUncached(events)
}

// activityRowsForRenderUncached is the underlying renderer the cache
// short-circuits when warm. Kept separate so the cache hit path is a
// pure map read with zero card-render work.
func (m Model) activityRowsForRenderUncached(events []domain.Event) []string {
	return m.activityRowsForRenderWithCursor(events, m.activityCursor)
}

// activityRowsForRenderWithCursor renders the event cards with an explicit
// focused index instead of reading m.activityCursor. The task feed passes
// m.activityCursor (via activityRowsForRenderUncached); the project-view
// activity feed passes m.projectActivityCursor so the two feeds draw their
// own focused-card accent border without the task cursor leaking across.
func (m Model) activityRowsForRenderWithCursor(events []domain.Event, cursor int) []string {
	rows := make([]string, 0, len(events))
	for i, ev := range events {
		focused := i == cursor
		if ev.EventType == domain.EventTypeComment {
			rows = append(rows, m.renderCommentCardSelected(eventToComment(ev), focused))
			continue
		}
		rows = append(rows, m.renderSystemEventCard(ev, focused))
	}
	return rows
}

// cachedActivityRowsForRender is the *Model variant of
// activityRowsForRender that writes back into the per-model cache.
// Hot-path handlers (clampActivityScroll, syncActivityScrollToCursor,
// visibleActivityCardRange via moveActivityCursor) call this so the
// subsequent value-receiver render path inside renderTaskCommentsCell
// hits a warm cache.
func (m *Model) cachedActivityRowsForRender(events []domain.Event) []string {
	key := m.activityRowsForRenderKey(events)
	if m.activityCardsCache.valid && m.activityCardsCache.key == key {
		return m.activityCardsCache.cards
	}
	cards := m.activityRowsForRenderUncached(events)
	m.activityCardsCache = activityCardsCacheEntry{valid: true, key: key, cards: cards}
	return cards
}

// activityRowsForRenderKey fingerprints the inputs activityRowsForRender
// depends on: cursor (changes the focused-card accent border), commentCard
// content width (changes wrap), and per-event scope (entity_type +
// entity_id) + id + type + body content + tag identities. The full body
// content (not just its length) is folded so an edit-in-place that keeps the
// same character count — e.g. swapping "abc" for "xyz" — still bumps the key
// and the activity card re-renders the new text instead of reusing the stale
// cached card. writeString's null separator keeps adjacent fields
// unambiguous, so deterministic content folding does not widen the key into
// spurious misses. Tags are folded as (len, then sorted ids) so a swap (remove
// tag 3, add tag 5 — same length) still bumps the key while a reorder (tags
// returned in different order from the DB) does not.
//
// The scope is folded per-event (entity_type + entity_id) rather than keying
// the whole feed on m.taskID. Without this a project-scoped feed and a
// task-scoped feed that happened to share cursor + widths + event ids would
// hash identical and collide in m.activityCardsCache; the per-event scope
// fold keeps each feed's cards distinct.
func (m Model) activityRowsForRenderKey(events []domain.Event) uint64 {
	f := newFingerprint()
	f.writeInt64(int64(m.activityCursor))
	f.writeInt64(int64(m.commentCardWidth()))
	for _, ev := range events {
		f.writeString(ev.EntityType)
		f.writeInt64(ev.EntityID)
		f.writeInt64(ev.ID)
		f.writeString(ev.EventType)
		f.writeString(ev.Body)
		f.writeInt64(int64(len(ev.Tags)))
		if n := len(ev.Tags); n > 0 {
			ids := make([]int64, n)
			for i, t := range ev.Tags {
				ids[i] = t.ID
			}
			f.writeInt64Slice(ids)
		}
	}
	return f.sum()
}

// activityCardsCacheEntry is the memoised projection of
// activityRowsForRender; lives on the Model so a fresh value copy
// from the Bubbletea event loop carries the cache forward into the
// next render.
type activityCardsCacheEntry struct {
	valid bool
	key   uint64
	cards []string
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
// untouched while the activity feed funnels through Event. Scope-aware:
// the event's entity_type rides back onto Comment.Scope, and TaskID is set
// only for task-scoped rows so card/screen rendering does not assume a task
// owner for project/universal comments.
func eventToComment(ev domain.Event) domain.Comment {
	c := domain.Comment{
		ID:         ev.ID,
		ProjectID:  ev.ProjectID,
		Scope:      ev.EntityType,
		Body:       ev.Body,
		AuthorType: ev.AuthorType,
		CreatedAt:  ev.CreatedAt,
		Tags:       ev.Tags,
	}
	if ev.EntityType == "" || ev.EntityType == domain.EventEntityTask {
		c.Scope = domain.CommentScopeTask
		c.TaskID = ev.EntityID
	}
	return c
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

// scrollActivityLines nudges the activity body scroll by a raw line
// delta. Routes through linelist.Model.ScrollBy so the component's
// internal clamp keeps scroll inside [0, last renderable offset].
// Decouples body-scroll from cursor — useful when an expanded card
// is taller than the viewport and the user wants to read past its
// first screenful without losing cursor position.
func (m *Model) scrollActivityLines(delta int) {
	m.refreshActivityLines()
	m.activityLines = m.activityLines.ScrollBy(delta)
}

// refreshActivityLines rebuilds the linelist's body lines + viewport
// from the current event slice + measured viewport. Called from
// every *Model handler that touches activity scroll state so the
// linelist always has accurate inputs before its resync fires.
func (m *Model) refreshActivityLines() {
	events := m.activityForTaskInView(m.taskID)
	body := flattenActivityCards(m.cachedActivityRowsForRender(events))
	viewport := m.activityViewportLines()
	m.activityLines = m.activityLines.WithLines(body).WithViewport(viewport)
}

// toggleTaskFocus rotates which column inside the task detail screen
// owns j/k/enter. With three possible zones (form / sub-tasks / activity)
// the rotation is form → sub-tasks → activity → form. The sub-tasks
// zone is skipped automatically when the current task has no children
// so a barren detail screen still feels like a two-zone toggle.
//
// Side effects mirror the prior two-zone behaviour: entering activity
// auto-lands the cursor on a card so j/k always moves something
// visible, and entering sub-tasks clamps subtaskCursor to 0 for the
// same reason. Leaving a zone resets the cursor sentinel so the
// inactive panel stops drawing a focused border.
func (m *Model) toggleTaskFocus() {
	hasSubtasks := m.subtaskCount(m.taskID) > 0
	next := m.taskFocus
	switch m.taskFocus {
	case taskFocusForm:
		if hasSubtasks {
			next = taskFocusSubtasks
		} else {
			next = taskFocusActivity
		}
	case taskFocusSubtasks:
		next = taskFocusActivity
	case taskFocusActivity:
		next = taskFocusForm
	}
	m.applyTaskFocus(next)
}

// applyTaskFocus is the focus-set helper used by toggleTaskFocus and
// by per-key handlers that promote a zone (e.g. `j` on the form
// auto-rotates into the sub-tasks pane when one exists). Centralised
// so the cursor-anchor side effects (activity card / sub-task card)
// run regardless of how the focus changed. The page-level
// taskView.Viewport.Scroll is also snapped to the newly-focused
// section's offset so the zone always reads from the top of the
// terminal viewport instead of getting cropped halfway down in the
// stacked layout — the bug the user filed as "ao navegar via tab a
// coluna em foco deve estar no top da tela".
func (m *Model) applyTaskFocus(focus taskScreenFocus) {
	if focus == taskFocusSubtasks && m.subtaskCount(m.taskID) == 0 {
		focus = taskFocusActivity
	}
	m.taskFocus = focus
	switch focus {
	case taskFocusForm:
		m.activityCursor = -1
		m.subtasks = m.subtasks.WithItems(nil)
	case taskFocusSubtasks:
		m.activityCursor = -1
		m.refreshSubtaskList()
		if m.subtasks.Cursor() < 0 {
			m.subtasks = m.subtasks.JumpFirst()
		}
	case taskFocusActivity:
		m.subtasks = m.subtasks.WithItems(nil)
		if m.activityCursor < 0 {
			rows := len(m.activityForTaskInView(m.taskID))
			if rows > 0 {
				m.activityCursor = 0
				m.syncActivityScrollToCursor()
			}
		}
	}
	m.taskView.Viewport.Scroll = m.taskFocusedSectionOffset()
}

// moveActivityCursor advances the focus to the previous/next event card and
// auto-scrolls so the focused card stays inside the viewport. Wraps from
// "no selection" (-1) to the first or last card depending on direction so a
// single keypress always lands on a real row.
//
// When pgup/pgdn has scrolled the cursor off-screen (the cursor is decoupled
// from the body scroll by design), the next j/k anchors the cursor to the
// visible edge nearest its current position — first if the cursor sits
// above the viewport, last if below — regardless of the delta sign. Without
// this anchor, syncActivityScrollToCursor would snap the viewport back to
// the old cursor position and throw away the user's page-scroll work (the
// symptom filed as "activity column navigation snaps back after pgdown");
// gating on direction instead would jump the cursor BACKWARD on a "next"
// key when it sat below the viewport.
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
		if m.activityCursor < first {
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
	top := m.activityLines.Scroll()
	bottom := top + viewport
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
	cards := m.cachedActivityRowsForRender(events)
	body := flattenActivityCards(cards)
	ranges := cardLineRanges(cards)
	viewport := m.activityViewportLines()
	if viewport <= 0 || len(body) <= viewport {
		m.activityLines = m.activityLines.WithLines(body).WithViewport(viewport)
		return
	}
	// Feed the linelist the body + viewport, then place its cursor
	// on the focused card's LAST line (so the entire card fits
	// inside the slice with HintsSplit reservation). The linelist's
	// internal Resync handles the Follow + clamp chain.
	r := ranges[m.activityCursor]
	cardTop := r.start
	cardLast := r.start + r.height - 1
	// Clear any stale linelist cursor first — m.activityCursor is the
	// authoritative card cursor; the linelist's cursor is only used
	// as the Resync vehicle for the current sync call. Without this
	// clear, a prior sync's cursor (e.g. cardLast=3) would clamp the
	// Follow run via Follow's `if offset > cursor { offset = cursor }`
	// guard, snapping scroll back to that stale cursor instead of
	// honouring the user's pgup/pgdn work.
	list := m.activityLines.WithCursor(-1).WithLines(body).WithViewport(viewport).WithCursor(cardLast)
	scroll := list.Scroll()
	// Top-align tall expanded cards: if Follow drove scroll past the
	// card's first line, snap back so the header stays visible. The
	// scrollwindow.Follow chain inside WithCursor only ADVANCES
	// scroll to fit the cursor; snapping back to cardTop is a
	// separate UX rule.
	if scroll > cardTop {
		list = list.WithCursor(-1).ScrollBy(cardTop - scroll)
	}
	m.activityLines = list
}

// Chrome budget consumed by the embedded comment input + panel
// constants. The prior activityChromeBase / activityChromeStatus
// constants died with W12 — the screen-chrome accounting now lives
// inside taskViewportHeight (consumed via layout.TaskViewBudget)
// instead of being re-derived here.
const (
	// activityChromeEmbeddedInput: extra rows consumed by the embedded
	// comment input (header + 5 input rows + hint + padding).
	activityChromeEmbeddedInput = 9
	// activityViewportMinLines is the floor enforced on the computed slice
	// so the panel never collapses to a single card on a short terminal.
	activityViewportMinLines = 6
	// activityViewportFallbackLines is what we return when m.height has
	// not yet been initialised (program just started, no WindowSizeMsg
	// received yet) — large enough to render a few cards without flashing
	// an empty panel on first paint.
	activityViewportFallbackLines = 12
)

// activityViewportLines returns the inner row budget the activity
// linelist gets after layout chrome / borders. Routes through the
// task-view budget so the policy stays paired with the sub-tasks
// panel.
//
// Returns activityViewportMinLines as a floor for navigability when
// the panel will render but the budget collapses tight. The fallback
// path keeps the activityViewportFallbackLines value for early
// renders before WindowSizeMsg arrives.
func (m Model) activityViewportLines() int {
	if m.height <= 0 {
		return activityViewportFallbackLines
	}

	rows := 0
	if m.taskScreen == taskScreenView {
		if task, ok := m.activeTask(); ok {
			l := m.computeTaskViewLayout(m.availableWidth(), true)
			formH := m.cachedTaskDetailsBoxHeight(task, l)
			rows = m.taskViewBudget(l, formH).ActivityRows()
		}
	}

	// Subtract the embedded comment input from the available rows so
	// the panel does not grow when the user starts composing.
	if m.isEmbeddedCommentInput() {
		rows -= activityChromeEmbeddedInput
	}

	if rows < activityViewportMinLines {
		rows = activityViewportMinLines
	}
	return rows
}
