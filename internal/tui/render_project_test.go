package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

func ctrlP() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlP} }
func tabKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }
func endKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnd} }

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

// TestRenderProjectMetaPanelSingleFrame proves the metadata panel renders
// exactly ONE frame (the grid's own border) — the prior double-wrap
// (gridtable border + an outer m.styles.panel) produced two stacked top
// borders and misaligned rule fragments. The grid frame is a single top
// rule (┌…┐) and a single bottom rule (└…┘).
func TestRenderProjectMetaPanelSingleFrame(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160

	panel := m.renderProjectMetaPanel(true)
	lines := strings.Split(strings.TrimRight(panel, "\n"), "\n")

	tops, bottoms := 0, 0
	for _, line := range lines {
		if strings.Contains(line, "┌") {
			tops++
		}
		if strings.Contains(line, "└") {
			bottoms++
		}
	}
	if tops != 1 || bottoms != 1 {
		t.Fatalf("meta panel should have a single frame (1 top, 1 bottom); got %d top / %d bottom:\n%s", tops, bottoms, panel)
	}
}

// TestRenderProjectViewTagsAndDescription proves the project-view metadata
// panel surfaces the tags row and the description Span. The tag chips and
// description body are seeded onto the model directly (the comment-only
// test store wires neither the Projects nor Tags repo).
func TestRenderProjectViewTagsAndDescription(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160

	// Empty state: no tags, no description → both fall back to a hint/dash.
	out := m.renderProjectView()
	if !strings.Contains(out, strings.ToUpper(m.t("tui.row.tags"))) {
		t.Fatalf("project view missing TAGS row:\n%s", out)
	}
	if !strings.Contains(out, strings.ToUpper(m.t("tui.kicker.description"))) {
		t.Fatalf("project view missing DESCRIPTION kicker:\n%s", out)
	}
	if !strings.Contains(out, m.t("tui.empty.project_description")) {
		t.Fatalf("project view missing empty-state description hint:\n%s", out)
	}

	// Populated: chips + a markdown description body both render.
	m.projectTags = []domain.Tag{{Name: "go", Label: "go"}, {Name: "tui", Label: "tui"}}
	m.projectDescription = "ProjectDescriptionMarker"
	out = m.renderProjectView()
	if !strings.Contains(out, "go") || !strings.Contains(out, "tui") {
		t.Fatalf("project view missing tag chips:\n%s", out)
	}
	// The markdown renderer styles per word, so assert on a single-token
	// marker that survives without an interleaved ANSI break.
	if !strings.Contains(out, "ProjectDescriptionMarker") {
		t.Fatalf("project view missing description body:\n%s", out)
	}
	if strings.Contains(out, m.t("tui.empty.project_description")) {
		t.Fatalf("description empty-state hint should be gone once a body is set:\n%s", out)
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

// TestProjectViewGScrollsActivityToBottom proves G/end snaps the focused
// activity panel to its bottom offset (the renderer-clamped last item),
// mirroring g/home → top.
func TestProjectViewGScrollsActivityToBottom(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	// Focus the activity panel (Tab) so the scroll bindings act on it.
	tabbed, _ := m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("setup: tab did not focus the activity panel; focus = %v", m.projectFocus)
	}

	wantMax := m.projectFocusedScrollMax()
	if wantMax <= 0 {
		t.Fatalf("setup: expected a scrollable activity feed; max = %d", wantMax)
	}

	ended, _ := m.Update(endKey())
	m = ended.(Model)
	if m.projectActivityScroll != wantMax {
		t.Fatalf("end did not snap activity scroll to bottom; got %d want %d", m.projectActivityScroll, wantMax)
	}

	// g/home returns it to the top.
	top, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = top.(Model)
	if m.projectActivityScroll != 0 {
		t.Fatalf("home did not return activity scroll to top; got %d", m.projectActivityScroll)
	}
}

// TestRenderProjectViewLayoutSwitchesOnWidth proves the side-by-side ↔
// stacked decision is driven by terminal width: a wide terminal renders the
// two panels on the same rows (side-by-side), a narrow one stacks them.
func TestRenderProjectViewLayoutSwitchesOnWidth(t *testing.T) {
	model, project, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	// Wide: meta + activity share rows. The two panels are top-aligned, so
	// the meta kicker line (`PROJECT · <slug>`) and the activity kicker line
	// (`ACTIVITY`) land on the same physical row — proving a side-by-side
	// horizontal join. Pinning to the kicker rows (rather than a specific
	// field/card pairing) keeps the assertion stable as the meta panel grows
	// new fields and its body height shifts.
	upperName := strings.ToUpper(project.Name)
	m.width = 160
	wide := m.renderProjectView()
	sideBySide := false
	for _, line := range strings.Split(wide, "\n") {
		if strings.Contains(line, upperName) && strings.Contains(line, "ACTIVITY") {
			sideBySide = true
			break
		}
	}
	if !sideBySide {
		t.Fatalf("wide layout should place meta and activity side-by-side:\n%s", wide)
	}

	// Narrow: panels stack, so no single line carries content from both
	// columns — the name row never shares with any activity body.
	m.width = 40
	narrow := m.renderProjectView()
	for _, line := range strings.Split(narrow, "\n") {
		if !strings.Contains(line, project.Name) {
			continue
		}
		for _, body := range []string{"pinned cover sheet", "project handoff body", "universal body"} {
			if strings.Contains(line, body) {
				t.Fatalf("narrow layout should stack meta above activity, not share a row:\n%s", narrow)
			}
		}
	}
}
