package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
)

func (m *Model) handleBoardKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "enter":
		if task, ok := m.selectedTask(); ok {
			m.openTaskView(task)
		}
	case "m":
		if _, ok := m.selectedTask(); ok {
			m.moveMode = !m.moveMode
			if m.moveMode {
				m.status = m.t("tui.status.move_mode_active")
			} else {
				m.status = m.t("tui.status.move_cancelled")
			}
		}
	case "left", "h":
		if m.moveMode {
			m.moveSelectedToColumn(m.colIdx - 1)
			return
		}
		// Plain navigation wraps the same way cycleEntityKind does on the
		// config view: stepping past the first lane lands on the last.
		// moveMode keeps its bounded behavior so dragging a task off the
		// edge stays an explicit no-op.
		if n := len(m.workflow.Buckets); n > 0 {
			m.colIdx = (m.colIdx - 1 + n) % n
			m.clampCardIdx()
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
			m.syncBoardColScroll()
		}
	case "right", "l":
		if m.moveMode {
			m.moveSelectedToColumn(m.colIdx + 1)
			return
		}
		if n := len(m.workflow.Buckets); n > 0 {
			m.colIdx = (m.colIdx + 1) % n
			m.clampCardIdx()
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
			m.syncBoardColScroll()
		}
	case "up", "k":
		if m.cardIdx > 0 {
			m.cardIdx--
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	case "down", "j":
		bucketTasks := m.tasksInCurrentBucket()
		if m.cardIdx < len(bucketTasks)-1 {
			m.cardIdx++
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	case "pgup", "ctrl+u":
		m.cardIdx -= boardScrollPageStep(m)
		if m.cardIdx < 0 {
			m.cardIdx = 0
		}
		m.syncSelectedFromBoard()
		m.syncFocusedColumnScroll()
	case "pgdown", "ctrl+d":
		bucketTasks := m.tasksInCurrentBucket()
		m.cardIdx += boardScrollPageStep(m)
		if m.cardIdx > len(bucketTasks)-1 {
			m.cardIdx = len(bucketTasks) - 1
		}
		if m.cardIdx < 0 {
			m.cardIdx = 0
		}
		m.syncSelectedFromBoard()
		m.syncFocusedColumnScroll()
	case "home", "g":
		m.cardIdx = 0
		m.syncSelectedFromBoard()
		m.syncFocusedColumnScroll()
	case "end", "G":
		bucketTasks := m.tasksInCurrentBucket()
		if len(bucketTasks) > 0 {
			m.cardIdx = len(bucketTasks) - 1
			m.syncSelectedFromBoard()
			m.syncFocusedColumnScroll()
		}
	}
}

// syncFocusedColumnScroll keeps m.boardScroll[focusedBucket] aligned so the
// selected card stays fully visible inside the column viewport. Rendered card
// heights vary (1- vs 2-line titles, badges line) so we render each card to
// measure the actual height instead of using an approximation, otherwise
// `down` arrow lags behind the cursor by ~1 card.
func (m *Model) syncFocusedColumnScroll() {
	bucket, ok := m.focusedBucketKey()
	if !ok {
		return
	}
	viewport := m.boardViewportRows()
	if viewport <= 0 {
		return
	}
	tasks := m.tasksInCurrentBucket()
	if len(tasks) == 0 {
		if m.boardScroll != nil {
			delete(m.boardScroll, bucket)
		}
		return
	}

	layout := m.computeBoardLayout(len(m.workflow.Buckets))
	heights := make([]int, len(tasks))
	for i, task := range tasks {
		rendered := m.renderCard(task, false, layout)
		heights[i] = strings.Count(rendered, "\n") + 1
	}

	if m.boardScroll == nil {
		m.boardScroll = map[string]int{}
	}
	m.boardScroll[bucket] = followScrollWindowSplit(m.boardScroll[bucket], m.cardIdx, heights, viewport)
}

func (m Model) focusedBucketKey() (string, bool) {
	if len(m.workflow.Buckets) == 0 || m.colIdx < 0 || m.colIdx >= len(m.workflow.Buckets) {
		return "", false
	}
	return m.workflow.Buckets[m.colIdx].Key, true
}

func boardScrollPageStep(m *Model) int {
	step := m.boardViewportRows() / 8 // each card is ~4 rows; half-page ≈ rows/8 cards
	if step < 2 {
		return 2
	}
	return step
}

// boardLayout holds the per-render geometry for the kanban board so columns
// and cards grow with the available terminal width instead of being pinned
// to fixed constants.
type boardLayout struct {
	columnInner      int // kanban column inner content width (passed to Width())
	cardWidth        int // card.Width() — content width of each card box
	cardContentWidth int // text area inside a card (cardWidth - 2 padding)
	cardHeight       int // rendered on-screen height of a single card (incl. borders)
	viewportRows     int // rows available inside a column for cards (after header+sep)
}

func (m Model) computeBoardLayout(n int) boardLayout {
	const (
		minColumnInner = 28
		maxColumnInner = 44
	)
	available := m.availableWidth()
	colOnScreen := minColumnInner + 2
	if n > 0 {
		colOnScreen = (available - (n - 1)) / n
	}
	columnInner := colOnScreen - 2
	if columnInner < minColumnInner {
		columnInner = minColumnInner
	}
	if columnInner > maxColumnInner {
		columnInner = maxColumnInner
	}
	// The card style has its own border (+2 cols) and Padding(0,1) which adds
	// 2 cols inside the Width() box, so:
	//   on-screen card width = card.Width() + 2 (border)
	//   card text width      = card.Width() - 2 (padding)
	// To make the card fit exactly inside the column's inner area we set
	// cardWidth = columnInner - 2.
	cardWidth := columnInner - 2
	cardContent := cardWidth - 2

	return boardLayout{
		columnInner:      columnInner,
		cardWidth:        cardWidth,
		cardContentWidth: cardContent,
		cardHeight:       4,
		viewportRows:     m.boardViewportRows(),
	}
}

// boardViewportRows is the number of terminal rows the kanban columns
// can use for cards (after each lane's header + separator and the
// surrounding screen chrome). Sources its chrome from the shared
// `panelViewportRows` helper so the budget tracks live header / status
// / nav strip changes — the prior hard-coded `chrome := 9` undercounted
// the screen header and let tall lanes spill below the terminal.
// Per-lane chrome = 2 borders + 2 header rows (kicker / separator) = 4.
func (m Model) boardViewportRows() int {
	return m.panelViewportRows(4)
}

// boardColumnCapacity returns how many board columns fit side-by-side at the
// current width using the same column-inner sizing as the full layout.
// Returns 1 even on very narrow terminals (one column always renders).
func (m Model) boardColumnCapacity(layout boardLayout) int {
	if layout.columnInner <= 0 {
		return 1
	}
	available := m.availableWidth()
	per := layout.columnInner + 2 // +2 for the border on either side
	if per <= 0 {
		return 1
	}
	// First column doesn't need a leading gap; each additional column adds 1.
	cap := (available + 1) / (per + 1)
	if cap < 1 {
		cap = 1
	}
	return cap
}

// scrollIntoView slides start so that focused stays in the [start, start+cap)
// window. Persistent — callers store the returned value so tabbing keeps the
// previous scroll position when the focused column already fits in view.
func scrollIntoView(start, focused, total, cap int) int {
	if cap >= total {
		return 0
	}
	if focused < start {
		start = focused
	}
	if focused >= start+cap {
		start = focused - cap + 1
	}
	if start < 0 {
		start = 0
	}
	if start > total-cap {
		start = total - cap
	}
	return start
}

// syncBoardColScroll keeps boardColScroll aligned so the focused bucket stays
// inside the currently-visible horizontal window.
func (m *Model) syncBoardColScroll() {
	n := len(m.workflow.Buckets)
	if n == 0 {
		m.boardColScroll = 0
		return
	}
	layout := m.computeBoardLayout(n)
	cap := m.boardColumnCapacity(layout)
	focused := clampInt(m.colIdx, 0, n-1)
	m.boardColScroll = scrollIntoView(m.boardColScroll, focused, n, cap)
}

func (m Model) renderBoard() string {
	if len(m.workflow.Buckets) == 0 {
		return m.renderPanel(m.t("tui.empty.board_no_buckets"))
	}

	tasksByBucket := m.tasksByBucket()
	totalTasks := 0
	for _, bucket := range m.workflow.Buckets {
		totalTasks += len(tasksByBucket[bucket.Key])
	}

	n := len(m.workflow.Buckets)
	layout := m.computeBoardLayout(n)
	// Lanes are content-sized: short columns close their bottom border at
	// the last card, tall columns hit the internal viewport scroll (which
	// already caps height to layout.viewportRows). Forcing a fixed Height
	// here pads short lanes with empty rows AND can overshoot the screen
	// when the chrome estimate undercounts — both regressions the user
	// flagged. The natural sizing matches "ajusta a quantidade de cards
	// dentro dela e não passa do tamanho limite da tela".
	columnStyle := m.styles.kanbanColumnSized(layout.columnInner, 0)
	emptyStyle := m.styles.empty.Width(layout.columnInner)

	cap := m.boardColumnCapacity(layout)
	if cap > n {
		cap = n
	}
	start := scrollIntoView(m.boardColScroll, clampInt(m.colIdx, 0, n-1), n, cap)
	end := start + cap
	if end > n {
		end = n
	}

	cells := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		bucket := m.workflow.Buckets[i]
		bucketTasks := tasksByBucket[bucket.Key]
		selectedIdx := -1
		if i == m.colIdx {
			selectedIdx = m.cardIdx
		}
		cellContent := m.renderKanbanCell(bucket, bucketTasks, i == m.colIdx, selectedIdx, layout, emptyStyle)
		cells = append(cells, columnStyle.Render(cellContent))
	}

	var parts []string
	for i, cell := range cells {
		parts = append(parts, cell)
		if i < len(cells)-1 {
			parts = append(parts, " ")
		}
	}
	board := lipgloss.JoinHorizontal(lipgloss.Top, parts...)

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(indentBlock(board, 2))
	if cap < n {
		// Surface a hint listing the off-screen lanes so the user knows
		// left/right keeps scrolling beyond the visible window.
		hint := fmt.Sprintf(m.t("tui.board.lanes_hint_fmt"), start+1, end, n)
		sb.WriteString("\n  " + m.styles.hint.Render(hint))
	}
	if totalTasks == 0 {
		sb.WriteString("\n\n")
		sb.WriteString(indentBlock(m.renderEmptyBoardHint(), 2))
	}
	return sb.String()
}

func (m Model) renderKanbanCell(bucket domain.Bucket, tasks []domain.Task, focused bool, selectedIdx int, layout boardLayout, emptyStyle lipgloss.Style) string {
	headerStyle := m.styles.hintAccent
	if !focused {
		headerStyle = m.styles.muted
	}
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(bucket.Name), len(tasks))
	lines := []string{
		headerStyle.Render(headerText),
		m.hRule(layout.columnInner),
	}

	if len(tasks) == 0 {
		lines = append(lines, emptyStyle.Render(m.t("tui.board.empty")))
		return strings.Join(lines, "\n")
	}

	// Render every card first so we know the real rendered height of each one.
	rendered := make([]string, len(tasks))
	heights := make([]int, len(tasks))
	for i, task := range tasks {
		rendered[i] = m.renderCard(task, focused && i == selectedIdx, layout)
		heights[i] = strings.Count(rendered[i], "\n") + 1
	}

	viewport := layout.viewportRows
	if viewport <= 0 {
		// Height unknown — render everything; the terminal will scroll natively.
		lines = append(lines, rendered...)
		return strings.Join(lines, "\n")
	}

	offset := m.boardScroll[bucket.Key]
	lines = append(lines, m.renderScrollWindowSplit(rendered, heights, offset, viewport)...)
	return strings.Join(lines, "\n")
}

func (m Model) renderCard(task domain.Task, selected bool, layout boardLayout) string {
	prefix := fmt.Sprintf("#%d ", task.ID)
	prefixWidth := lipgloss.Width(prefix)

	firstWidth := layout.cardContentWidth - prefixWidth
	restWidth := layout.cardContentWidth - prefixWidth
	if firstWidth < 1 {
		firstWidth = 1
	}
	if restWidth < 1 {
		restWidth = 1
	}

	wrapped := wrapWords(task.Title, firstWidth, restWidth)
	lines := make([]string, 0, len(wrapped)+1)
	for i, part := range wrapped {
		if i == 0 {
			lines = append(lines, prefix+part)
		} else {
			lines = append(lines, strings.Repeat(" ", prefixWidth)+part)
		}
	}

	if badgeLine := m.renderTaskBadges(task, layout.cardContentWidth); badgeLine != "" {
		lines = append(lines, badgeLine)
	}

	style := m.styles.card.Width(layout.cardWidth)
	if selected {
		style = m.styles.cardSelected.Width(layout.cardWidth)
	}
	if task.State == domain.TaskStateArchived {
		style = m.styles.archivedCard.Width(layout.cardWidth)
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderTaskBadges builds a line of colored badges for a task: priority,
// blocker count, and comment count. Each badge is rendered as a filled pill
// using Lipgloss background colors. wrapBadges breaks badges onto a new line
// whenever the next would overflow maxWidth so every badge stays visible.
func (m Model) renderTaskBadges(task domain.Task, maxWidth int) string {
	var badges []string

	if badge := m.priorityBadge(task.Priority); badge != "" {
		badges = append(badges, badge)
	}
	if deps := m.dependencyCount(task.ID); deps > 0 {
		badges = append(badges, m.styles.badgeBlocker.Render(fmt.Sprintf("%d %s", deps, plural(deps, m.t("tui.badge.blocker"), m.t("tui.badge.blockers")))))
	}
	if cmts := m.commentCount(task.ID); cmts > 0 {
		badges = append(badges, m.styles.badgeComment.Render(fmt.Sprintf("%d %s", cmts, plural(cmts, m.t("tui.badge.comment"), m.t("tui.badge.comments")))))
	}

	return wrapBadges(badges, maxWidth)
}

func (m Model) renderEmptyBoardHint() string {
	lines := []string{
		m.styles.hintAccent.Render(m.t("tui.empty.board_no_tasks")),
		"",
		m.styles.hint.Render(m.t("tui.board.empty_press_n")) + m.styles.hintAccent.Render("n") + m.styles.hint.Render(m.t("tui.board.empty_to_create")),
		m.styles.hint.Render(m.t("tui.board.empty_cli_example")),
		"",
		m.styles.hintAccent.Render("m") + m.styles.hint.Render(m.t("tui.board.empty_inline_move")) + m.styles.hintAccent.Render("enter") + m.styles.hint.Render(m.t("tui.board.empty_inline_open")) + m.styles.hintAccent.Render("c") + m.styles.hint.Render(m.t("tui.board.empty_inline_comment")),
	}
	return m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join(lines, "\n"))
}
