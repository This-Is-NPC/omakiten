package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/scrollwindow"
	"omakiten/internal/tui/components/viewport"
)

// openProjectView routes to the dedicated project-view screen
// (subProjectView, reached via Ctrl+P). It pushes the current view onto
// the back-stack so ctrl+o restores it, resets the screen's focus to the
// form zone + scroll to the top, and primes the project-scoped activity
// feed + status dashboard via refreshProjectSummary. The project identity
// itself comes from m.project (already resolved at boot); this only
// fetches the feed + dashboard counts.
//
// Lives outside render_*.go on purpose: the scroll-state boundary arch
// test (internal/arch) forbids render_*.go from writing a *Scroll field,
// and the open/scroll mutators here reset projectActivityScroll. The render
// pass (render_project.go) stays pure.
func (m *Model) openProjectView() {
	m.pushHistory()
	m.moveMode = false
	m.status = ""
	m.top = topTasks
	m.sub = subProjectView
	m.projectFocus = projectFocusForm
	m.projectActivityScroll = 0
	m.projectActivityCursor = -1
	m.projectFormScreenOpen = false
	m.projectFormScreen = detailscreen.New(0)
	if err := m.refreshProjectSummary(); err != nil {
		m.status = err.Error()
	}
}

// refreshProjectSummary loads the project- and universal-scoped comment
// events for the current project into m.projectActivity, the project's
// description / tag attachments into m.projectDescription / m.projectTags
// for the form zone, and the status dashboard counts into
// m.projectDashboard. An empty filter pulls the full pinned-first feed.
//
// Every cached field is cleared BEFORE the fetch so a project switch (or a
// mid-refresh error) never leaves stale description / tags / dashboard
// numbers from the previously-viewed project on screen. The Projects /
// Tags / Plans repos are nil-guarded so the comment-only test model (which
// wires neither) still refreshes the feed without panicking; a fetch error
// on any one leaves only that field empty rather than failing the whole
// refresh.
func (m *Model) refreshProjectSummary() error {
	// Clear cached fields up front so a switch/error shows the new
	// project's data (or an empty state), never the previous project's.
	m.projectDescription = ""
	m.projectTags = nil
	m.projectDashboard = projectDashboardData{}

	events, err := m.commentsForProjectScope(domain.CommentFilter{})
	if err != nil {
		return err
	}
	m.projectActivity = events

	if m.repos.Projects != nil {
		if project, perr := m.repos.Projects.FindProjectByID(m.ctx, m.project.ID); perr == nil {
			m.projectDescription = project.Description
		}
	}
	if m.repos.Tags != nil {
		if tags, terr := m.repos.Tags.ListProjectTags(m.ctx, m.project.ID); terr == nil {
			m.projectTags = tags
		}
	}

	// Reload the project's active task set BEFORE folding the dashboard so
	// the counts reflect the current state on open AND on `r` — the board's
	// own load only fires on board nav, so without this Ctrl+P (and the
	// project-view refresh) would show whatever m.tasks held when the board
	// was last visited. Reuses the board's snapshot path (same sort + active
	// scope) so the two views agree on which tasks count.
	m.reloadProjectTasks()

	m.projectDashboard = m.computeProjectDashboard()
	return nil
}

// reloadProjectTasks refreshes m.tasks (and the dependent board caches) so
// the project-view dashboard counts stay current. Reads the task list
// directly via the Tasks repo's ListTasks (the same call the board's
// Snapshot uses) rather than a full TUI snapshot, so it does not depend on
// the Comments/Dependencies repos a comment-only model leaves nil. Scope
// mirrors the board: active tasks unless the user has toggled archived in,
// sorted by the active board view. Nil-guards the Tasks repo and leaves
// m.tasks untouched on a query error rather than blanking a loaded set.
func (m *Model) reloadProjectTasks() {
	if m.repos.Tasks == nil {
		return
	}
	views := m.activeViewSettings()
	snap := m.repos.activeSnapshot()
	filter := domain.TaskFilter{
		Sort:            domain.TaskSort{Field: views.Board.Sort.Field, Order: views.Board.Sort.Order},
		IncludeArchived: m.includeArchived,
	}
	tasks, err := m.repos.Tasks.ListTasks(m.ctx, m.project.ID, filter, snap)
	if err != nil {
		return
	}
	m.tasks = tasks
	m.invalidateBoardCaches()
	m.rebuildBoardCaches()
}

// computeProjectDashboard folds the model's already-loaded task slice and
// the plan rollups into the status-dashboard snapshot. Tasks-per-bucket
// counts every active task (roots AND sub-tasks, unlike tasksByBucket
// which hides sub-tasks for the kanban columns); the root/sub split reads
// the parent_id (domain.Task.IsSubTask). Plan progress comes from
// PlanService.ListRollups, nil-guarded so a project without the Plans repo
// wired still renders the task half of the dashboard.
func (m *Model) computeProjectDashboard() projectDashboardData {
	data := projectDashboardData{}

	// Per-bucket counts in workflow order. Pre-seed every bucket at 0 so
	// empty lanes still render a row (the user reads the full workflow
	// distribution, not just the populated lanes).
	byBucket := map[string]int{}
	for _, task := range m.tasks {
		byBucket[task.BucketKey]++
		data.totalTasks++
		if task.IsSubTask() {
			data.subTasks++
		} else {
			data.rootTasks++
		}
	}
	for _, bucket := range m.workflow.Buckets {
		name := bucket.Name
		if name == "" {
			name = bucket.Key
		}
		data.bucketCounts = append(data.bucketCounts, projectBucketCount{
			name:  name,
			count: byBucket[bucket.Key],
		})
	}

	if m.repos.Plans != nil {
		planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
		if rollups, err := planSvc.ListRollups(m.ctx, m.project); err == nil {
			data.planCount = len(rollups)
			for _, r := range rollups {
				data.planDone += r.DoneCount
				data.planTotal += r.TotalCount
			}
		}
	}
	return data
}

// handleProjectKey owns the project-view screen's keys: Tab cycles which
// zone (form / dashboard / activity) owns scroll; j/k/pgup/pgdn scroll the
// active zone; g/home and G/end jump the focused zone to top/bottom; `f`
// opens the fullscreen project form overlay; r refreshes; esc leaves the
// screen via the same back-nav as ctrl+o. The task-mutation keys
// (n/e/c/m/A) are swallowed so the read-only v1 screen stays inert.
// Returns true when the key was consumed so the dispatcher does not fall
// through.
func (m *Model) handleProjectKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "tab":
		m.cycleProjectFocus(1)
		return true
	case "shift+tab":
		m.cycleProjectFocus(-1)
		return true
	case "f":
		m.openProjectFormScreen()
		return true
	case "enter":
		// Enter opens the focused project/universal comment in the shared
		// full-width comment detail overlay. No-op off the activity zone or
		// on a system-event row (no body to read).
		if m.projectFocus == projectFocusActivity {
			m.openProjectCommentScreen()
		}
		return true
	case "j", "down":
		// In the activity zone, j/k move the selection by comment card
		// (mirroring the task activity panel) and the scroll follows; the
		// form + dashboard zones have no cursor, so they fall through to the
		// shared line-scroll no-op.
		if m.projectFocus == projectFocusActivity {
			m.moveProjectActivityCursor(1)
		} else {
			m.scrollProjectFocused(1)
		}
		return true
	case "k", "up":
		if m.projectFocus == projectFocusActivity {
			m.moveProjectActivityCursor(-1)
		} else {
			m.scrollProjectFocused(-1)
		}
		return true
	case "pgdown", "pgdn":
		m.scrollProjectFocused(m.projectActivityViewportLines())
		return true
	case "pgup":
		m.scrollProjectFocused(-m.projectActivityViewportLines())
		return true
	case "g", "home":
		// In the activity zone, jump the SELECTION to the first card (scroll
		// follows); other zones snap their raw line offset to the top.
		if m.projectFocus == projectFocusActivity && len(m.projectActivity) > 0 {
			m.projectActivityCursor = 0
			m.syncProjectActivityScrollToCursor()
		} else {
			m.setProjectFocusedScroll(0)
		}
		return true
	case "G", "end":
		if m.projectFocus == projectFocusActivity && len(m.projectActivity) > 0 {
			m.projectActivityCursor = len(m.projectActivity) - 1
			m.syncProjectActivityScrollToCursor()
		} else {
			m.setProjectFocusedScroll(m.projectFocusedScrollMax())
		}
		return true
	case "esc":
		// Leave the project view the same way ctrl+o does: pop the back
		// stack to restore the prior (top, sub). The footer advertises
		// "esc back", so this makes the binding real. No-op when the
		// stack is empty (start of session).
		if m.popHistory() {
			m.moveMode = false
			m.status = ""
		}
		return true
	case "r":
		if err := m.refreshProjectSummary(); err != nil {
			m.status = err.Error()
		} else {
			m.status = m.t("tui.status.refreshed")
		}
		return true
	case "n", "e", "c", "m", "A":
		// Read-only in v1: swallow the task-mutation keys so they do not
		// fall through to handleCommonKey (which gates on top==topTasks,
		// true here) and open a task create/edit/comment surface.
		return true
	}
	return false
}

// cycleProjectFocus rotates the project-view focus through
// form → dashboard → activity by delta steps, wrapping at both ends.
// delta=+1 for Tab, delta=-1 for Shift+Tab. Mirrors cycleTaskField.
//
// Entering the activity zone auto-anchors the cursor onto the first card
// (so the first j/k always moves a visible selection, like applyTaskFocus
// does for the task feed); leaving it clears the cursor so the panel stops
// drawing a focused border while another zone owns the keys.
func (m *Model) cycleProjectFocus(delta int) {
	const n = 3
	cur := int(m.projectFocus)
	cur = (cur + delta + n) % n
	m.projectFocus = projectScreenFocus(cur)
	if m.projectFocus == projectFocusActivity {
		if m.projectActivityCursor < 0 && len(m.projectActivity) > 0 {
			m.projectActivityCursor = 0
			m.syncProjectActivityScrollToCursor()
		}
	} else {
		m.projectActivityCursor = -1
	}
}

// moveProjectActivityCursor advances the project activity selection by delta
// cards and scrolls so the focused card stays inside the viewport. Mirrors
// moveActivityCursor for the task feed: wraps from "no selection" (-1) to the
// first or last card by direction, and re-anchors onto the nearest visible
// edge when pgup/pgdn has scrolled the cursor off-screen so the page-scroll
// work is not thrown away.
func (m *Model) moveProjectActivityCursor(delta int) {
	rows := len(m.projectActivity)
	if rows == 0 {
		m.projectActivityCursor = -1
		return
	}
	if m.projectActivityCursor < 0 {
		if delta > 0 {
			m.projectActivityCursor = 0
		} else {
			m.projectActivityCursor = rows - 1
		}
		m.syncProjectActivityScrollToCursor()
		return
	}
	if first, last, ok := m.visibleProjectActivityCardRange(); ok && (m.projectActivityCursor < first || m.projectActivityCursor > last) {
		if m.projectActivityCursor < first {
			m.projectActivityCursor = first
		} else {
			m.projectActivityCursor = last
		}
		m.syncProjectActivityScrollToCursor()
		return
	}
	next := m.projectActivityCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= rows {
		next = rows - 1
	}
	m.projectActivityCursor = next
	m.syncProjectActivityScrollToCursor()
}

// visibleProjectActivityCardRange returns the inclusive [first, last] card
// indices whose start line falls inside the current project activity viewport
// window. ok=false when the feed is empty or no card start sits in the visible
// band. Mirrors visibleActivityCardRange but reads the raw
// projectActivityScroll offset instead of the linelist.
func (m Model) visibleProjectActivityCardRange() (int, int, bool) {
	if len(m.projectActivity) == 0 {
		return 0, 0, false
	}
	ranges := cardLineRanges(m.activityRowsForRender(m.projectActivity))
	viewport := m.projectActivityViewportLines()
	if viewport <= 0 {
		return 0, 0, false
	}
	top := m.projectActivityScroll
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

// syncProjectActivityScrollToCursor positions projectActivityScroll (a LINE
// offset) so the focused card's body is visible inside the viewport. Delegates
// the slice math to scrollwindow.Follow with HintsSplit so the hint-row
// reservation matches what renderScrollWindowSplit consumes. Mirrors
// syncActivityScrollToCursor but operates on the plain int offset (the project
// feed has no linelist component). Tall cards top-align so the header stays
// reachable.
func (m *Model) syncProjectActivityScrollToCursor() {
	if m.projectActivityCursor < 0 || m.projectActivityCursor >= len(m.projectActivity) {
		return
	}
	cards := m.activityRowsForRender(m.projectActivity)
	body := flattenActivityCards(cards)
	ranges := cardLineRanges(cards)
	viewport := m.projectActivityViewportLines()
	if viewport <= 0 || len(body) <= viewport {
		m.projectActivityScroll = 0
		return
	}
	r := ranges[m.projectActivityCursor]
	cardTop := r.start
	cardLast := r.start + r.height - 1
	heights := make([]int, len(body))
	for i := range heights {
		heights[i] = 1
	}
	scroll := scrollwindow.Follow(m.projectActivityScroll, cardLast, heights, viewport, scrollwindow.HintsSplit)
	// Top-align tall cards: Follow only ADVANCES scroll to fit the last
	// line, so snap back to the card's first line when it overshot the
	// header (same UX rule as syncActivityScrollToCursor).
	if scroll > cardTop {
		scroll = cardTop
	}
	if scroll < 0 {
		scroll = 0
	}
	m.projectActivityScroll = scroll
}

// openProjectCommentScreen opens the shared full-width comment detail overlay
// for the project/universal comment under the project activity cursor. System
// events have no body to read, so Enter ignores them. Sets
// commentScreenFromProject so the detail lookup reads m.projectActivity and
// esc restores the project activity cursor. Mirrors openCommentScreen.
func (m *Model) openProjectCommentScreen() {
	if m.projectActivityCursor < 0 || m.projectActivityCursor >= len(m.projectActivity) {
		return
	}
	ev := m.projectActivity[m.projectActivityCursor]
	if ev.EventType != domain.EventTypeComment {
		return
	}
	m.commentScreenOpen = true
	m.commentScreenFromProject = true
	m.commentScreenID = ev.ID
	m.commentScreen = detailscreen.New(0)
}

// scrollProjectFocused nudges the activity zone's scroll offset by delta
// lines, clamped at zero. Only the activity zone is scroll-windowed; the
// form + dashboard draw a full fixed body, so scroll keys are a no-op there
// (the footer hides the scroll hints to match).
func (m *Model) scrollProjectFocused(delta int) {
	if m.projectFocus != projectFocusActivity {
		return
	}
	m.projectActivityScroll = clampMinZero(m.projectActivityScroll + delta)
}

// setProjectFocusedScroll snaps the activity zone's scroll to an absolute
// offset (clamped at zero) — backs the g/home top-of-zone binding. No-op for
// the non-windowed form + dashboard zones.
func (m *Model) setProjectFocusedScroll(offset int) {
	if m.projectFocus != projectFocusActivity {
		return
	}
	if offset < 0 {
		offset = 0
	}
	m.projectActivityScroll = offset
}

// projectFocusedScrollMax is the largest scroll offset that still shows
// content for the focused zone — the absolute target for the G/end
// "scroll to bottom" binding. For the activity zone it mirrors the
// renderer's clamp (offset is bounded to len(items)-1 inside
// renderScrollWindowSplit). The form + dashboard zones are not
// scroll-windowed in v1 (they draw a full fixed detail), so their bottom
// is the top: 0.
func (m *Model) projectFocusedScrollMax() int {
	if m.projectFocus != projectFocusActivity {
		return 0
	}
	if len(m.projectActivity) == 0 {
		return 0
	}
	items := flattenActivityCards(m.activityRowsForRender(m.projectActivity))
	if len(items) == 0 {
		return 0
	}
	return len(items) - 1
}

// openProjectFormScreen opens the dedicated, full-width project form
// overlay (`f` from the project view). Resets the embedded detailscreen so
// the body always opens at the top. Mirrors openDescriptionScreen /
// openPlanGoalScreen — the proven fullscreen-overlay pattern.
func (m *Model) openProjectFormScreen() {
	m.projectFormScreenOpen = true
	m.projectFormScreen = detailscreen.New(0)
}

// closeProjectFormScreen returns the user to the project view. Focus state
// on the underlying screen (form / dashboard / activity) survives the
// round-trip, so esc lands them back where they were.
func (m *Model) closeProjectFormScreen() {
	m.projectFormScreenOpen = false
	m.projectFormScreen = detailscreen.New(0)
}

// updateProjectFormScreen runs the key handler while the project form
// overlay is on screen. Delegates scrolling to the embedded detailscreen;
// esc / `f` closes; `M` toggles raw / rendered markdown — mirrors
// updateDescriptionScreen / updatePlanGoalScreen so the read-only overlays
// share one keybinding vocabulary. Value receiver (like updatePlanGoalScreen)
// so the dispatcher returns a Model, not *Model — the pointer-receiver
// close / toggle helpers still mutate the addressable local copy in place.
func (m Model) updateProjectFormScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "f":
		m.closeProjectFormScreen()
		return m, nil
	case "M":
		m.toggleMarkdownRendered()
		return m, nil
	}
	var cmd tea.Cmd
	m.projectFormScreen, cmd = m.projectFormScreen.Update(msg, m.taskViewportHeight())
	if m.projectFormScreen.LastEvent() == viewport.EventCancel {
		m.closeProjectFormScreen()
	}
	return m, cmd
}

// clampMinZero returns n, or 0 when n is negative. Tiny helper kept local
// to the project-view scroll math.
func clampMinZero(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
