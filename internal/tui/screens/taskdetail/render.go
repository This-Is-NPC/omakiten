package taskdetail

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/taskprojection"
	"omakiten/internal/tui/components/card"
	"omakiten/internal/tui/components/cardtable"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/list"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
)

const (
	descriptionLineCap = 5
	commentInputHeight = 5
)

type subtaskLayout struct {
	columnInner      int
	cardWidth        int
	cardContentWidth int
	capacity         int
}

func (s Screen) render(frame screenhost.Frame) string {
	switch s.mode {
	case ModeBlockers:
		return s.renderBlockers(frame)
	case ModeMove:
		return s.renderMove(frame)
	default:
		return s.renderTask(frame)
	}
}

func (s Screen) renderMove(frame screenhost.Frame) string {
	kit := frame.Kit()
	box := screenlayout.Box{Width: frame.Width(), Rows: frame.Height()}
	return screengrid.Render(kit, s.grid, box, s.moveRoot(frame)).View
}

func (s Screen) moveSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionMove, MinRows: 1},
		Body: func(screenlayout.Canvas) screenlayout.Block {
			view := s.renderMoveInput(frame) + "\n" + s.renderTask(frame)
			return screenlayout.Block{Header: framed.Document(view), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s Screen) moveRoot(frame screenhost.Frame) screengrid.Node {
	section := s.moveSection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

// renderTask arranges the screen's three zones through one nested grid and
// returns the body verbatim. The left stack is a real container, so Resync
// preserves a focused leaf instead of rebuilding a parallel body state.
func (s Screen) renderTask(frame screenhost.Frame) string {
	kit := s.bodyKit(frame)
	box := screenlayout.HostBox(kit)
	if s.payload.Task.ID == 0 {
		box.Width = frame.Width()
		box.Rows = frame.Height()
		return screengrid.Render(kit, s.grid, box, s.bodyRoot(frame)).View
	}
	view := screengrid.Render(kit, s.grid, box, s.bodyRoot(frame)).View
	return screenkit.Indent("\n"+view, 2)
}

// bodyRoot is the complete task-detail tree: details and subtasks stack in a
// left column beside activity. Only the three leaves are focusable zones.
func (s Screen) emptyTaskSection(frame screenhost.Frame) screenlayout.Func {
	kit := frame.Kit()
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionState, MinRows: 1},
		Body: func(screenlayout.Canvas) screenlayout.Block {
			view := kit.Panel(frame.Text("tui.empty.task_not_found_refresh"))
			return screenlayout.Block{Header: framed.Document(view), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (s Screen) bodyRoot(frame screenhost.Frame) screengrid.Node {
	if s.payload.Task.ID == 0 {
		section := s.emptyTaskSection(frame)
		return screengrid.Cell(section.Def, section.Body)
	}
	if s.mode == ModeBlockers {
		return s.blockerRoot(frame)
	}
	detailsSection := s.detailsSection(frame)
	subtasksSection := s.subtasksSection(frame)
	activitySection := s.activitySection(frame)
	details := screengrid.Cell(detailsSection.Def, detailsSection.Body)
	subtasks := screengrid.Cell(subtasksSection.Def, subtasksSection.Body)
	activity := screengrid.Cell(activitySection.Def, activitySection.Body)
	// Same tree at every width. screengrid.stack / enterFocused decide whether
	// the body is side by side, stacked, windowed, or one focused zone filling
	// the box — the screen does not rebuild a flatter tree when it stacks.
	//
	// Move mode is the exception: the bucket input sits above the body, so a
	// stacked terminal cannot afford the three zones plus that chrome. The
	// side-by-side packing still can, and keeps the full tree.
	box := screenlayout.HostBox(s.bodyKit(frame))
	if s.mode == ModeMove && !screenlayout.FitsSideBySide(box, detailsSection.Def, subtasksSection.Def, activitySection.Def) {
		return details
	}
	left := screengrid.Rows(screenlayout.Spec{
		ID: groupLeft, MinWidth: screenkit.DetailColumnFloor,
		MinRows: detailsMinRows + subtasksMinRows,
	}, details, subtasks)
	return screengrid.Cols(screenlayout.Spec{ID: "taskdetail"}, left, activity)
}

// bodyKit is the geometry the arranged body is budgeted against.
//
// It is the frame's kit, except in move mode, where the bucket-move box sits
// ABOVE the body and has to be paid for. screenlayout.BodyRows takes only the
// kit — there is no way for a screen to declare chrome above the arranged body
// the way screenkit.Chrome().Lines(...) lets it declare chrome around a panel —
// so the rows the box occupies are charged to the host chrome, which is the one
// number both derivations already share. Measured from the box the screen
// actually renders, never assumed.
func (s Screen) bodyKit(frame screenhost.Frame) screenkit.Kit {
	kit := frame.Kit()
	if s.mode == ModeMove {
		kit.ChromeRows += screenkit.BlockRows(s.renderMoveInput(frame))
	}
	return kit
}

// sections is the screen's whole layout declaration: the details block and the
// subtask board stacked inside one column, the activity feed beside them.
//
// Declaration order is load-bearing under the hard MinRows floor: activity is
// last so a short stacked terminal drops the feed before it shortens the task
// or its subtasks. See the const block on feedMinRows.
func (s Screen) sections(frame screenhost.Frame) []screenlayout.Section {
	return []screenlayout.Section{
		s.detailsSection(frame),
		s.subtasksSection(frame),
		s.activitySection(frame),
	}
}

func (s Screen) detailsSpec() screenlayout.Spec {
	spec := screenlayout.BesideFeed(sectionDetails, groupLeft, screenkit.DetailColumnFloor, detailsMinRows, screenlayout.ScrollItems)
	spec.Weight = detailsWeight
	// A column-scoped ceiling: it caps the preview while details shares the
	// left column with sub-tasks, and the arranger drops it the moment there
	// is no column left to share — stacked past the breakpoint, or fullscreen.
	spec.ColumnMaxRows = s.detailsCeiling()
	return spec
}

// detailsCeiling is how many rows the details grid may take, description
// preview included. Same shape as Studio's inspectorFieldsCeiling: a formula,
// never a paint, so TaskTags stays a once-per-paint counter.
func (s Screen) detailsCeiling() int {
	blockers := len(s.projection.Blockers(s.payload.Task.ID))
	if blockers == 0 {
		blockers = 1
	}
	desc := 1
	if strings.TrimSpace(s.payload.Task.Description) != "" {
		desc = descriptionLineCap + 1
	}
	// Unwrapped grid: top+kicker+rule, row+rule per meta field, blockers
	// kicker+rule and body+rule, description kicker+rule, desc lines, bottom.
	// 3*fields used to reserve a wrap row per field; those unused rows were
	// assigned to details, never painted, and vanished from the left column
	// instead of stretching sub-tasks to the activity bottom.
	unwrapped := 3 + 2*detailsMetaFields + 2 + 2*blockers + 2 + desc + 1
	return max(detailsMinRows, unwrapped+s.detailsTitleWrap())
}

func (s Screen) detailsTitleWrap() int {
	valueWidth := 16
	if s.frame.Width() > 0 {
		left := max(screenkit.DetailColumnFloor, screenlayout.HostBox(s.bodyKit(s.frame)).Width/2)
		valueWidth = max(16, left-(gridtable.LabelWidth+3))
	}
	width := screenkit.VisibleWidth(s.payload.Task.Title)
	if width <= valueWidth {
		return 0
	}
	return (width+valueWidth-1)/valueWidth - 1
}

func (s Screen) detailsBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	task := s.payload.Task
	key := taskDetailBlockKey{width: canvas.Width(), generation: s.payload.Generation, taskID: task.ID, focus: s.focus, mode: s.mode, offset: s.grid.Layout().Offset(sectionDetails), cursor: s.activityCursor(), subtaskCursor: s.subtasks.Cursor(), title: task.Title, description: task.Description, bucket: task.BucketKey, priority: fmt.Sprintf("%d", task.Priority), deps: len(s.projection.Blockers(task.ID)), full: s.grid.Fullscreen() && s.focus == FocusDetails}
	build := func() screenlayout.Block {
		return screenlayout.Block{Items: strings.Split(s.renderDetails(frame, canvas.Width()), "\n"), Cursor: screenlayout.NoSelection()}
	}
	if s.body == nil {
		return build()
	}
	return s.body.details.Block(key, nil, build)
}

func (s Screen) detailsSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{Def: s.detailsSpec(), Body: func(canvas screenlayout.Canvas) screenlayout.Block {
		return s.detailsBlock(frame, canvas)
	}}
}

func (s Screen) subtasksSpec() screenlayout.Spec {
	return screenlayout.BesideFeed(sectionSubtasks, groupLeft, screenkit.DetailColumnFloor, subtasksMinRows, screenlayout.ScrollNone)
}

// subtasksBlock is NOT memoised, and must not be.
//
// Its item is the board, and the board is elastic: renderSubtaskBoard sizes the
// lanes to `canvas.Rows() - framed.Rows`, so the block's height is a
// function of the row budget it was handed. A BlockMemo keyed on anything else
// therefore serves the arranger a board measured against a DIFFERENT budget
// than the one it is being placed in, and the arranger believes the stale
// number: the zone measures short, the surplus is handed to the details pane
// stacked above it, and the board paints two rows inside its own box. That is
// what happened between the first arrange (Open -> syncActivity -> Resync) and
// the paint, and it is the whole of the task_detail view golden's drift.
//
// Keying `rows` into the memo is not the fix either — see P2 in
// .docs/internal/tui-screen-assembly.md. What is memoisable here is the
// row-INDEPENDENT half, the lane cards, and that is already cached by
// s.body.cards through renderSubtaskColumn. The row-dependent assembly is paid
// per render, exactly as commentdetail's and plans' fitted detail bodies pay it.
func (s Screen) subtasksBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	build := func() screenlayout.Block {
		kit := frame.Kit()
		lanes := s.projection.ChildLanes(s.payload.Task.ID)
		label := fmt.Sprintf(frame.Text("tui.task.subtasks_progress_fmt"), strings.ToUpper(frame.Text("tui.row.sub_tasks")), lanes.Completed, lanes.Total)
		header := kit.Styles.Info.Render("// " + label)
		if s.focus == FocusSubtasks {
			header = kit.Styles.HintAccent.Render("▸ " + label)
		}
		boardRows := max(0, canvas.Rows()-framed.Rows)
		board := s.renderSubtaskBoard(frame, lanes.Lanes, canvas.Width()-panel.Borders, boardRows)
		block := framed.Box(kit.Styles.Border, canvas.Width(), header, []string{board})
		block.Cursor = screenlayout.NoSelection()
		return framed.Fill(block, canvas.Rows())
	}
	return build()
}

func (s Screen) subtasksSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{Def: s.subtasksSpec(), Body: func(canvas screenlayout.Canvas) screenlayout.Block {
		return s.subtasksBlock(frame, canvas)
	}}
}

func (s Screen) activitySpec() screenlayout.Spec {
	return screenlayout.Feed(sectionActivity, feedMinRows)
}

func (s Screen) activityBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	kit := frame.Kit()
	header := kit.Styles.KickerCount(frame.Text("tui.kicker.activity"), len(s.payload.Activity))
	if s.focus == FocusActivity {
		header = kit.Styles.HintAccent.Render(fmt.Sprintf("▸ %s · %d", strings.ToUpper(frame.Text("tui.kicker.activity")), len(s.payload.Activity)))
	}
	cursor := canvas.Cursor()
	empty := len(s.payload.Activity) == 0
	var items []string
	if empty {
		items = []string{kit.Styles.Hint.Render(frame.Text("tui.empty.activity")), kit.Styles.Hint.Render(frame.Text("tui.empty.activity_hint"))}
	} else {
		items = s.feedCards(cursor)
	}
	block := framed.Box(kit.Styles.Border, canvas.Width(), header, items)
	wrap := block.Chrome
	if s.mode == ModeComment {
		block.Footer = append([]string{wrap(""), wrap(s.renderCommentInput(frame, canvas.Width()))}, block.Footer...)
	}
	if empty {
		block.Cursor = screenlayout.NoSelection()
	}
	block = framed.Fill(block, canvas.Rows())
	return block
}

func (s Screen) activitySection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{Def: s.activitySpec(), Body: func(canvas screenlayout.Canvas) screenlayout.Block {
		return s.activityBlock(frame, canvas)
	}}
}

// feedCards is the memoized feed, repainted at the cursor the arranger is
// holding. A hit costs two card renders; a miss costs the sixty this screen
// used to pay unconditionally.
func (s Screen) feedCards(cursor int) []string {
	feed := s.currentActivityFeed()
	if feed.focused == cursor {
		return feed.cards
	}
	cards := append([]string(nil), feed.cards...)
	for _, i := range [2]int{feed.focused, cursor} {
		if i >= 0 && i < len(cards) {
			cards[i] = s.cardAt(i, i == cursor)
		}
	}
	return cards
}

// renderDetails paints the task meta block at the width the ARRANGER gave the
// zone. formValueWidth used to be computeLayout's `max(16, left-chrome)`.
func (s Screen) renderDetails(frame screenhost.Frame, width int) string {
	kit := frame.Kit()
	valueWidth := max(16, width-(gridtable.LabelWidth+3))
	task := s.payload.Task
	label := fmt.Sprintf(frame.Text("tui.kicker.task_fmt"), task.ID)
	if trail := s.breadcrumbTrail(); trail != "" {
		label += "  " + trail
	}
	kicker := kit.Styles.Kicker(label)
	if s.focus == FocusDetails {
		kicker = kit.Styles.HintAccent.Render("▸ " + strings.ToUpper(label))
	}
	tags := s.taskTags(task.ID)
	tagNames := make([]string, len(tags))
	for i, tag := range tags {
		tagNames[i] = screenkit.Sanitize(tag.Label)
	}
	blockers := s.projection.Blockers(task.ID)
	detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).Custom(gridtable.Styled(kicker)).
		Row(frame.Text("tui.row.title"), screenkit.Sanitize(task.Title)).
		Row(frame.Text("tui.row.bucket"), screenkit.Sanitize(task.BucketKey)).
		Row(frame.Text("tui.row.priority"), screenkit.Sanitize(s.priorityLabel(task.Priority))).
		Row(frame.Text("tui.row.comments"), fmt.Sprintf("%d", s.projection.Badges(task.ID).Comments)).
		Row(frame.Text("tui.row.tags"), strings.Join(tagNames, " · ")).
		KickerCount(frame.Text("tui.row.blockers"), len(blockers))
	if len(blockers) == 0 {
		detail = detail.Span(gridtable.Styled(kit.Styles.Hint.Render(frame.Text("tui.empty.blockers"))))
	} else {
		for _, blocker := range blockers {
			detail = detail.Span(gridtable.Styled(s.renderTaskReference(kit, blocker)))
		}
	}
	detail = detail.Kicker(frame.Text("tui.kicker.description")).Span(gridtable.Styled(s.renderDescription(frame, task.Description, valueWidth)))
	return detail.View(kit.Styles.Border)
}

func (s Screen) renderDescription(frame screenhost.Frame, description string, width int) string {
	body := strings.TrimSpace(description)
	if body == "" {
		return frame.Kit().Styles.Hint.Render(frame.Text("tui.empty.task_no_description"))
	}
	kit := frame.Kit()
	s.md.Reload(kit.Markdown)
	rendered := markdown.Body(s.md, description, width, true)
	if s.grid.Fullscreen() && s.focus == FocusDetails {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) <= descriptionLineCap {
		return rendered
	}
	return strings.Join(lines[:descriptionLineCap], "\n") + "\n" + kit.Styles.Hint.Render(fmt.Sprintf(frame.Text("tui.task.description_more_fmt"), len(lines)-descriptionLineCap))
}

func (s Screen) activityCards(frame screenhost.Frame) []string {
	cards := make([]string, len(s.payload.Activity))
	for i := range s.payload.Activity {
		cards[i] = s.activityCard(frame, i)
	}
	return cards
}

// cardAt renders one feed card at a stated focus, for the two the cursor move
// flipped. The frame is the screen's own, which is what every other card in the
// memo was rendered against.
func (s Screen) cardAt(i int, focused bool) string {
	event := s.payload.Activity[i]
	var rendered string
	if event.EventType == domain.EventTypeComment {
		rendered = s.renderCommentCard(s.frame, event, focused)
	} else {
		rendered = s.renderSystemEventCard(s.frame, event, focused)
	}
	return rendered
}

// activityCard renders ONE feed card at its payload index. It is a pure
// function of the frame, the event and whether that index is the focused one,
// which is what lets refocused repaint a pair of cards and reuse the rest
// byte-for-byte instead of composing the whole feed again.
func (s Screen) activityCard(frame screenhost.Frame, i int) string {
	event := s.payload.Activity[i]
	focused := s.focus == FocusActivity && i == s.activityCursor()
	if event.EventType == domain.EventTypeComment {
		return s.renderCommentCard(frame, event, focused)
	}
	return s.renderSystemEventCard(frame, event, focused)
}

// activityCardTheme fingerprints everything a feed card paints with beyond its
// own event: the five styles the two card renderers reach for, and the catalog
// strings a card can contain. Kept as rendered ANSI rather than as style
// getters because it is the emitted bytes the memo is claiming are unchanged,
// and a resolved Style is not comparable so there is nothing cheaper to compare.
//
// Charged once per keystroke, against sixty card renders it stands in for.
func (s Screen) activityCardTheme(frame screenhost.Frame) string {
	kit := frame.Kit()
	return strings.Join([]string{
		kit.Styles.HintAccent.Render("."),
		kit.Styles.Hint.Render("."),
		kit.Styles.BadgeInfo.Render("."),
		kit.Styles.CommentCard.Render("."),
		kit.Styles.SystemEventCard.Render("."),
		kit.Styles.Border.Render("."),
		frame.Text("tui.comment.empty"),
		frame.Text("tui.event.task_created"),
		frame.Text("tui.event.task_created_in_fmt"),
		frame.Text("tui.event.task_moved"),
		frame.Text("tui.event.task_moved_to_fmt"),
		frame.Text("tui.event.task_moved_from_to_fmt"),
		frame.Text("tui.event.task_completed"),
		frame.Text("tui.event.task_completed_in_fmt"),
	}, "\x00")
}

func (s Screen) taskCardTheme(frame screenhost.Frame) string {
	kit := frame.Kit()
	painter := card.Painter{Styles: kit.Styles}
	probe := card.Spec{Title: ".", BoxWidth: 8, InnerWidth: 4}
	parts := []string{
		painter.Render(probe),
		painter.Render(card.Spec{Title: ".", Selected: true, Cursor: true, BoxWidth: 8, InnerWidth: 4}),
		painter.Render(card.Spec{Title: ".", Archived: true, BoxWidth: 8, InnerWidth: 4}),
		kit.Styles.Info.Render("."),
		kit.Styles.BadgeHigh.Render("."),
		kit.Styles.BadgeFix.Render("."),
		kit.Styles.BadgeNormal.Render("."),
		kit.Styles.BadgeInfo.Render("."),
		kit.Styles.BadgeBlocker.Render("."),
		kit.Styles.BadgeComment.Render("."),
		kit.Styles.BadgeSubtask.Render("."),
		frame.Text("tui.badge.blocker"),
		frame.Text("tui.badge.blockers"),
		frame.Text("tui.badge.comment"),
		frame.Text("tui.badge.comments"),
		frame.Text("tui.badge.subtask"),
		frame.Text("tui.badge.subtasks"),
	}
	for _, definition := range s.deps.Priorities {
		parts = append(parts, fmt.Sprintf("%d", definition.ID), definition.Color, definition.Value)
	}
	return strings.Join(parts, "\x00")
}

func (s Screen) renderCommentCard(frame screenhost.Frame, event domain.Event, focused bool) string {
	kit := frame.Kit()
	width := s.commentCardWidth(frame)
	tags := make([]string, len(event.Tags))
	for i, tag := range event.Tags {
		tags[i] = screenkit.Sanitize(tag.Label)
	}
	return card.Painter{Styles: kit.Styles}.Comment(card.Comment{
		Author:    event.AuthorType,
		Timestamp: event.CreatedAt,
		Body:      event.Body,
		EmptyText: frame.Text("tui.comment.empty"),
		Tags:      tags,
		MoreFmt:   frame.Text("tui.event.more_lines_fmt"),
		Focused:   focused,
		Width:     width,
	})
}

func (s Screen) renderSystemEventCard(frame screenhost.Frame, event domain.Event, focused bool) string {
	kit := frame.Kit()
	return card.Painter{Styles: kit.Styles}.System(card.System{
		Label:     s.systemEventLabel(frame, event),
		Timestamp: event.CreatedAt,
		Focused:   focused,
		Width:     s.commentCardWidth(frame),
	})
}

func (s Screen) systemEventLabel(frame screenhost.Frame, event domain.Event) string {
	switch event.EventType {
	case domain.EventTypeTaskCreated:
		if bucket := eventPayloadField(event.Payload, "bucket"); bucket != "" {
			return screenkit.Sanitize(fmt.Sprintf(frame.Text("tui.event.task_created_in_fmt"), bucket))
		}
		return frame.Text("tui.event.task_created")
	case domain.EventTypeTaskMoved:
		from, to := eventPayloadField(event.Payload, "from"), eventPayloadField(event.Payload, "to")
		if from != "" && to != "" {
			return screenkit.Sanitize(fmt.Sprintf(frame.Text("tui.event.task_moved_from_to_fmt"), from, to))
		}
		if to != "" {
			return screenkit.Sanitize(fmt.Sprintf(frame.Text("tui.event.task_moved_to_fmt"), to))
		}
		return frame.Text("tui.event.task_moved")
	case domain.EventTypeTaskCompleted:
		if bucket := eventPayloadField(event.Payload, "bucket"); bucket != "" {
			return screenkit.Sanitize(fmt.Sprintf(frame.Text("tui.event.task_completed_in_fmt"), bucket))
		}
		return frame.Text("tui.event.task_completed")
	default:
		return screenkit.Sanitize(event.EventType)
	}
}

func eventPayloadField(payload, key string) string {
	var fields map[string]any
	if json.Unmarshal([]byte(payload), &fields) != nil {
		return ""
	}
	value, _ := fields[key].(string)
	return screenkit.Sanitize(value)
}

// commentCardWidth is the box a feed card renders at. It used to be
// `activityPanelWidth(available) - 6`: a second copy of the feed column's width
// expression plus a hand-counted chrome allowance. The column's width is now
// the arranger's answer; two columns belong to the section frame and two to the
// card's own border.
func (s Screen) commentCardWidth(frame screenhost.Frame) int {
	// Card box inside the activity section. Fullscreen hands the feed the
	// whole body; measuring the side-by-side column there left a strip of
	// empty space and broke the box.
	return s.activityCardWidth(frame)
}

func (s Screen) activityCardWidth(frame screenhost.Frame) int {
	// Card.Width is content+padding with the border OUTSIDE it, and wrap()
	// already spends two columns on the section edges. Size the box to the
	// inner of that inner so `│┌──┐│` fits without TruncateStyled adding "…".
	col := s.feedColumnWidth(frame)
	if s.grid.Fullscreen() && s.focus == FocusActivity {
		col = screenlayout.HostBox(s.bodyKit(frame)).Width
	}
	return max(8, col-panel.Borders-panel.Borders)
}

// feedColumnWidth asks the arranger what the feed column is worth at this
// geometry, without rendering anything. It is the same pass Arrange runs, so
// the width the cards are memoized at is the width they are painted at.
func (s Screen) feedColumnWidth(frame screenhost.Frame) int {
	widths := screenlayout.Widths(s.bodyKit(frame), s.sections(frame)...)
	if len(widths) < 3 {
		return screenkit.FeedFloor
	}
	return widths[2]
}

func (s Screen) renderCommentInput(frame screenhost.Frame, width int) string {
	kit := frame.Kit()
	theme := field.Theme{Border: kit.Styles.FormMultiline, BorderActive: kit.Styles.HintAccent.GetForeground(), Cursor: kit.Styles.Cursor}
	width = clamp(width-8, 24, max(24, width-8))
	lines := []string{
		kit.Styles.Kicker(frame.Text("tui.kicker.new_comment")),
		kit.Styles.Hint.Render(strings.Join([]string{frame.Text("tui.form.hint.enter_saves"), frame.Text("tui.form.hint.alt_newline"), frame.Text("tui.form.hint.esc_cancels")}, " · ")),
		field.RenderArea(s.comment, width, commentInputHeight, true, theme),
	}
	return kit.Panel(strings.Join(lines, "\n"))
}

func (s Screen) renderSubtaskBoard(frame screenhost.Frame, lanes []taskprojection.ChildLane, width, cardRows int) string {
	if len(lanes) == 1 && lanes[0].Bucket.Key == "" {
		lanes[0].Bucket.Name = frame.Text("tui.subtask_board.fallback_lane_name")
	}
	lyt2 := computeSubtaskLayout(len(lanes), width)
	n := len(lanes)
	cap := min(lyt2.capacity, n)
	focusedCol := clamp(s.subtaskCol, 0, n-1)
	start := scrollIntoView(s.subtaskOff, focusedCol, n, cap)
	end := min(n, start+cap)
	showHint := cap < n && cardRows >= panel.Borders+3
	laneRows := cardRows
	if showHint {
		laneRows--
	}
	cells := make([]string, 0, end-start)
	theme := s.taskCardTheme(frame)
	if laneRows >= panel.Borders+2 {
		for i := start; i < end; i++ {
			cells = append(cells, s.renderSubtaskColumn(frame, lanes[i].Bucket, lanes[i].Tasks, s.focus == FocusSubtasks && i == focusedCol, lyt2, laneRows, theme))
		}
	}
	board := cardtable.Row(cells)
	if showHint {
		board += "\n" + frame.Kit().Styles.Hint.Render(fmt.Sprintf(frame.Text("tui.board.lanes_hint_fmt"), start+1, end, n))
	}
	return board
}

func (s Screen) renderSubtaskColumn(frame screenhost.Frame, bucket domain.Bucket, children []domain.Task, focused bool, lyt subtaskLayout, totalRows int, theme string) string {
	kit := frame.Kit()
	headerStyle := kit.Styles.Hint
	if focused {
		headerStyle = kit.Styles.HintAccent
	}
	contentRows := max(0, totalRows-panel.Borders)
	cardRows := max(0, contentRows-2)
	cards := list.NewCards().WithText(kit.Text)
	if cardRows > 0 {
		inputs := s.taskCardInputs(children)
		key := taskCardSetKey{generation: s.payload.Generation, bucket: bucket.Key, boxWidth: lyt.cardWidth, innerWidth: lyt.cardContentWidth, theme: theme}
		build := func() screenlayout.Block {
			items := make([]string, len(children))
			for i, child := range children {
				items[i] = s.renderTaskCard(kit, child, false, lyt.cardWidth, lyt.cardContentWidth)
			}
			return screenlayout.Block{Items: items}
		}
		var base screenlayout.Block
		if s.body == nil {
			base = build()
		} else {
			base = s.body.cards.BlockMany(key, inputs, build)
		}
		items := s.subtaskItems(kit, children, base, focused, lyt)
		cards = cards.WithItems(items).WithViewport(cardRows)
		if focused {
			cards = s.subtasks.WithItems(items).WithViewport(cardRows).WithCursor(s.subtasks.Cursor()).WithText(kit.Text)
		}
	}
	emptyText := ""
	if cardRows > 0 {
		emptyText = frame.Text("tui.board.empty")
	}
	header := headerStyle.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(screenkit.Sanitize(bucket.Name)), len(children)))
	body := ""
	if cards.Len() == 0 {
		if emptyText != "" {
			body = kit.Styles.Empty.Width(lyt.columnInner).Render(emptyText)
		}
	} else {
		body = cards.View(kit.Styles.Hint)
	}
	return framedLane(kit, header, body, lyt.columnInner, totalRows)
}

func (s Screen) subtaskItems(kit screenkit.Kit, children []domain.Task, base screenlayout.Block, focused bool, lyt subtaskLayout) []list.Item {
	items := make([]list.Item, len(children))
	for i, child := range children {
		card := base.Items[i]
		if focused && i == s.subtasks.Cursor() {
			card = s.renderTaskCard(kit, child, true, lyt.cardWidth, lyt.cardContentWidth)
		}
		items[i] = list.Item{Content: card, Height: strings.Count(card, "\n") + 1}
	}
	return items
}

func (s Screen) taskCardInputs(children []domain.Task) []string {
	buf := make([]byte, 0, len(children)*48)
	for _, child := range children {
		buf = strconv.AppendInt(buf, child.ID, 10)
		buf = append(buf, 0)
		buf = append(buf, child.Title...)
		buf = append(buf, 0)
		buf = append(buf, child.BucketKey...)
		buf = append(buf, 0)
		buf = append(buf, child.State...)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, int64(child.Priority), 10)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, int64(s.projection.Badges(child.ID).Blockers), 10)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, int64(s.projection.Badges(child.ID).Comments), 10)
		buf = append(buf, 0)
		buf = strconv.AppendInt(buf, int64(s.projection.Badges(child.ID).Subtasks), 10)
		buf = append(buf, 0)
	}
	return []string{string(buf)}
}

// renderTaskCard is the sub-task lane's card. It was a second writing of the
// board's card and is now the same one — including the hard wrap its own copy
// of the word wrapper did not do, so a sub-task titled with one long token no
// longer paints through the lane edge.
func (s Screen) renderTaskCard(kit screenkit.Kit, task domain.Task, selected bool, boxWidth, innerWidth int) string {
	return card.Painter{Styles: kit.Styles}.Render(card.Spec{
		ID: task.ID, Title: screenkit.Sanitize(task.Title),
		Badges:   s.taskBadges(kit, task),
		Selected: selected, Cursor: selected,
		Archived: task.State == domain.TaskStateArchived,
		BoxWidth: boxWidth, InnerWidth: innerWidth,
	})
}

// framedLane is a sub-task lane in the same box-drawing as the task grid:
// Border-coloured edges and a kicker rule that joins the sides (`├─┤`),
// rather than a lipgloss column wrapping an inner HRule (`│───│`).
//
// The shape is exactly framed.Box plus framed.Fill: one kicker (header), a
// body windowed to whatever rows are left after the chrome, and the
// shortfall (a body shorter than its slots) blank-padded to the total. The
// slots are computed up front, rather than left to Fill's own row-vs-used
// count, only so laneBodyWindow can choose which lines the ▲/▼ hints keep
// BEFORE they are handed to Box as items — Fill only ever pads a shortfall,
// it does not window an overflow.
func framedLane(kit screenkit.Kit, header, body string, inner, totalRows int) string {
	inner = max(1, inner)
	var bodyLines []string
	if strings.TrimSpace(body) != "" {
		bodyLines = strings.Split(body, "\n")
	}
	bodySlots := max(0, totalRows-framed.Rows)
	items := laneBodyWindow(bodyLines, bodySlots)
	if len(items) > bodySlots {
		items = items[:bodySlots]
	}
	block := framed.Fill(framed.Box(kit.Styles.Border, inner+panel.Borders, header, items), totalRows)
	rows := append(append(append([]string{}, block.Header...), block.Items...), block.Footer...)
	return strings.Join(rows, "\n")
}

// laneBodyWindow keeps the card-list ▲/▼ hints when Cards.View paints more
// rows than the lane box. Taking the first N rows dropped "▼ N below" off the
// bottom, so the below count only appeared as "▲ N above" after a scroll.
func laneBodyWindow(lines []string, slots int) []string {
	if slots <= 0 || len(lines) <= slots {
		return lines
	}
	keepHead, keepTail := 0, 0
	if strings.Contains(lines[0], "▲") {
		keepHead = 1
	}
	if strings.Contains(lines[len(lines)-1], "▼") {
		keepTail = 1
	}
	if keepHead+keepTail >= slots {
		out := append([]string{}, lines[:keepHead]...)
		if keepTail > 0 {
			out = append(out, lines[len(lines)-1])
		}
		if len(out) > slots {
			return out[:slots]
		}
		return out
	}
	out := append([]string{}, lines[:keepHead]...)
	out = append(out, lines[keepHead:keepHead+slots-keepHead-keepTail]...)
	if keepTail > 0 {
		out = append(out, lines[len(lines)-1])
	}
	return out
}

func (s Screen) renderBlockers(frame screenhost.Frame) string {
	kit := frame.Kit()
	width := frame.Width()
	if width <= 0 {
		width = screenlayout.HostBox(kit).Width
	}
	view := screengrid.Render(kit, s.grid, screenlayout.Box{Width: width, Rows: screenlayout.HostBox(kit).Rows}, s.blockerRoot(frame)).View
	return screenkit.Indent("\n"+view, 2)
}

func (s Screen) blockersSection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionBlockers, MinRows: 1, Scroll: screenlayout.ScrollItems, SelectFirst: true},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			kit := frame.Kit()
			rows := s.blockerRows(kit, s.blockerCandidates(), canvas.Cursor())
			if len(rows) == 0 {
				rows = []string{kit.Styles.Hint.Render(frame.Text("tui.empty.blocker_picker"))}
			}
			style := kit.Styles.Border
			contentWidth := max(0, canvas.Width()-8)
			innerWidth := max(0, contentWidth+4)
			// The picker indents its content two columns inside the box, which
			// framed.Box has no knob for — every other framed zone pads a row to
			// the FULL inner width. That inset is this screen's content layout,
			// not chrome, so it is built into the row BEFORE the row reaches the
			// border wrap: panel.WrapLine still owns the │ edges and the pad-to-
			// width, it is just handed a row that is already inner-width wide.
			inset := func(line string) string {
				return "  " + screenkit.PadRight(screenkit.TruncateStyled(line, contentWidth), contentWidth) + "  "
			}
			edgeWrap := panel.WrapLine(style, innerWidth)
			wrap := func(line string) string { return edgeWrap(inset(line)) }
			header := []string{panel.Top(style, innerWidth)}
			for _, line := range s.blockerHeader(frame, canvas.Width()) {
				header = append(header, wrap(line))
			}
			items := make([]string, len(rows))
			for i, row := range rows {
				items[i] = wrap(row)
			}
			return screenlayout.Block{
				Header: header,
				Items:  items,
				Footer: []string{panel.Bottom(style, innerWidth)},
				Chrome: wrap,
			}
		},
	}
}

func (s Screen) blockerRoot(frame screenhost.Frame) screengrid.Node {
	section := s.blockersSection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

// blockerRows is one row per blocker candidate: cursor marker, checkbox, id,
// title and the `bucket · priority` tail.
//
// Nothing here is painted per row. The two checkbox glyphs are the same two
// strings for the whole picker, so they are painted once above the walk instead
// of once — sometimes twice — per candidate. The meta tails are a column in one
// tone, so they are painted as a column. The rows carry the bytes the per-row
// paint produced.
func (s Screen) blockerRows(kit screenkit.Kit, candidates []domain.Task, cursor int) []string {
	if len(candidates) == 0 {
		return nil
	}
	metas := make([]string, len(candidates))
	for i, candidate := range candidates {
		metas[i] = fmt.Sprintf("%s · %s", screenkit.Sanitize(candidate.BucketKey), screenkit.Sanitize(s.priorityLabel(candidate.Priority)))
	}
	metas = screenkit.PaintColumn(kit.Styles.Hint, metas)
	unchecked := kit.Styles.Hint.Render("[ ]")
	checked := kit.Styles.HintAccent.Render("[x]")
	rows := make([]string, len(candidates))
	for i, candidate := range candidates {
		check := unchecked
		if s.checks[candidate.ID] {
			check = checked
		}
		rows[i] = fmt.Sprintf("%s %s #%d %s  %s",
			kit.CursorMarker(cursor == i), check, candidate.ID, screenkit.Sanitize(candidate.Title), metas[i])
	}
	return rows
}

// blockerHeader is the chrome above the blocker picker's scrolled candidates:
// the kicker, the hint, a blank, the task meta row, another blank and the rule.
func (s Screen) blockerHeader(frame screenhost.Frame, width int) []string {
	kit := frame.Kit()
	task := s.payload.Task
	return []string{
		kit.Styles.Kicker(fmt.Sprintf(frame.Text("tui.kicker.blockers_fmt"), task.ID)),
		kit.Styles.Hint.Render(frame.Text("tui.picker.hint.blockers")),
		"",
		metaRow(kit, frame.Text("tui.row.task"), task.Title),
		"",
		kit.HRule(width - 8),
	}
}

func (s Screen) renderMoveInput(frame screenhost.Frame) string {
	kit := frame.Kit()
	task, ok := s.taskByID(s.moveTaskID)
	prompt := frame.Text("tui.input.target_bucket_key")
	if ok && s.deps.MovePrompt != nil {
		prompt = s.deps.MovePrompt(task)
	}
	prompt = screenkit.Sanitize(prompt)
	// The persistent model is sized in resize(), which cannot know how wide this
	// label will be — the prompt comes from a host callback over the task being
	// moved, so it is only knowable here. field.RenderLine sizes a render copy against
	// the measured label, which is what keeps the row inside the terminal at
	// every geometry; see moveInputWidth.
	return field.RenderLine(kit.Styles, prompt, s.move, s.moveInputWidth(kit, prompt+": "))
}

// minMoveInputWidth keeps a bucket key visible on a terminal too narrow to hold
// the prompt and the input side by side. Below this the row is allowed to be
// the one thing that overflows, because a zero-width input cannot be typed in.
const minMoveInputWidth = 8

// moveInputWidth sizes the bucket-move text input so the whole prompt row fits
// the terminal.
//
// The row was sized by `s.width - 12` in resize(), a constant that charged for
// none of the four things actually between the input and the terminal edge: the
// 2-cell indent this row is rendered at, the input box's border and padding,
// the prompt label, and the caret cell bubbles paints past the value window.
// The result overflowed by a fixed 19 columns at EVERY geometry — 99 cells at
// 80 columns, 139 at 120, 219 at 200 — because the constant was independent of
// all of them.
//
// Every term is now measured from the thing that renders it: the box budget
// from the kit, the frame from the live Input style, the label from the label.
// moveInputWidth is the typing room in the bucket-move prompt.
//
// The subtraction — the style's measured frame plus the cell bubbles paints past
// the value for its cursor — moved into components/field, which is where the
// row is drawn. This screen had it right and the root did not; consolidating
// keeps the one that was measured rather than the one that was assumed.
//
// kit.BoxWidth() is already net of the body indent, so the budget passed here is
// the room the row actually has.
func (s Screen) moveInputWidth(kit screenkit.Kit, label string) int {
	return field.Width(kit.Styles, kit.BoxWidth()+field.Indent, screenkit.VisibleWidth(label), minMoveInputWidth)
}

func (s Screen) taskByID(id int64) (domain.Task, bool) {
	return s.projection.TaskByID(id)
}

// breadcrumbTrail is plain ancestor copy. Style it with the kicker, never
// before: Hint.Render then strings.ToUpper corrupts the SGR terminator (`m`→`M`)
// and the nested-task header paints empty.
func (s Screen) breadcrumbTrail() string {
	ids := s.projection.Ancestors(s.payload.Task.ID)
	if len(ids) == 0 {
		return ""
	}
	prefix := ""
	if len(ids) > 3 {
		prefix, ids = "… ", ids[:3]
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	return prefix + "← " + strings.Join(parts, " ← ")
}

func (s Screen) renderTaskReference(kit screenkit.Kit, task domain.Task) string {
	meta := kit.Styles.Hint.Render(fmt.Sprintf("%s · %s", screenkit.Sanitize(task.BucketKey), screenkit.Sanitize(s.priorityLabel(task.Priority))))
	return kit.Styles.HintAccent.Render(fmt.Sprintf("#%d", task.ID)) + " " + screenkit.Sanitize(task.Title) + "  " + meta
}

func (s Screen) priorityLabel(priority domain.Priority) string {
	return s.projection.PriorityLabel(priority)
}

// taskBadges is the sub-task card's badge line: the priority pill, then the
// blocker, comment and sub-task counts — the same order the board reads.
func (s Screen) taskBadges(kit screenkit.Kit, task domain.Task) []string {
	badges := make([]string, 0, 4)
	if def, ok := s.projection.Priority(task.Priority); ok {
		if pill := tokenstrip.Pill(kit.Styles, def.Color, def.Value); pill != "" {
			badges = append(badges, pill)
		}
	}
	counts := s.projection.Badges(task.ID)
	return append(badges, tokenstrip.Counts{
		Blockers: counts.Blockers,
		Comments: counts.Comments,
		Subtasks: counts.Subtasks,
	}.Render(kit.Styles, kit.T)...)
}

func (s Screen) taskTags(taskID int64) []domain.Tag {
	if s.deps.TaskTags == nil {
		return nil
	}
	return s.deps.TaskTags(taskID)
}

func computeSubtaskLayout(n, available int) subtaskLayout {
	if n <= 0 {
		n = 1
	}
	available = max(20, available)
	capacity := max(1, min(n, (available+1)/27))
	columnInner := max(18, (available-(capacity-1))/capacity-2)
	return subtaskLayout{columnInner: columnInner, cardWidth: columnInner - 2, cardContentWidth: columnInner - 4, capacity: capacity}
}

func metaRow(kit screenkit.Kit, label, value string) string {
	rendered := "// " + strings.ToUpper(label)
	return kit.Styles.Info.Render(rendered) + strings.Repeat(" ", max(1, 14-screenkit.VisibleWidth(rendered))) + screenkit.Sanitize(value)
}

func scrollIntoView(start, focused, total, capacity int) int {
	if capacity >= total {
		return 0
	}
	if focused < start {
		start = focused
	} else if focused >= start+capacity {
		start = focused - capacity + 1
	}
	return clamp(start, 0, total-capacity)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
