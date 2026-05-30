package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
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
// and the open/scroll mutators here reset projectActivityScroll /
// projectMetaScroll. The render pass (render_project.go) stays pure.
func (m *Model) openProjectView() {
	m.pushHistory()
	m.moveMode = false
	m.status = ""
	m.top = topTasks
	m.sub = subProjectView
	m.projectFocus = projectFocusForm
	m.projectActivityScroll = 0
	m.projectMetaScroll = 0
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
	m.projectDashboard = m.computeProjectDashboard()
	return nil
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
	case "j", "down":
		m.scrollProjectFocused(1)
		return true
	case "k", "up":
		m.scrollProjectFocused(-1)
		return true
	case "pgdown", "pgdn":
		m.scrollProjectFocused(m.projectActivityViewportLines())
		return true
	case "pgup":
		m.scrollProjectFocused(-m.projectActivityViewportLines())
		return true
	case "g", "home":
		m.setProjectFocusedScroll(0)
		return true
	case "G", "end":
		m.setProjectFocusedScroll(m.projectFocusedScrollMax())
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
func (m *Model) cycleProjectFocus(delta int) {
	const n = 3
	cur := int(m.projectFocus)
	cur = (cur + delta + n) % n
	m.projectFocus = projectScreenFocus(cur)
}

// scrollProjectFocused nudges the scroll offset of whichever zone
// currently owns focus by delta lines, clamped at zero. The dashboard zone
// is fixed-height (renders the full grid), so it shares the metadata
// scroll field with the form zone — neither scroll-windows in v1.
func (m *Model) scrollProjectFocused(delta int) {
	if m.projectFocus == projectFocusActivity {
		m.projectActivityScroll = clampMinZero(m.projectActivityScroll + delta)
		return
	}
	m.projectMetaScroll = clampMinZero(m.projectMetaScroll + delta)
}

// setProjectFocusedScroll snaps the focused zone's scroll to an absolute
// offset (clamped at zero) — backs the g/home top-of-zone binding.
func (m *Model) setProjectFocusedScroll(offset int) {
	if offset < 0 {
		offset = 0
	}
	if m.projectFocus == projectFocusActivity {
		m.projectActivityScroll = offset
		return
	}
	m.projectMetaScroll = offset
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
