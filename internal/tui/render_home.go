package tui

import (
	"bytes"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
	"omakiten/internal/tui/components/notification"
	"omakiten/internal/tui/components/picker"
	"omakiten/internal/tui/components/scrollwindow"
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
func (m *Model) handleHomeKey(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "ctrl+h" {
		if err := m.loadHome(); err != nil {
			m.status = err.Error()
		} else {
			m.status = m.t("tui.status.refreshed")
		}
		return nil
	}
	// Destructive delete gate: lowercase `d` arms the highlighted
	// card; a second press on the same card fires the cascade. esc
	// (handled by the picker below as EventCancel) clears the arm.
	// Routed before the picker so `d` is consumed instead of bubbling
	// into the picker's filter/search handlers.
	if msg.String() == "d" && len(m.homeProjects) > 0 {
		project := m.homeProjects[m.homePicker.Cursor]
		return m.armOrConfirmHomeProjectDelete(project)
	}
	// Any other key clears a pending delete so cursor moves cannot
	// confirm a delete that targeted a different card.
	if m.homeProjectDeletePendingID != 0 {
		m.homeProjectDeletePendingID = 0
		m.homeProjectDeletePendingCounters = domain.ProjectDeleteCounters{}
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
			return nil
		}
		project := m.homeProjects[m.homePicker.Cursor]
		if err := m.selectHomeProject(project); err != nil {
			m.status = err.Error()
		}
	case picker.EventCancel:
		// Esc on Home is a no-op — quitting requires explicit q/ctrl+c so
		// the user does not accidentally drop out of the TUI.
	}
	return nil
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
	m.boardColOffset = 0
	m.boardLists = nil
	m.selected = 0
	m.tableList = m.tableList.WithLines(nil)
	m.graphList = m.graphList.WithLines(nil)
	m.graphCursor = m.graphCursor.WithItemCount(0)
	m.logsList = m.logsList.WithLines(nil)
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
	m.homePicker.Scroll = scrollwindow.Follow(m.homePicker.Scroll, m.homePicker.Cursor, heights, m.homeViewportRows(), scrollwindow.HintsSplit)
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
func (m *Model) armOrConfirmHomeProjectDelete(project domain.Project) tea.Cmd {
	if project.ID <= 0 {
		return nil
	}
	// Second-press fallback path: an existing pending id means the
	// overlay could not be shown on the first press, so the
	// status-driven gate is active. Honour the second `d` press by
	// firing the delete directly with the arm-time counters snapshot.
	if m.homeProjectDeletePendingID == project.ID {
		counters := m.homeProjectDeletePendingCounters
		m.homeProjectDeletePendingID = 0
		m.homeProjectDeletePendingCounters = domain.ProjectDeleteCounters{}
		return m.executeHomeProjectDelete(project, counters)
	}
	notif, ok := m.notifications["home-project-delete-confirm"]
	if !ok {
		// Degraded status-driven gate: still resolve counters so the
		// second-press execute can hand them to ProjectService.Delete
		// without re-querying. On failure fall through with zero
		// counters; Delete tolerates the zero value (it only uses
		// counters for the audit payload).
		counters, _ := m.repos.Projects.ProjectDeleteCounts(m.ctx, project.ID)
		m.homeProjectDeletePendingID = project.ID
		m.homeProjectDeletePendingCounters = counters
		m.status = fmt.Sprintf(m.t("tui.confirm.home_project_delete_fmt"), project.Name)
		return nil
	}
	counters, err := m.repos.Projects.ProjectDeleteCounts(m.ctx, project.ID)
	if err != nil {
		m.homeProjectDeletePendingID = project.ID
		m.homeProjectDeletePendingCounters = domain.ProjectDeleteCounters{}
		m.status = fmt.Sprintf(m.t("tui.confirm.home_project_delete_fmt"), project.Name)
		return nil
	}
	m.homeProjectDeletePendingID = project.ID
	m.homeProjectDeletePendingCounters = counters
	title := fmt.Sprintf(m.t("tui.notification.project_delete.title_fmt"), project.Name)
	body := fmt.Sprintf(m.t("tui.notification.project_delete.body_fmt"),
		counters.Tasks, counters.Comments, counters.Plans, counters.Tags, counters.ActivityLogEntries)
	bm, _ := notification.New(notification.Options{
		Notification: notif,
		Theme:        m.theme,
		Text:         title + "\n\n" + body,
		Catalog:      m.repos.Catalog,
	})
	m.notification = &bm
	return nil
}

// handleHomeProjectDeleteAction is the slug-routed counterpart of
// handleNotificationAction for the home-project-delete-confirm
// overlay. The "confirm" action id fires ProjectService.Delete
// against the still-armed project; any other id (today: only
// "cancel") clears the pending state without side effects. Returns
// the tea.Cmd that drives the (asynchronous) delete so the bubbletea
// runtime can schedule the IO off the Update goroutine — the
// destructive flow's checkpoint + backup + cascade transaction can
// take seconds on contended SQLite handles, and running it inline in
// Update freezes the entire TUI render loop until it returns.
func (m *Model) handleHomeProjectDeleteAction(action notification.ActionMsg) tea.Cmd {
	pendingID := m.homeProjectDeletePendingID
	pendingCounters := m.homeProjectDeletePendingCounters
	m.homeProjectDeletePendingID = 0
	m.homeProjectDeletePendingCounters = domain.ProjectDeleteCounters{}
	if action.ActionID != "confirm" || pendingID == 0 {
		return nil
	}
	var project domain.Project
	for _, p := range m.homeProjects {
		if p.ID == pendingID {
			project = p
			break
		}
	}
	if project.ID == 0 {
		return nil
	}
	return m.executeHomeProjectDelete(project, pendingCounters)
}

// homeProjectDeleteResultMsg carries the outcome of the asynchronous
// ProjectService.Delete invocation back to the bubbletea Update loop.
// The destructive sequence (checkpoint → backup file copy → cascade
// transaction → audit emission) can block for seconds on contended
// SQLite handles, so executeHomeProjectDelete returns a tea.Cmd that
// runs the work off the Update goroutine and emits this message once
// the call returns. Update folds the result into m.status + reloads
// the Home read-model.
type homeProjectDeleteResultMsg struct {
	project   domain.Project
	result    app.ProjectDeleteResult
	err       error
	audit     string
	pruneWarn error
}

// executeHomeProjectDelete prepares the destructive cascade and
// returns the tea.Cmd that runs it asynchronously. Synchronous prep
// (backup service construction + degraded-path counter re-query)
// stays on the Update goroutine because both touch m.repos directly;
// the long-running steps (checkpoint, file copy, transaction) move
// inside the returned Cmd so the bubbletea render loop keeps
// drawing while ProjectService.Delete runs.
//
// auditWarn is captured into a local buffer (never stderr — the
// bubbletea alt-screen lives on stdout and stderr writes leak under
// the render). Anything the service logs (checkpoint failure, audit
// emission failure, payload marshal failure) lands on m.status via
// the result handler so the operator still sees the discrepancy
// without a corrupted draw frame.
func (m *Model) executeHomeProjectDelete(project domain.Project, counters domain.ProjectDeleteCounters) tea.Cmd {
	var pruneWarn error
	backup, err := m.buildHomeBackupService(func(perr error) { pruneWarn = perr })
	if err != nil {
		m.status = err.Error()
		return nil
	}
	// Degraded-path re-query: arm-time counter resolution swallows
	// errors (armOrConfirmHomeProjectDelete:415-433) so a transient
	// SQLite hiccup lands a zero-value counter snapshot here. Without
	// the re-query the project.removed audit payload would claim
	// "deleted empty project" for a project with thousands of rows.
	// One extra round-trip on a rare branch is cheaper than an audit
	// lie. A genuinely empty project re-resolves to the same zeros,
	// so the value is correct either way.
	if counters == (domain.ProjectDeleteCounters{}) {
		if requeried, qerr := m.repos.Projects.ProjectDeleteCounts(m.ctx, project.ID); qerr == nil {
			counters = requeried
		}
	}
	// Render an immediate "deleting…" hint so the user sees the press
	// landed before the IO completes; the final status replaces this
	// on result.
	m.status = fmt.Sprintf(m.t("tui.status.deleting_project_fmt"), project.Name)

	ctx := m.ctx
	repos := m.repos
	return func() tea.Msg {
		var auditBuf bytes.Buffer
		svc := app.NewProjectService(repos.Projects, backup, repos.Events).
			WithCheckpointer(repos.Checkpointer).
			SetAuditWarnWriter(&auditBuf)
		result, err := svc.Delete(ctx, project.ID, counters)
		return homeProjectDeleteResultMsg{
			project:   project,
			result:    result,
			err:       err,
			audit:     auditBuf.String(),
			pruneWarn: pruneWarn,
		}
	}
}

// handleHomeProjectDeleteResult folds the asynchronous delete outcome
// into m.status and the Home read-model. Mirrors the legacy
// synchronous tail of executeHomeProjectDelete (status formatting +
// prune warning append + audit drain) so the user-visible surface
// stays identical; only the threading model changed.
func (m *Model) handleHomeProjectDeleteResult(msg homeProjectDeleteResultMsg) {
	if msg.err != nil {
		m.status = msg.err.Error()
		drainAuditString(&m.status, msg.audit)
		return
	}
	if err := m.loadHome(); err != nil {
		m.status = err.Error()
		drainAuditString(&m.status, msg.audit)
		return
	}
	m.status = fmt.Sprintf(m.t("tui.status.project_deleted_fmt"), msg.result.Project.Slug, msg.result.BackupPath)
	if msg.pruneWarn != nil {
		// Append the prune warning so the operator still sees the
		// snapshot landed AND knows the rotation pass left old
		// snapshots behind. The TUI cannot write to stderr (it would
		// corrupt the bubbletea render) so the status surface is the
		// only channel the operator can observe.
		m.status += " · " + fmt.Sprintf(m.t("cli.db.backup.prune_warn_fmt"), msg.pruneWarn.Error())
	}
	drainAuditString(&m.status, msg.audit)
}

// drainAuditString appends any audit-warning lines captured in audit
// to status, joined with " · " so the single-line status surface
// stays intact. ProjectService.Delete writes each warning via
// fmt.Fprintf with a "\n" terminator, and a single Delete can emit
// up to three (checkpoint failure, payload marshal failure, audit
// emission failure). Splitting on "\n" folds the multi-line buffer
// onto one status row instead of letting embedded newlines fracture
// the bubbletea render. No-op when audit is empty.
func drainAuditString(status *string, audit string) {
	audit = strings.TrimSpace(audit)
	if audit == "" {
		return
	}
	parts := strings.Split(audit, "\n")
	cleaned := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			cleaned = append(cleaned, t)
		}
	}
	if len(cleaned) == 0 {
		return
	}
	joined := strings.Join(cleaned, " · ")
	if *status == "" {
		*status = joined
		return
	}
	*status += " · " + joined
}

// buildHomeBackupService constructs a BackupService against the TUI's
// resolved DB path + the active snapshot's retention setting. Returns
// an error when the paths package cannot resolve the backup directory
// (rare; surfaces as a status hint rather than panicking). pruneWarn
// receives any failure from the post-snapshot prune pass so the caller
// can surface it through the TUI status surface — stderr is unsafe
// while bubbletea is rendering.
func (m *Model) buildHomeBackupService(pruneWarn func(error)) (app.BackupRunner, error) {
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
		PruneWarn:  pruneWarn,
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
