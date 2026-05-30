package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

func ctrlP() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyCtrlP} }
func tabKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }
func endKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnd} }
func fKey() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}} }

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
	if got.projectFocus != projectFocusForm {
		t.Fatalf("project view should open focused on the form; focus = %v", got.projectFocus)
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

// TestProjectViewTabTogglesFocus proves Tab cycles the focused zone
// form → dashboard → activity → form inside the project view, mirroring
// the task view's three-zone rotation.
func TestProjectViewTabTogglesFocus(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	if m.projectFocus != projectFocusForm {
		t.Fatalf("setup: project view should open on form focus")
	}

	next, _ := m.Update(tabKey())
	m = next.(Model)
	if m.projectFocus != projectFocusDashboard {
		t.Fatalf("tab did not move focus to dashboard; focus = %v", m.projectFocus)
	}

	next, _ = m.Update(tabKey())
	m = next.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("second tab did not move focus to activity; focus = %v", m.projectFocus)
	}

	next, _ = m.Update(tabKey())
	m = next.(Model)
	if m.projectFocus != projectFocusForm {
		t.Fatalf("third tab did not wrap focus back to the form; focus = %v", m.projectFocus)
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

	// Focus the activity zone (Tab twice: form → dashboard → activity) so
	// the scroll bindings act on it.
	tabbed, _ := m.Update(tabKey())
	m = tabbed.(Model)
	tabbed, _ = m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("setup: two tabs did not focus the activity zone; focus = %v", m.projectFocus)
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

// TestProjectViewDashboardCounts proves the status dashboard zone surfaces
// the per-bucket task counts + total, the root/sub-task split, and the
// aggregate plan progress computed by computeProjectDashboard.
func TestProjectViewDashboardCounts(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160

	// Seed a deterministic task mix: 2 roots in backlog, 1 child of root #1
	// in a second lane. The workflow buckets drive the per-bucket rows.
	m.workflow.Buckets = []domain.Bucket{{Key: "backlog", Name: "Backlog"}, {Key: "doing", Name: "Doing"}}
	parentID := int64(1)
	m.tasks = []domain.Task{
		{ID: 1, Title: "Root A", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 2, Title: "Root B", BucketKey: "backlog", Priority: domain.Priority(2)},
		{ID: 3, Title: "Child", BucketKey: "doing", Priority: domain.Priority(2), ParentID: &parentID},
	}
	m.cachedTasksByBucket = nil
	m.projectDashboard = m.computeProjectDashboard()

	d := m.projectDashboard
	if d.totalTasks != 3 {
		t.Fatalf("dashboard total = %d, want 3", d.totalTasks)
	}
	if d.rootTasks != 2 || d.subTasks != 1 {
		t.Fatalf("dashboard root/sub split = %d/%d, want 2/1", d.rootTasks, d.subTasks)
	}
	if len(d.bucketCounts) != 2 {
		t.Fatalf("dashboard bucketCounts = %d, want 2 (one per workflow bucket)", len(d.bucketCounts))
	}
	if d.bucketCounts[0].count != 2 || d.bucketCounts[1].count != 1 {
		t.Fatalf("dashboard per-bucket counts = backlog:%d doing:%d, want 2/1", d.bucketCounts[0].count, d.bucketCounts[1].count)
	}

	out := m.renderProjectDashboardPanel(true, m.projectMetaPanelWidth())
	for _, want := range []string{
		strings.ToUpper(m.t("tui.kicker.dashboard")),
		strings.ToUpper(m.t("tui.dashboard.tasks")),
		strings.ToUpper(m.t("tui.dashboard.total")),
		strings.ToUpper(m.t("tui.dashboard.subtasks")),
		strings.ToUpper(m.t("tui.dashboard.plans")),
		"BACKLOG",
		"DOING",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dashboard panel missing %q:\n%s", want, out)
		}
	}
}

// TestProjectViewDashboardRendersInView proves the dashboard zone is wired
// into the composed project view (not just the isolated panel renderer).
func TestProjectViewDashboardRendersInView(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160

	out := m.renderProjectView()
	if !strings.Contains(out, strings.ToUpper(m.t("tui.kicker.dashboard"))) {
		t.Fatalf("project view missing dashboard zone:\n%s", out)
	}
}

// TestProjectDescriptionCapped proves a long project description is elided
// in the form zone to taskDescriptionInlineCap lines plus a "+N more" cue,
// rather than overflowing the zone (the #390 warning the rework subsumes).
func TestProjectDescriptionCapped(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160

	// A body well over the cap so the inline render must truncate + cue.
	bodyLines := make([]string, 0, taskDescriptionInlineCap+8)
	for i := 0; i < taskDescriptionInlineCap+8; i++ {
		bodyLines = append(bodyLines, "line-of-the-project-description")
	}
	m.projectDescription = strings.Join(bodyLines, "\n")

	inline := m.renderProjectDescriptionInline(80)
	gotLines := strings.Count(inline, "\n") + 1
	// cap lines + 1 cue line.
	if gotLines > taskDescriptionInlineCap+1 {
		t.Fatalf("capped description rendered %d lines, want <= %d", gotLines, taskDescriptionInlineCap+1)
	}
	// The cue carries the "+N more" formatting (the f-to-focus hint).
	if !strings.Contains(inline, "more") && !strings.Contains(inline, "+") {
		t.Fatalf("capped description missing the +N more cue:\n%s", inline)
	}
}

// TestProjectFormScreenOpensAndCloses proves `f` from the project view
// opens the fullscreen project form overlay (uncapped, scrollable
// description) and that `f`/esc close it back to the project view.
func TestProjectFormScreenOpensAndCloses(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160
	if m.projectFormScreenOpen {
		t.Fatalf("setup: form overlay should be closed on open")
	}

	pressed, _ := m.Update(fKey())
	m = pressed.(Model)
	if !m.projectFormScreenOpen {
		t.Fatalf("`f` did not open the project form overlay")
	}

	// The overlay renders the full metadata + the (uncapped) description.
	m.projectDescription = "FullProjectDescriptionMarker"
	out := m.renderProjectFormScreen()
	if !strings.Contains(out, "FullProjectDescriptionMarker") {
		t.Fatalf("form overlay missing the full description body:\n%s", out)
	}

	// `f` again closes it.
	closed, _ := m.Update(fKey())
	m = closed.(Model)
	if m.projectFormScreenOpen {
		t.Fatalf("second `f` did not close the project form overlay")
	}

	// esc also closes it.
	reopened, _ := m.Update(fKey())
	m = reopened.(Model)
	if !m.projectFormScreenOpen {
		t.Fatalf("setup: `f` did not reopen the overlay")
	}
	escd, _ := m.Update(escKey())
	m = escd.(Model)
	if m.projectFormScreenOpen {
		t.Fatalf("esc did not close the project form overlay")
	}
}

// TestProjectFormScreenBlocksPalette proves the fullscreen form overlay
// gates the global Ctrl+K palette, matching the description / plan-goal
// overlays.
func TestProjectFormScreenBlocksPalette(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	if !m.canOpenPalette() {
		t.Fatalf("setup: palette should be openable from the project view")
	}
	m.projectFormScreenOpen = true
	if m.canOpenPalette() {
		t.Fatalf("palette should be gated while the project form overlay is open")
	}
}
}

// TestRefreshProjectSummaryReloadsTasks proves refreshProjectSummary reloads
// the project's task set before folding the dashboard, so a task created
// after the board was last loaded shows up in the dashboard counts on the
// next refresh (`r`) — the dashboard is no longer stale.
func TestRefreshProjectSummaryReloadsTasks(t *testing.T) {
	model, project, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	before := m.projectDashboard.totalTasks

	// Create a new task directly in the store AFTER the project view opened.
	// Without the in-refresh reload, m.tasks (and thus the dashboard) would
	// keep the pre-create count.
	if _, err := m.repos.Tasks.CreateTask(context.Background(), project.ID, "Fresh", "", domain.Priority(2), "backlog", nil, m.repos.activeSnapshot()); err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}

	if err := m.refreshProjectSummary(); err != nil {
		t.Fatalf("refreshProjectSummary() = %v", err)
	}

	if got := m.projectDashboard.totalTasks; got != before+1 {
		t.Fatalf("dashboard total after refresh = %d, want %d (reflecting the new task)", got, before+1)
	}
}

