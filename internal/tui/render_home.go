package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/tui/components/notification"
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
	snap := m.repos.activeSnapshot()
	tasks, err := m.repos.Tasks.ListTasks(m.ctx, projectID, domain.TaskFilter{}, snap)
	if err != nil {
		return 0, err
	}
	if snap == nil {
		return len(tasks), nil
	}
	wf := snap.Workflow()
	if len(wf.Buckets) == 0 {
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
			m.status = m.t("tui.status.refreshed")
		}
		return
	}
	// Destructive delete gate: lowercase `d` arms the highlighted
	// card; a second press on the same card fires the cascade. esc
	// (handled by the picker below as EventCancel) clears the arm.
	// Routed before the picker so `d` is consumed instead of bubbling
	// into the picker's filter/search handlers.
	if msg.String() == "d" && len(m.homeProjects) > 0 {
		project := m.homeProjects[m.homePicker.Cursor]
		m.armOrConfirmHomeProjectDelete(project)
		return
	}
	// Any other key clears a pending delete so cursor moves cannot
	// confirm a delete that targeted a different card.
	if m.homeProjectDeletePendingID != 0 {
		m.homeProjectDeletePendingID = 0
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
		lines = append(lines, m.styles.empty.Width(columnInner).Render(m.t("tui.empty.home_no_projects")))
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
		m.styles.hintAccent.Render(m.t("tui.empty.home_no_projects_full")),
		"",
		m.styles.hint.Render(m.t("tui.home.register_with")),
		m.styles.hint.Render("  okt init --name MyProject --slug my-project"),
		"",
		m.styles.hint.Render(m.t("tui.home.then_reopen_prefix")) + m.styles.hintAccent.Render(m.t("tui.home.okt_tui_cmd")) + m.styles.hint.Render(m.t("tui.home.then_reopen_suffix")),
	}
	return m.styles.hintBox.Width(m.hintBoxWidth()).Render(strings.Join(lines, "\n"))
}

// armOrConfirmHomeProjectDelete fires the destructive Home delete
// confirmation overlay. The first press looks up the
// `home-project-delete-confirm` notification, resolves counters for
// the highlighted project, and shows the card with the body
// pre-rendered. Pressing the overlay's `D` action runs the cascade
// (handled by handleHomeProjectDeleteAction); pressing esc dismisses.
//
// Degraded fallback: when the notification YAML cannot be loaded (no
// bundle / stripped install) OR counter resolution fails, the gate
// reverts to the status-driven arm-then-confirm shape so the
// destructive flow stays available without the overlay.
func (m *Model) armOrConfirmHomeProjectDelete(project domain.Project) {
	if project.ID <= 0 {
		return
	}
	// Second-press fallback path: an existing pending id means the
	// overlay could not be shown on the first press, so the
	// status-driven gate is active. Honour the second `d` press by
	// firing the delete directly.
	if m.homeProjectDeletePendingID == project.ID {
		m.executeHomeProjectDelete(project)
		return
	}
	notif, ok := m.notifications["home-project-delete-confirm"]
	if !ok {
		m.homeProjectDeletePendingID = project.ID
		m.status = fmt.Sprintf(m.t("tui.confirm.home_project_delete_fmt"), project.Name)
		return
	}
	counters, err := m.repos.Projects.ProjectDeleteCounts(m.ctx, project.ID)
	if err != nil {
		m.homeProjectDeletePendingID = project.ID
		m.status = fmt.Sprintf(m.t("tui.confirm.home_project_delete_fmt"), project.Name)
		return
	}
	m.homeProjectDeletePendingID = project.ID
	title := fmt.Sprintf(m.t("tui.notification.project_delete.title_fmt"), project.Name)
	body := fmt.Sprintf(m.t("tui.notification.project_delete.body_fmt"),
		counters.Tasks, counters.Comments, counters.Plans, counters.Tags, counters.ActivityLogEntries)
	bm, _ := notification.New(notification.Options{
		Notification: notif,
		Theme:        m.theme,
		Text:         title + "\n\n" + body,
	})
	m.notification = &bm
}

// handleHomeProjectDeleteAction is the slug-routed counterpart of
// handleNotificationAction for the home-project-delete-confirm
// overlay. The "confirm" action id fires ProjectService.Delete
// against the still-armed project; any other id (today: only
// "cancel") clears the pending state without side effects.
func (m *Model) handleHomeProjectDeleteAction(action notification.ActionMsg) {
	pendingID := m.homeProjectDeletePendingID
	m.homeProjectDeletePendingID = 0
	if action.ActionID != "confirm" || pendingID == 0 {
		return
	}
	var project domain.Project
	for _, p := range m.homeProjects {
		if p.ID == pendingID {
			project = p
			break
		}
	}
	if project.ID == 0 {
		return
	}
	m.executeHomeProjectDelete(project)
}

// executeHomeProjectDelete wires the same ProjectService.Delete the
// CLI uses against the active TUI store + a freshly constructed
// BackupService. The snapshot lands under StateDir/backups/ with the
// configured retention. On success the Home read-model is reloaded
// (the row disappears) and the backup path surfaces in the status
// badge so the user knows where the recovery artefact lives. Failure
// leaves the project intact and surfaces the underlying error.
func (m *Model) executeHomeProjectDelete(project domain.Project) {
	m.homeProjectDeletePendingID = 0
	backup, err := m.buildHomeBackupService()
	if err != nil {
		m.status = err.Error()
		return
	}
	svc := app.NewProjectService(m.repos.Projects, backup, m.repos.Events)
	result, err := svc.Delete(m.ctx, project.ID)
	if err != nil {
		m.status = err.Error()
		return
	}
	if err := m.loadHome(); err != nil {
		m.status = err.Error()
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.project_deleted_fmt"), result.Project.Slug, result.BackupPath)
}

// buildHomeBackupService constructs a BackupService against the
// TUI's resolved DB path + the active snapshot's retention setting.
// Returns an error when the paths package cannot resolve the backup
// directory (rare; surfaces as a status hint rather than panicking).
func (m *Model) buildHomeBackupService() (app.BackupRunner, error) {
	destDir, err := paths.BackupDir()
	if err != nil {
		return nil, err
	}
	retention := 0
	if snap := m.repos.activeSnapshot(); snap != nil {
		retention = snap.Settings().Backup.RetentionCount
	}
	return app.NewBackupService(app.BackupOptions{
		SourcePath: m.repos.DBPath,
		DestDir:    destDir,
		Retention:  retention,
	}), nil
}

// homeFooterTokens returns the footer hint shown while on Home as the
// structured token list `renderFooter` expects. Kept inline (not in
// render_chrome) so the Home-specific keymap lives next to the rest
// of the Home rendering — easier to keep in sync as the view evolves.
func (m Model) homeFooterTokens() []footerToken {
	if len(m.homeProjects) == 0 {
		return []footerToken{
			{key: "q", label: m.t("tui.footer.quit")},
			m.helpToken(),
		}
	}
	tokens := []footerToken{
		{key: "enter", label: m.t("tui.footer.open"), primary: true},
		{key: "up/down", label: m.t("tui.footer.move")},
	}
	if m.homeProjectDeletePendingID != 0 {
		tokens = append(tokens, footerToken{key: "d", label: m.t("tui.footer.confirm_delete_project"), primary: true})
	} else {
		tokens = append(tokens, footerToken{key: "d", label: m.t("tui.footer.delete_project")})
	}
	tokens = append(tokens,
		footerToken{key: "ctrl+h", label: m.t("tui.footer.refresh")},
		footerToken{key: "q", label: m.t("tui.footer.quit")},
		m.helpToken(),
	)
	return tokens
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
