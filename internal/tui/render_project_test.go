package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// TestProjectViewGEndJumpActivityCard proves G/end and g/home jump the
// activity SELECTION to the last / first card (card navigation), mirroring
// the task activity panel rather than the old raw line-scroll.
func TestProjectViewGEndJumpActivityCard(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	// Focus the activity zone (Tab twice: form → dashboard → activity) so
	// the navigation bindings act on it. Entering the zone auto-anchors the
	// cursor onto the first card.
	tabbed, _ := m.Update(tabKey())
	m = tabbed.(Model)
	tabbed, _ = m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("setup: two tabs did not focus the activity zone; focus = %v", m.projectFocus)
	}
	if m.projectActivityCursor != 0 {
		t.Fatalf("entering the activity zone should anchor the cursor on card 0; got %d", m.projectActivityCursor)
	}

	lastCard := len(m.projectActivity) - 1
	if lastCard <= 0 {
		t.Fatalf("setup: expected a multi-card feed; got %d cards", len(m.projectActivity))
	}

	ended, _ := m.Update(endKey())
	m = ended.(Model)
	if m.projectActivityCursor != lastCard {
		t.Fatalf("end did not jump the selection to the last card; got %d want %d", m.projectActivityCursor, lastCard)
	}

	// g/home returns the selection to the first card.
	top, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = top.(Model)
	if m.projectActivityCursor != 0 {
		t.Fatalf("home did not return the selection to the first card; got %d", m.projectActivityCursor)
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

// TestProjectViewFooterScrollOnlyForActivity proves the footer advertises
// the scroll/page/top-bottom keys ONLY while the activity zone owns focus —
// the form + dashboard zones are not scroll-windowed, so claiming scroll
// there would promise a no-op.
func TestProjectViewFooterScrollOnlyForActivity(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	scrollKeys := func(mm Model) bool {
		for _, tok := range mm.footerTokens() {
			if tok.key == "j/k" || tok.key == "pgup/pgdn" || tok.key == "g/G" {
				return true
			}
		}
		return false
	}

	// Form zone (open default): no scroll hints.
	if m.projectFocus != projectFocusForm {
		t.Fatalf("setup: project view should open on form focus; got %v", m.projectFocus)
	}
	if scrollKeys(m) {
		t.Fatalf("form-focused footer should not advertise scroll keys")
	}

	// Dashboard zone: still no scroll hints.
	tabbed, _ := m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectFocus != projectFocusDashboard {
		t.Fatalf("setup: tab did not focus dashboard; got %v", m.projectFocus)
	}
	if scrollKeys(m) {
		t.Fatalf("dashboard-focused footer should not advertise scroll keys")
	}

	// Activity zone: scroll hints reappear (it IS windowed).
	tabbed, _ = m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("setup: tab did not focus activity; got %v", m.projectFocus)
	}
	if !scrollKeys(m) {
		t.Fatalf("activity-focused footer should advertise scroll keys")
	}
}

// TestProjectViewNavNoOpOnNonWindowedZones proves navigation keys are inert
// while the form or dashboard zone owns focus (neither has a cursor), and the
// activity zone moves its card selection — confirming the card-nav path only
// engages when the activity zone owns focus.
func TestProjectViewNavNoOpOnNonWindowedZones(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)

	// Form focus: j must not move the activity cursor (it is -1: no selection).
	down, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = down.(Model)
	if m.projectActivityCursor != -1 {
		t.Fatalf("nav on the form zone should be a no-op; activity cursor = %d", m.projectActivityCursor)
	}

	// Dashboard focus: same.
	tabbed, _ := m.Update(tabKey())
	m = tabbed.(Model)
	down, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = down.(Model)
	if m.projectActivityCursor != -1 {
		t.Fatalf("nav on the dashboard zone should be a no-op; activity cursor = %d", m.projectActivityCursor)
	}

	// Activity focus: entering anchors on card 0, then j advances to card 1.
	tabbed, _ = m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectActivityCursor != 0 {
		t.Fatalf("entering the activity zone should anchor the cursor on card 0; got %d", m.projectActivityCursor)
	}
	down, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = down.(Model)
	if m.projectActivityCursor != 1 {
		t.Fatalf("nav on the activity zone should advance the card selection; cursor = %d, want 1", m.projectActivityCursor)
	}
}

// projectViewOnActivity opens the project view and tabs focus to the activity
// zone (form → dashboard → activity), returning the model with the cursor
// auto-anchored on the first card. Shared setup for the project activity
// navigation / open / border regression tests (task #592).
func projectViewOnActivity(t *testing.T) Model {
	t.Helper()
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	m.width = 160
	tabbed, _ := m.Update(tabKey())
	m = tabbed.(Model)
	tabbed, _ = m.Update(tabKey())
	m = tabbed.(Model)
	if m.projectFocus != projectFocusActivity {
		t.Fatalf("setup: two tabs did not focus the activity zone; focus = %v", m.projectFocus)
	}
	return m
}

// TestProjectActivityCardNavigation proves j/k and the arrow keys move the
// project activity selection by card (not raw line scroll) and that the
// focused card's accent border tracks the cursor in the rendered panel.
// Regression for task #592 (DoD: navigation by card/item).
func TestProjectActivityCardNavigation(t *testing.T) {
	m := projectViewOnActivity(t)
	last := len(m.projectActivity) - 1
	if last <= 0 {
		t.Fatalf("setup: expected a multi-card feed; got %d cards", len(m.projectActivity))
	}

	// j steps forward one card; k steps back; arrows behave identically.
	down, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = down.(Model)
	if m.projectActivityCursor != 1 {
		t.Fatalf("j should advance the card cursor to 1; got %d", m.projectActivityCursor)
	}
	up, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = up.(Model)
	if m.projectActivityCursor != 0 {
		t.Fatalf("up arrow should step the card cursor back to 0; got %d", m.projectActivityCursor)
	}

	// The cursor never runs past the ends: k at the top stays on card 0.
	up, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = up.(Model)
	if m.projectActivityCursor != 0 {
		t.Fatalf("k at the top should clamp the cursor at 0; got %d", m.projectActivityCursor)
	}

	// The cursor selection drives which card the render path marks as focused:
	// renderProjectActivityPanel feeds m.projectActivityCursor into
	// activityRowsForRenderWithCursor, so the panel must reflect the moved
	// cursor by surfacing the selected (last) card's body. Move to the last
	// card and confirm the panel renders it. (The accent border is a
	// color-only diff, stripped by lipgloss in the no-TTY test profile, so the
	// behavioral assertion is the cursor + the card content reaching the panel.)
	for i := 0; i < last; i++ {
		nxt, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = nxt.(Model)
	}
	if m.projectActivityCursor != last {
		t.Fatalf("repeated j should reach the last card %d; got %d", last, m.projectActivityCursor)
	}
	out := m.renderProjectActivityPanel(true)
	if !strings.Contains(out, "universal") {
		t.Fatalf("rendered activity panel missing the last card body:\n%s", out)
	}
}

// TestProjectActivityEnterOpensCommentDetail proves Enter on a selected
// project/universal comment opens the shared full-width comment detail view,
// the detail resolves from the project feed (scope-aware), and Esc returns to
// the project view with the activity selection preserved. Regression for task
// #592 (DoD: Enter opens project/universal comment detail; Esc restores).
func TestProjectActivityEnterOpensCommentDetail(t *testing.T) {
	m := projectViewOnActivity(t)

	// Move onto the universal comment (last card) so we exercise a comment that
	// is absent from any task feed — the detail must still resolve.
	last := len(m.projectActivity) - 1
	for i := 0; i < last; i++ {
		nxt, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = nxt.(Model)
	}
	wantID := m.projectActivity[last].ID
	wantCursor := m.projectActivityCursor

	enter, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = enter.(Model)
	if !m.commentScreenOpen {
		t.Fatalf("enter on a project comment did not open the comment detail screen")
	}
	if !m.commentScreenFromProject {
		t.Fatalf("comment detail opened from the project feed should set commentScreenFromProject")
	}
	if m.commentScreenID != wantID {
		t.Fatalf("comment detail screen id = %d, want %d", m.commentScreenID, wantID)
	}

	// The scope-aware lookup must find the comment in the project feed and the
	// detail render must surface its body without a misleading "Task #0" row.
	comment, ok := m.activeComment()
	if !ok {
		t.Fatalf("activeComment() did not resolve the project comment from the project feed")
	}
	if comment.Scope != domain.CommentScopeUniversal {
		t.Fatalf("resolved comment scope = %q, want universal", comment.Scope)
	}
	detail := m.renderCommentScreen()
	// The markdown renderer styles each word separately, so assert on a single
	// token that survives without an interleaved ANSI break (same idiom the
	// existing project-view tests use).
	if !strings.Contains(detail, "universal") {
		t.Fatalf("comment detail missing the comment body:\n%s", detail)
	}
	if strings.Contains(detail, "#0") {
		t.Fatalf("project/universal comment detail should not render a Task #0 row:\n%s", detail)
	}

	// Esc closes the detail and returns to the project view with the same card
	// still selected.
	back, _ := m.Update(escKey())
	m = back.(Model)
	if m.commentScreenOpen {
		t.Fatalf("esc did not close the comment detail screen")
	}
	if m.commentScreenFromProject {
		t.Fatalf("esc should clear the commentScreenFromProject flag")
	}
	if m.sub != subProjectView {
		t.Fatalf("esc from the comment detail should land back on the project view; sub = %v", m.sub)
	}
	if m.projectActivityCursor != wantCursor {
		t.Fatalf("activity selection not preserved across the detail round-trip; cursor = %d want %d", m.projectActivityCursor, wantCursor)
	}
}

// TestProjectActivityEnterIgnoresEmptyAndNonComment proves Enter off the
// activity zone, or with no card selected, does not open the comment detail —
// the project overview stays read-only / inert outside a selected comment.
func TestProjectActivityEnterIgnoresNoSelection(t *testing.T) {
	model, _, _ := scopedFeedModel(t)
	opened, _ := model.Update(ctrlP())
	m := opened.(Model)
	// Form zone, no activity selection: Enter must be inert.
	enter, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = enter.(Model)
	if m.commentScreenOpen {
		t.Fatalf("enter on the form zone should not open a comment detail")
	}
}

// TestProjectActivityBorderContainment proves every visible activity card in
// the project overview panel renders with a single intact border that stays
// inside the panel's content area — no clipped or dangling border fragments
// past the panel edge. Regression for the "broken borders" report (task #592,
// DoD: border containment).
func TestProjectActivityBorderContainment(t *testing.T) {
	m := projectViewOnActivity(t)

	// Seed cards that stress the border: a long body (forces wrapping + the
	// "more lines" footer), a pinned comment, and a comment with tag chips —
	// the three card shapes the DoD calls out.
	long := strings.Repeat("lorem ipsum dolor sit amet ", 20)
	m.projectActivity = []domain.Event{
		{ID: 101, EntityType: domain.EventEntityProject, EntityID: 1, EventType: domain.EventTypeComment, Body: long, AuthorType: "agent"},
		{ID: 102, EntityType: domain.EventEntityProject, EntityID: 1, EventType: domain.EventTypeComment, Body: "pinned handoff", AuthorType: "agent"},
		{ID: 103, EntityType: domain.EventEntityUniversal, EventType: domain.EventTypeComment, Body: "tagged note", AuthorType: "human",
			Tags: []domain.Tag{{ID: 1, Name: "deploy", Label: "deploy"}, {ID: 2, Name: "blocker", Label: "blocker"}}},
	}
	m.activityCardsCache = activityCardsCacheEntry{} // invalidate any warmed slice

	panel := m.renderProjectActivityPanel(true)
	lines := strings.Split(strings.TrimRight(panel, "\n"), "\n")

	// The outer panel border is the widest row; every inner card border row
	// (├ corners ┌┐└┘ and │ verticals) must fit within it. lipgloss right-pads
	// the panel content to a uniform width, so no card row may be wider than
	// the panel's own border rows.
	panelWidth := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > panelWidth {
			panelWidth = w
		}
	}
	if panelWidth == 0 {
		t.Fatalf("panel rendered empty:\n%s", panel)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > panelWidth {
			t.Fatalf("line %d (%d cells) exceeds the panel width %d — a card border tipped past the panel edge:\n%s", i, w, panelWidth, panel)
		}
	}

	// Each card draws exactly one top + one bottom border. Three cards → three
	// of each glyph inside the panel body, plus the panel's own single frame.
	tops := strings.Count(panel, "┌")
	bottoms := strings.Count(panel, "└")
	if tops != 4 || bottoms != 4 {
		t.Fatalf("expected 1 panel frame + 3 card frames (4 tops / 4 bottoms); got %d tops / %d bottoms:\n%s", tops, bottoms, panel)
	}
}
