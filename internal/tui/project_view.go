package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

// openProjectView routes to the dedicated project-view screen
// (subProjectView, reached via Ctrl+P). It pushes the current view onto
// the back-stack so ctrl+o restores it, resets the screen's focus to the
// metadata panel + scroll to the top, and primes the project-scoped
// activity feed via refreshProjectSummary. The project identity itself
// comes from m.project (already resolved at boot); this only fetches the
// feed.
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
	m.projectFocus = projectFocusMeta
	m.projectActivityScroll = 0
	m.projectMetaScroll = 0
	if err := m.refreshProjectSummary(); err != nil {
		m.status = err.Error()
	}
}

// refreshProjectSummary loads the project- and universal-scoped comment
// events for the current project into m.projectActivity, ready for the
// activity feed on the project-view screen. Project identity (name / slug
// / root path / id) is read straight from m.project at render time, so it
// is not re-fetched here. An empty filter pulls the full pinned-first
// feed.
func (m *Model) refreshProjectSummary() error {
	events, err := m.commentsForProjectScope(domain.CommentFilter{})
	if err != nil {
		return err
	}
	m.projectActivity = events
	return nil
}

// handleProjectKey owns the project-view screen's keys: Tab toggles which
// panel (metadata / activity) owns scroll; j/k/pgup/pgdn scroll the active
// panel; g/home and G/end jump the focused panel to top/bottom; r refreshes
// the feed; esc leaves the screen via the same back-nav as ctrl+o (pop the
// view history to restore the prior top/sub). The task-mutation keys
// (n/e/c/m/A) are swallowed so the read-only v1 screen stays inert instead
// of falling through to handleCommonKey's board bindings. Returns true when
// the key was consumed so the dispatcher does not fall through.
func (m *Model) handleProjectKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "tab":
		m.toggleProjectFocus()
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

// toggleProjectFocus rotates the project-view focus metadata ↔ activity.
func (m *Model) toggleProjectFocus() {
	if m.projectFocus == projectFocusMeta {
		m.projectFocus = projectFocusActivity
	} else {
		m.projectFocus = projectFocusMeta
	}
}

// scrollProjectFocused nudges the scroll offset of whichever panel
// currently owns focus by delta lines, clamped at zero.
func (m *Model) scrollProjectFocused(delta int) {
	if m.projectFocus == projectFocusActivity {
		m.projectActivityScroll = clampMinZero(m.projectActivityScroll + delta)
		return
	}
	m.projectMetaScroll = clampMinZero(m.projectMetaScroll + delta)
}

// setProjectFocusedScroll snaps the focused panel's scroll to an absolute
// offset (clamped at zero) — backs the g/home top-of-panel binding.
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
// content for the focused panel — the absolute target for the G/end
// "scroll to bottom" binding. For the activity panel it mirrors the
// renderer's clamp (offset is bounded to len(items)-1 inside
// renderScrollWindowSplit). The metadata panel is not scroll-windowed in
// v1 (renderProjectMetaPanel draws the full fixed detail), so its bottom
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

// clampMinZero returns n, or 0 when n is negative. Tiny helper kept local
// to the project-view scroll math.
func clampMinZero(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
