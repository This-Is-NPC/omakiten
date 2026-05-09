package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/picker"
)

// loadHome (re)loads the cross-project data the Home view renders: every
// non-archived project, the tags attached to each project, and the count of
// tasks not yet in a final bucket. Failures from the per-project enrichment
// are degraded silently so the picker still renders even if a tag/task query
// fails for one project — the main goal of Home is letting the user pick a
// project, not surfacing perfect metadata.
func (m *Model) loadHome() error {
	if m.repos.Projects == nil {
		m.homeProjects = nil
		m.homeProjectTags = nil
		m.homeProjectPending = nil
		return nil
	}
	projects, err := m.repos.Projects.ListProjects(m.ctx)
	if err != nil {
		return err
	}
	m.homeProjects = projects

	tags := make(map[int64][]domain.Tag, len(projects))
	pending := make(map[int64]int, len(projects))
	for _, p := range projects {
		if m.repos.Tags != nil {
			if list, terr := m.repos.Tags.ListProjectTags(m.ctx, p.ID); terr == nil {
				tags[p.ID] = list
			}
		}
		if m.repos.Tasks != nil {
			if count, cerr := m.countPendingTasks(p.ID); cerr == nil {
				pending[p.ID] = count
			}
		}
	}
	m.homeProjectTags = tags
	m.homeProjectPending = pending

	if m.homePicker.Cursor >= len(projects) {
		if len(projects) == 0 {
			m.homePicker.Cursor = 0
			m.homePicker.Scroll = 0
		} else {
			m.homePicker.Cursor = len(projects) - 1
		}
	}
	m.syncHomeScroll()
	return nil
}

// countPendingTasks returns the number of tasks for a project that are NOT
// in the workflow's final bucket. Falls back to TaskCount when the workflow
// is unavailable so the badge still surfaces "open work" in some form.
func (m *Model) countPendingTasks(projectID int64) (int, error) {
	tasks, err := m.repos.Tasks.ListTasks(m.ctx, projectID, domain.TaskFilter{})
	if err != nil {
		return 0, err
	}
	if m.repos.Config == nil {
		return len(tasks), nil
	}
	wf, err := m.repos.Config.ActiveWorkflow(m.ctx)
	if err != nil || len(wf.Buckets) == 0 {
		return len(tasks), nil
	}
	final := wf.Buckets[len(wf.Buckets)-1].Key
	count := 0
	for _, t := range tasks {
		if t.BucketKey != final {
			count++
		}
	}
	return count, nil
}

// handleHomeKey routes keypresses while the multi-project Home view is
// active. ctrl+h is intercepted here before delegating to the picker so it
// works as a "refresh" action while on Home (the view-switch interpretation
// in handleCommonKey only fires on per-project views, which Home isn't).
// Navigation is delegated to the picker component; enter on a highlighted
// card selects a project and switches the model to its Board.
func (m *Model) handleHomeKey(msg tea.KeyMsg) {
	if msg.String() == "ctrl+h" {
		if err := m.loadHome(); err != nil {
			m.status = err.Error()
		} else {
			m.status = "Refreshed"
		}
		return
	}
	rowCount := len(m.homeProjects)
	// Picker uses viewport for pgup/pgdn step + cursor scroll bounds in
	// CARDS, not rows; project cards average ~5 terminal rows so divide
	// the row budget down. The picker's Scroll is then overwritten by
	// syncHomeScroll which does the proper height-aware budgeting via
	// the shared scrollwindow helpers.
	const avgCardLines = 5
	cardsViewport := m.homeViewportRows() / avgCardLines
	if cardsViewport < 1 {
		cardsViewport = 1
	}
	updated, _ := m.homePicker.Update(msg, rowCount, cardsViewport)
	m.homePicker = updated
	m.syncHomeScroll()

	switch m.homePicker.LastEvent() {
	case picker.EventSelect:
		if rowCount == 0 {
			return
		}
		project := m.homeProjects[m.homePicker.Cursor]
		if err := m.selectHomeProject(project); err != nil {
			m.status = err.Error()
		}
	case picker.EventCancel:
		// Esc on Home is a no-op — quitting requires explicit q/ctrl+c so
		// the user does not accidentally drop out of the TUI.
	}
}

// selectHomeProject swaps the active project context, reloads the
// per-project read-model, and restores the user's last (top, sub) for
// the per-project surface. The (top, sub) is preserved across project
// hops in T3 so power users who use Home as a project switcher keep
// their working zone — picking a different project should not eject
// them from `Stats › Logs` back to the board. View-specific cursors
// (board / table / graph / logs) still reset because they reference
// project-scoped data the new bundle does not share.
//
// The home sentinel is replaced with the canonical Tasks › Board only
// when the session was started in Home (i.e. the previous top was
// `topHome`) — without that, "the user picked their first project"
// would land on an undefined sub.
func (m *Model) selectHomeProject(project domain.Project) error {
	m.project = project.Context()
	m.lastProjectRoot = project.RootPath
	if m.top == topHome {
		m.top = topTasks
		m.sub = subBoard
	}
	m.syncEntityKindFromSub()
	m.colIdx = 0
	m.cardIdx = 0
	m.boardColScroll = 0
	m.boardScroll = nil
	m.selected = 0
	m.tableScroll = 0
	m.graphScroll = 0
	m.graphCursor = 0
	m.logsScroll = 0
	m.logsSelected = 0
	m.status = ""
	return m.refresh()
}

// homeViewportRows is the terminal-row budget for the project picker
// panel — used by both the renderer and the height-aware scroll sync
// so card heights drive the slice. Sources its chrome from the shared
// panelViewportRows helper (panel chrome = 2 borders + 2 header rows).
func (m Model) homeViewportRows() int {
	return m.panelViewportRows(4)
}

// homeCardSizes returns the card width and inner content width used
// when rendering project cards on the Home picker. Extracted so the
// renderer and the sync routine measure heights against the same
// geometry — the prior inline version lived in renderHome only and
// the count-based scroll bypassed the question entirely.
func (m Model) homeCardSizes() (cardWidth, cardContent int) {
	available := m.availableWidth()
	columnInner := available - 2
	if columnInner > homeColumnInnerMax {
		columnInner = homeColumnInnerMax
	}
	if columnInner < homeColumnInnerMin {
		columnInner = homeColumnInnerMin
	}
	cardWidth = columnInner - 2
	cardContent = cardWidth - 2
	if cardContent < 16 {
		cardContent = 16
	}
	return cardWidth, cardContent
}

// syncHomeScroll keeps m.homePicker.Scroll aligned so the cursor
// project card stays inside the viewport regardless of multi-line
// titles or badge rows. Project cards have variable heights so the
// scroll can't be counted in items — it's measured in rendered
// terminal rows via the shared scrollwindow helpers.
func (m *Model) syncHomeScroll() {
	if len(m.homeProjects) == 0 {
		m.homePicker.Scroll = 0
		return
	}
	cardWidth, cardContent := m.homeCardSizes()
	heights := make([]int, len(m.homeProjects))
	for i := range m.homeProjects {
		rendered := m.renderProjectCard(m.homeProjects[i], false, cardWidth, cardContent)
		heights[i] = strings.Count(rendered, "\n") + 1
	}
	m.homePicker.Scroll = followScrollWindowSplit(m.homePicker.Scroll, m.homePicker.Cursor, heights, m.homeViewportRows())
}

// renderHome renders the multi-project picker mirroring the visual grammar
// of a board column: an outer kanbanColumn box, the same `// X · N` kicker
// + horizontal rule, and stacked cards inside that reuse the same
// card/cardSelected styles as the board's task cards.
//
// The geometry is wider than a board column (paths and project names need
// breathing room) but the layered structure — column wrapper, internal
// header, internal cards — is identical. wrapBadges is shared with task-
// card rendering so chip alignment matches across surfaces.
func (m Model) renderHome() string {
	available := m.availableWidth()
	columnInner := available - 2
	if columnInner > homeColumnInnerMax {
		columnInner = homeColumnInnerMax
	}
	if columnInner < homeColumnInnerMin {
		columnInner = homeColumnInnerMin
	}
	cardWidth, cardContent := m.homeCardSizes()

	headerText := fmt.Sprintf("// PROJECTS · %d", len(m.homeProjects))
	lines := []string{
		m.styles.hintAccent.Render(headerText),
		m.hRule(columnInner),
	}

	if len(m.homeProjects) == 0 {
		lines = append(lines, m.styles.empty.Width(columnInner).Render("no projects"))
		body := m.styles.kanbanColumn.Width(columnInner).Render(strings.Join(lines, "\n"))
		return "\n" + indentBlock(body, 2) + "\n\n" + indentBlock(m.renderHomeEmptyHint(), 2)
	}

	cursor := m.homePicker.Cursor
	scroll := m.homePicker.Scroll
	rendered := make([]string, len(m.homeProjects))
	heights := make([]int, len(m.homeProjects))
	for i := range m.homeProjects {
		rendered[i] = m.renderProjectCard(m.homeProjects[i], i == cursor, cardWidth, cardContent)
		heights[i] = strings.Count(rendered[i], "\n") + 1
	}
	lines = append(lines, m.renderScrollWindowSplit(rendered, heights, scroll, m.homeViewportRows())...)

	body := m.styles.kanbanColumn.Width(columnInner).Render(strings.Join(lines, "\n"))
	return "\n" + indentBlock(body, 2)
}

const (
	homeColumnInnerMin = 40
	homeColumnInnerMax = 84
)

// renderProjectCard mirrors the board's renderCard layout — title line(s)
// wrapped to the card's content width, a hint-styled secondary line with
// metadata (slug + truncated path), and a bottom badge row carrying the
// open-task pill and every project_tag as a chip. wrapBadges and the card
// styles come from the board surface so the two cards share width math.
func (m Model) renderProjectCard(project domain.Project, selected bool, cardWidth, contentWidth int) string {
	title := project.Name
	if title == "" {
		title = project.Slug
	}
	wrapped := wrapWords(title, contentWidth, contentWidth)
	lines := make([]string, 0, len(wrapped)+2)
	lines = append(lines, wrapped...)

	metaRaw := project.Slug + " · " + project.RootPath
	if lipgloss.Width(metaRaw) > contentWidth {
		// Slug stays full; only the path is truncated so the project's leaf
		// directory remains identifiable even on narrow terminals.
		budget := contentWidth - lipgloss.Width(project.Slug+" · ")
		if budget < 4 {
			metaRaw = project.Slug
		} else {
			metaRaw = project.Slug + " · " + truncatePath(project.RootPath, budget)
		}
	}
	lines = append(lines, m.styles.hint.Render(metaRaw))

	if badges := m.renderProjectBadges(project, contentWidth); badges != "" {
		lines = append(lines, badges)
	}

	style := m.styles.card.Width(cardWidth)
	if selected {
		style = m.styles.cardSelected.Width(cardWidth)
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderProjectBadges builds the chip row for a project card: an open-task
// pill (using the same priority palette as task cards so the visual weight
// stays consistent) followed by every project_tag as a CUSTOM-style chip.
func (m Model) renderProjectBadges(project domain.Project, maxWidth int) string {
	var badges []string

	pending := m.homeProjectPending[project.ID]
	switch pending {
	case 0:
		badges = append(badges, m.styles.badgeLow.Render("0 OPEN"))
	case 1:
		badges = append(badges, m.styles.badgeNormal.Render("1 OPEN"))
	default:
		badges = append(badges, m.styles.badgeBlocker.Render(fmt.Sprintf("%d OPEN", pending)))
	}

	for _, tag := range m.homeProjectTags[project.ID] {
		label := tag.Label
		if label == "" {
			label = tag.Name
		}
		badges = append(badges, m.styles.badgeInfo.Render(strings.ToUpper(label)))
	}
	return wrapBadges(badges, maxWidth)
}

// truncatePath shortens an absolute path to fit width using a `…/tail`
// shape so the user still recognises the project's leaf directory. Falls
// back to a head ellipsis when even the leaf is too wide for the column.
func truncatePath(path string, width int) string {
	if width <= 0 || lipgloss.Width(path) <= width {
		return path
	}
	if width <= 3 {
		return "…"
	}
	parts := strings.Split(path, "/")
	tail := parts[len(parts)-1]
	if lipgloss.Width(tail)+2 > width {
		// Even the leaf is too wide — head-truncate it.
		return "…" + tail[len(tail)-(width-1):]
	}
	for i := len(parts) - 2; i >= 0; i-- {
		candidate := "…/" + strings.Join(parts[i:], "/")
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return "…/" + tail
}

func (m Model) renderHomeEmptyHint() string {
	lines := []string{
		m.styles.hintAccent.Render("No projects registered."),
		"",
		m.styles.hint.Render("Register one with:"),
		m.styles.hint.Render("  okt init --name MyProject --slug my-project"),
		"",
		m.styles.hint.Render("Then re-open ") + m.styles.hintAccent.Render("okt tui") + m.styles.hint.Render("."),
	}
	return m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join(lines, "\n"))
}

// homeFooterTokens returns the footer hint shown while on Home as the
// structured token list `renderFooter` expects. Kept inline (not in
// render_chrome) so the Home-specific keymap lives next to the rest
// of the Home rendering — easier to keep in sync as the view evolves.
func (m Model) homeFooterTokens() []footerToken {
	if len(m.homeProjects) == 0 {
		return []footerToken{
			{key: "q", label: "quit"},
			helpToken(),
		}
	}
	return []footerToken{
		{key: "enter", label: "open", primary: true},
		{key: "up/down", label: "move"},
		{key: "ctrl+h", label: "refresh"},
		{key: "q", label: "quit"},
		helpToken(),
	}
}

// homeHeaderTitle renders the chromeless Home title used by render_chrome
// when the tab bar is suppressed. The shape mirrors the standard nav row
// (kicker + rule) so the surface still feels at home in the TUI grammar.
func (m Model) homeHeaderTitle() string {
	width := m.availableWidth()
	if width > 78 {
		width = 78
	}
	kicker := m.styles.activeNav.Render("00 // HOME")
	hint := m.styles.hint.Render("  ctrl+h returns here from any view")
	rule := m.styles.activeNav.Render(strings.Repeat("─", lipgloss.Width("00 // HOME")))
	return kicker + hint + "\n  " + rule + strings.Repeat(" ", width-lipgloss.Width(rule))
}
