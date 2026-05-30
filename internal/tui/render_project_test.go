package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlP() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlP} }
func tabKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

// TestCtrlPOpensProjectView proves Ctrl+P from a per-project board routes
// to the dedicated project-view screen (subProjectView) and primes the
// project-scoped activity feed (refreshProjectSummary).
func TestCtrlPOpensProjectView(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	if model.sub == subProjectView {
		t.Fatalf("setup: model should not start on the project view")
	}

	next, _ := model.Update(ctrlP())
	got := next.(Model)

	if got.sub != subProjectView {
		t.Fatalf("ctrl+p did not route to subProjectView; sub = %v", got.sub)
	}
	if got.top != topTasks {
		t.Fatalf("project view should anchor under topTasks; top = %v", got.top)
	}
	// refreshProjectSummary fetched the project + universal feed (2 project
	// comments + 1 universal = 3; the task-scoped comment is excluded).
	if len(got.projectActivity) != 3 {
		t.Fatalf("projectActivity = %d events, want 3 (project+universal)", len(got.projectActivity))
	}
	if got.projectFocus != projectFocusMeta {
		t.Fatalf("project view should open focused on metadata; focus = %v", got.projectFocus)
	}
}

// TestRenderProjectViewShowsMetadataAndFeed is the render smoke: the
// metadata panel surfaces the projects-table identity fields and the
// activity feed surfaces the project-scoped comment bodies.
func TestRenderProjectViewShowsMetadataAndFeed(t *testing.T) {
	model, project, _ := scopedFeedModel(t)
	next, _ := model.Update(ctrlP())
	got := next.(Model)

	out := got.renderProjectView()

	// Metadata panel: name, slug, root path.
	for _, want := range []string{project.Name, project.Slug, project.RootPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("project view missing metadata %q:\n%s", want, out)
		}
	}
	// Activity feed: project-scoped + universal comment bodies, pinned first.
	for _, want := range []string{"pinned cover sheet", "project handoff body", "universal body"} {
		if !strings.Contains(out, want) {
			t.Fatalf("project view missing feed body %q:\n%s", want, out)
		}
	}
	// The task-scoped comment must NOT appear — it belongs to the task feed.
	if strings.Contains(out, "task body") {
		t.Fatalf("task-scoped comment leaked into project view:\n%s", out)
	}
}

// TestProjectViewTabTogglesFocus proves Tab rotates the focused panel
// metadata ↔ activity inside the project view.
func TestProjectViewTabTogglesFocus(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	if m.projectFocus != projectFocusMeta {
		t.Fatalf("setup: project view should open on metadata focus")
	}

	next, _ := m.Update(tabKey())
	m = next.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("tab did not move focus to activity; focus = %v", m.projectFocus)
	}

	next, _ = m.Update(tabKey())
	m = next.(Model)
	if m.projectFocus != projectFocusMeta {
		t.Fatalf("second tab did not return focus to metadata; focus = %v", m.projectFocus)
	}
}

// TestProjectViewFooterTokens proves the project-view footer advertises
// its own key vocabulary (zone toggle + scroll + back) instead of falling
// through to a board/default footer.
func TestProjectViewFooterTokens(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	keys := map[string]struct{}{}
	for _, tok := range m.footerTokens() {
		keys[tok.key] = struct{}{}
	}
	for _, want := range []string{"tab", "r", "esc"} {
		if _, ok := keys[want]; !ok {
			t.Fatalf("project-view footer missing %q token; got %v", want, keys)
		}
	}
}

// TestProjectViewEscLeavesScreen proves the footer's advertised "esc back"
// is real: esc pops the back stack so the project view (subProjectView) is
// no longer the active sub afterward.
func TestProjectViewEscLeavesScreen(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	priorSub := model.sub
	if priorSub == subProjectView {
		t.Fatalf("setup: model should not start on the project view")
	}

	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	if m.sub != subProjectView {
		t.Fatalf("setup: ctrl+p did not open the project view; sub = %v", m.sub)
	}

	back, _ := m.Update(escKey())
	m = back.(Model)
	if m.sub == subProjectView {
		t.Fatalf("esc did not leave the project view; sub still %v", m.sub)
	}
	if m.sub != priorSub {
		t.Fatalf("esc should restore the prior sub %v; got %v", priorSub, m.sub)
	}
}

// TestOpenProjectViewNilCommentsRepoNoPanic proves openProjectView (→
// refreshProjectSummary → commentsForProjectScope) does not panic when the
// Comments repo is nil, and surfaces an empty feed instead of crashing on
// the nil-interface QueryComments call.
func TestOpenProjectViewNilCommentsRepoNoPanic(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	model.repos.Comments = nil

	model.openProjectView()

	if model.sub != subProjectView {
		t.Fatalf("openProjectView should still route to subProjectView; sub = %v", model.sub)
	}
	if len(model.projectActivity) != 0 {
		t.Fatalf("nil Comments repo should yield an empty feed; got %d events", len(model.projectActivity))
	}
}
