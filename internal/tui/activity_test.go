package tui

import (
	"context"
	"fmt"
	"omakiten/internal/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
)

func TestActivityCursorMovesAndScrolls(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "with comments", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	// Seed enough comments to require scroll inside the activity column.
	for i := 0; i < 10; i++ {
		if _, err := store.AddComment(ctx, project.ID, task.ID, "comment", "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	// Open the task detail view; activity feed is loaded into m.activity.
	got := pressKey(t, model, tea.KeyEnter)
	if got.activityCursor != -1 {
		t.Fatalf("initial activityCursor = %d, want -1", got.activityCursor)
	}

	// First J should land on the first card (cursor=0). Subsequent Js advance
	// — and once they exceed the viewport, activityScroll auto-follows.
	for i := 0; i < 6; i++ {
		got = pressRune(t, got, 'J')
	}
	if got.activityCursor < 5 {
		t.Errorf("activityCursor after 6×J = %d, want >= 5", got.activityCursor)
	}
	// activityScroll is now line-based; we only assert the cursor advanced.
	// Viewport-following is exercised end-to-end via the View output in the
	// expand test below.
}

func TestActivityEnterOpensCommentScreen(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "long body", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}

	// Body that exceeds commentCardLineLimit so the cap+hint kicks in.
	longBody := strings.Repeat("line\n", 12)
	comment, err := store.AddComment(ctx, project.ID, task.ID, longBody, "human", nil)
	if err != nil {
		t.Fatalf("AddComment() = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	view := got.View()
	if !strings.Contains(view, "more lines — enter opens") {
		t.Fatalf("collapsed view should show open-detail hint:\n%s", view)
	}

	// Tab switches focus to the activity column so j/k navigate cards and
	// enter opens the comment detail screen. task.created is at index 0 (asc);
	// pressing j once advances to the comment at index 1.
	got = pressKey(t, got, tea.KeyTab)
	got = pressRune(t, got, 'j')
	got = pressKey(t, got, tea.KeyEnter)
	if !got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = false after enter, want true")
	}
	if got.commentScreenID != comment.ID {
		t.Fatalf("commentScreenID = %d, want %d", got.commentScreenID, comment.ID)
	}

	// The dedicated screen renders the full body without the cap hint, with
	// its own kicker and footer hint so the UX context is unmistakable.
	view = got.View()
	if strings.Contains(view, "more lines — enter opens") {
		t.Fatalf("comment screen should drop the cap hint:\n%s", view)
	}
	// kicker uppercases ("// COMMENT · #N"), so assert on the uppercased form.
	if !strings.Contains(view, fmt.Sprintf("COMMENT · #%d", comment.ID)) {
		t.Fatalf("comment screen header missing:\n%s", view)
	}

	// j/k must scroll the body within the comment screen, not move between
	// activity cards (that's the bug we're fixing — long comments need an
	// in-screen scroll so the top and bottom are reachable).
	before := got.commentScreen.Viewport.Scroll
	got = pressRune(t, got, 'j')
	if got.commentScreen.Viewport.Scroll <= before {
		t.Fatalf("j did not advance commentScreen viewport: before=%d after=%d", before, got.commentScreen.Viewport.Scroll)
	}

	// esc returns to the task view, leaving the activity cursor on the same
	// comment so the user lands back where they were.
	got = pressKey(t, got, tea.KeyEsc)
	if got.commentScreenOpen {
		t.Fatalf("commentScreenOpen = true after esc, want false")
	}
	if got.taskScreen != taskScreenView {
		t.Fatalf("taskScreen = %v after esc, want taskScreenView", got.taskScreen)
	}
}

func TestCommentScreenIgnoresSystemEvents(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "no comments", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	// Open task view; the only activity event is task.created (a system
	// event). Tab + j puts the cursor on it; Enter must NOT open the
	// comment screen since system events have no body.
	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)
	got = pressKey(t, got, tea.KeyEnter)
	if got.commentScreenOpen {
		t.Fatalf("Enter on a system event opened the comment screen — system events have no body to read")
	}
}

func TestTabTogglesTaskFocus(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "with comment", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	if _, err := store.AddComment(ctx, project.ID, task.ID, "hi", "human", nil); err != nil {
		t.Fatalf("AddComment() = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 40
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	if got.taskFocus != taskFocusForm {
		t.Fatalf("default focus = %v, want taskFocusForm", got.taskFocus)
	}

	got = pressKey(t, got, tea.KeyTab)
	if got.taskFocus != taskFocusActivity {
		t.Fatalf("focus after tab = %v, want taskFocusActivity", got.taskFocus)
	}
	if got.activityCursor != 0 {
		t.Errorf("activityCursor on entering activity = %d, want 0 (auto-land on first card)", got.activityCursor)
	}

	got = pressKey(t, got, tea.KeyTab)
	if got.taskFocus != taskFocusForm {
		t.Fatalf("focus after second tab = %v, want taskFocusForm", got.taskFocus)
	}
	if got.activityCursor != -1 {
		t.Errorf("activityCursor after returning focus = %d, want -1 (cleared)", got.activityCursor)
	}
}

// TestActivityScrollKeepsFocusedCardVisible reproduces task #74: navigating
// j past the activity viewport with the (pre-fix) sync routine left the
// focused card hidden behind the "▼ N below" hint row because sync compared
// against the raw viewport while renderScrollWindowSplit reserved up to two
// rows for the split hints. Each comment carries a unique body so the test
// can assert the focused one renders inside the visible region instead of
// asserting on activityScroll directly.
func TestActivityScrollKeepsFocusedCardVisible(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "lots of activity", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	const total = 30
	for i := 0; i < total; i++ {
		body := fmt.Sprintf("activity-marker-%02d", i)
		if _, err := store.AddComment(ctx, project.ID, task.ID, body, "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 30
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)

	for n := 0; n < total/2; n++ {
		got = pressRune(t, got, 'j')
	}
	if got.activityCursor < 0 {
		t.Fatalf("activityCursor = %d after j×%d, want >= 0", got.activityCursor, total/2)
	}

	events := got.activityForTaskInView(got.taskID)
	if got.activityCursor >= len(events) {
		t.Fatalf("activityCursor = %d out of range (%d events)", got.activityCursor, len(events))
	}
	focused := events[got.activityCursor]
	body := stripANSI(got.View())
	if focused.Body == "" {
		t.Fatalf("focused event has empty body — picked the wrong cursor")
	}
	if !strings.Contains(body, focused.Body) {
		t.Fatalf("focused card %q hidden after navigation; rendered view did not contain it.\n--- view ---\n%s", focused.Body, body)
	}
}

// TestActivityScrollResyncsOnResize covers AC#2 — shrinking the terminal
// height after navigating must keep the focused card visible. Without the
// resize-time sync, `activityScroll` keeps the offset that was valid for
// the old viewport and the focused card slides off-screen.
func TestActivityScrollResyncsOnResize(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "resize repro", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	const total = 25
	for i := 0; i < total; i++ {
		body := fmt.Sprintf("resize-marker-%02d", i)
		if _, err := store.AddComment(ctx, project.ID, task.ID, body, "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks: store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow: app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = 50
	model.width = 160

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)
	for n := 0; n < total/2; n++ {
		got = pressRune(t, got, 'j')
	}

	resized, _ := got.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	got, ok := resized.(Model)
	if !ok {
		t.Fatalf("Update(WindowSizeMsg) returned %T, want Model", resized)
	}

	events := got.activityForTaskInView(got.taskID)
	focused := events[got.activityCursor]
	body := stripANSI(got.View())
	if !strings.Contains(body, focused.Body) {
		t.Fatalf("focused card %q hidden after resize.\n--- view ---\n%s", focused.Body, body)
	}
}

// newActivityTestModel seeds a project + task + N human comments and
// returns a Model wired with the standard test theme / counters at the
// given terminal height. bodyFn produces the comment body for index i so
// callers can encode positional markers (e.g. "page-marker-03") used to
// assert what is or isn't on screen. The returned `bodies` slice mirrors
// the seed order so callers can address `bodies[0]` and `bodies[len-1]`
// without re-querying the store.
func newActivityTestModel(t *testing.T, height, commentCount int, bodyFn func(int) string) (Model, []string) {
	t.Helper()
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() = %v", err)
	}
	task, err := store.CreateTask(ctx, project.ID, "activity-test", "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask() = %v", err)
	}
	bodies := make([]string, commentCount)
	for i := 0; i < commentCount; i++ {
		body := bodyFn(i)
		bodies[i] = body
		if _, err := store.AddComment(ctx, project.ID, task.ID, body, "human", nil); err != nil {
			t.Fatalf("AddComment(%d) = %v", i, err)
		}
	}
	cfg := config.MustLoadKitConfig()
	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Events:       store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, cfg.Priorities, cfg.Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() = %v", err)
	}
	model.height = height
	model.width = 160
	return model, bodies
}

// TestActivityJAfterPageScrollDoesNotSnapBack reproduces the bug filed as
// task #125: pgdown is documented to scroll the activity body independently
// of the cursor, but the first subsequent j re-synced the scroll to the
// (still-at-the-top) cursor, throwing away the user's page-scroll work.
// After the fix, j snaps the cursor to the first visible card and keeps the
// viewport anchored there so navigation continues from where the user is
// looking — not from a stale offset behind the scroll.
func TestActivityJAfterPageScrollDoesNotSnapBack(t *testing.T) {
	model, _ := newActivityTestModel(t, 30, 30, func(i int) string {
		return fmt.Sprintf("page-marker-%02d", i)
	})

	// Open task, Tab into activity column. Cursor lands on first card.
	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)
	if got.activityCursor != 0 {
		t.Fatalf("activityCursor after Tab = %d, want 0", got.activityCursor)
	}

	// pgdown twice — enough to push the original cursor out of view.
	got = pressKey(t, got, tea.KeyPgDown)
	got = pressKey(t, got, tea.KeyPgDown)
	scrollAfterPage := got.activityLines.Scroll()
	if scrollAfterPage == 0 {
		t.Fatalf("pgdown did not advance activityScroll: still 0 after 2 pgdown presses")
	}

	// Capture which card is visible at the top of the viewport before j.
	events := got.activityForTaskInView(got.taskID)
	cards := got.activityRowsForRender(events)
	ranges := cardLineRanges(cards)
	firstVisible := -1
	for i, r := range ranges {
		if r.start >= got.activityLines.Scroll() {
			firstVisible = i
			break
		}
	}
	if firstVisible <= 0 {
		t.Fatalf("expected pgdown to push first-visible card past index 0, got %d", firstVisible)
	}

	// j after pgdown. Bug: scroll snaps back to cardTop[1] (top of feed),
	// throwing away the user's pgdown. Fix: cursor anchors to first
	// visible card, scroll stays put.
	got = pressRune(t, got, 'j')
	if got.activityLines.Scroll() < scrollAfterPage {
		t.Fatalf("j after pgdown regressed activityScroll: was %d, now %d (snap-back bug)", scrollAfterPage, got.activityLines.Scroll())
	}
	if got.activityCursor < firstVisible {
		t.Fatalf("activityCursor after j = %d, want >= %d (first card visible after pgdown)", got.activityCursor, firstVisible)
	}

	// The card that's now focused must render inside the visible viewport
	// — that's the user-facing contract: navigation always lands on
	// something visible.
	focused := events[got.activityCursor]
	body := stripANSI(got.View())
	if !strings.Contains(body, focused.Body) {
		t.Fatalf("focused card %q hidden after j-following-pgdown.\n--- view ---\n%s", focused.Body, body)
	}
}

// TestActivityJAfterPageUpFromBottomAnchorsToLastVisible exercises the
// mirror of the snap-back fix: when the cursor sits at the END of the
// feed and the user pgup'd far enough to push that cursor BELOW the
// viewport, the next j must anchor to the last visible card (not the
// first), otherwise the cursor jumps backward on a "next" key. The
// previous direction-gated anchor (`if delta > 0: cursor = first`)
// would have moved the focus to the top of the viewport — the wrong
// direction for j.
func TestActivityJAfterPageUpFromBottomAnchorsToLastVisible(t *testing.T) {
	const total = 30
	model, _ := newActivityTestModel(t, 30, total, func(i int) string {
		return fmt.Sprintf("page-marker-%02d", i)
	})

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)

	// Walk the cursor to the last card with j; sync follows so the
	// viewport sits near max scroll.
	for i := 0; i < total-1; i++ {
		got = pressRune(t, got, 'j')
	}
	if got.activityCursor != total-1 {
		t.Fatalf("activityCursor after j×%d = %d, want %d", total-1, got.activityCursor, total-1)
	}
	scrollAtBottom := got.activityLines.Scroll()

	// pgup repeatedly until cursor falls past the bottom of the viewport
	// — the body keeps scrolling up while the cursor stays pinned at the
	// last card.
	for i := 0; i < 4; i++ {
		got = pressKey(t, got, tea.KeyPgUp)
	}
	first, last, ok := got.visibleActivityCardRange()
	if !ok {
		t.Fatalf("visibleActivityCardRange returned ok=false; cannot reproduce scenario")
	}
	if got.activityCursor <= last {
		t.Fatalf("pgup did not push cursor below viewport: cursor=%d last-visible=%d", got.activityCursor, last)
	}
	scrollAfterPgup := got.activityLines.Scroll()
	if scrollAfterPgup >= scrollAtBottom {
		t.Fatalf("pgup did not reduce activityScroll: was %d, still %d", scrollAtBottom, scrollAfterPgup)
	}

	// j with cursor below viewport. Pre-fix: anchored to `first` (jumped
	// the cursor backward by many cards on a "next" key). Post-fix:
	// anchors to `last`, keeping cursor near where the user is looking
	// without retreating to the top of the viewport.
	got = pressRune(t, got, 'j')
	if got.activityCursor != last {
		t.Fatalf("j after pgup-from-bottom: activityCursor = %d, want %d (last visible card)", got.activityCursor, last)
	}
	if got.activityCursor == first && first != last {
		t.Fatalf("j after pgup-from-bottom landed on FIRST visible (%d) — backward jump on next-key", first)
	}
	// The fix is "don't snap back to where cursor=29 left the scroll".
	// Follow may still nudge scroll a few rows to bring the focused
	// card's tail into view (that's intentional UX), but it must stay
	// well clear of the pre-pgup max — otherwise the user's page-scroll
	// progress was thrown away.
	if got.activityLines.Scroll() >= scrollAtBottom {
		t.Fatalf("j after pgup-from-bottom snap-back: scroll regressed to %d (≥ pre-pgup %d)", got.activityLines.Scroll(), scrollAtBottom)
	}
}

// TestActivityLastCardReachableAtEndScroll is the symptom the user filed
// with the screenshot for task #125: G (or sustained pgdown / j-to-last)
// must leave the LAST event card's last line visible. The pre-fix
// `maxScroll = len(body) - viewport` ignored the row that
// renderScrollWindowSplit reserves for the "▲ N above" hint when scrolled
// past the top — that 1-row deficit cropped the tail of the bottom card
// so the focused border framed empty space while "▼ 2 below" lied about
// nothing being there to reach.
func TestActivityLastCardReachableAtEndScroll(t *testing.T) {
	model, bodies := newActivityTestModel(t, 35, 24, func(i int) string {
		return fmt.Sprintf("end-marker-%02d", i)
	})
	lastBody := bodies[len(bodies)-1]

	got := pressKey(t, model, tea.KeyEnter)
	got = pressKey(t, got, tea.KeyTab)

	// G jumps activityScroll to the sentinel; clampActivityScroll then
	// must leave the last card reachable inside the viewport budget.
	got = pressRune(t, got, 'G')

	body := stripANSI(got.View())
	if !strings.Contains(body, lastBody) {
		t.Fatalf("last event body %q missing after G; activityScroll=%d\n--- view ---\n%s", lastBody, got.activityLines.Scroll(), body)
	}
}

// TestActivityPanelFitsTerminalHeight asserts the activity panel's bottom
// border lands inside the terminal viewport. The chrome budget in
// activityViewportLines must leave room for: screen header + leading
// blank + panel borders + kicker + footer + the "\n" applyTaskViewScroll
// prepends. If the budget is off by even one row the bottom border (or
// the focused last card) gets clipped — symptom is the activity column
// looking unbordered at the bottom.
func TestActivityPanelFitsTerminalHeight(t *testing.T) {
	model, _ := newActivityTestModel(t, 30, 24, func(i int) string {
		return fmt.Sprintf("fit-%02d", i)
	})

	for _, h := range []int{30, 35, 45, 60} {
		model.height = h
		model.width = 160

		// Scenario A: just Tab. Cursor lands on first card, scroll=0.
		// Bottom border must still fit even when scroll has 100+ lines
		// below — this is the configuration in the user's screenshot
		// where the box bottom did not render.
		got := pressKey(t, model, tea.KeyEnter)
		got = pressKey(t, got, tea.KeyTab)
		viewTop := stripANSI(got.View())
		linesTop := strings.Split(viewTop, "\n")
		if len(linesTop) > h {
			t.Errorf("h=%d top: View() rendered %d lines, must not exceed terminal height", h, len(linesTop))
		}
		hasBottom := false
		for _, line := range linesTop {
			if strings.Contains(line, "└") && strings.Contains(line, "┘") {
				hasBottom = true
				break
			}
		}
		if !hasBottom {
			t.Errorf("h=%d top: bottom border missing with cursor=0 (▼ below state)\n--- view ---\n%s", h, viewTop)
		}

		// Scenario B: G + j×31 → cursor on last card.
		got = pressRune(t, got, 'G')
		got = pressRune(t, got, 'j')
		for n := 0; n < 30; n++ {
			got = pressRune(t, got, 'j')
		}

		view := stripANSI(got.View())
		lines := strings.Split(view, "\n")
		if len(lines) > h {
			t.Errorf("h=%d: View() rendered %d lines, must not exceed terminal height", h, len(lines))
		}
		// Activity box bottom border must land inside the rendered output,
		// not clipped by clampViewToHeight.
		hasBottomBorder := false
		for _, line := range lines {
			if strings.Contains(line, "└") && strings.Contains(line, "┘") {
				hasBottomBorder = true
				break
			}
		}
		if !hasBottomBorder {
			t.Errorf("h=%d: activity panel bottom border ('└...┘') missing from rendered view — clipped\n--- view ---\n%s", h, view)
		}
		// Bottom border must sit ABOVE the footer; if it's on the final
		// rendered row, terminals that reserve a trailing cursor row will
		// clip it. We assert at least one footer line lives after the
		// border in the rendered output.
		lastBorderIdx := -1
		for i, line := range lines {
			if strings.Contains(line, "└") && strings.Contains(line, "┘") {
				lastBorderIdx = i
			}
		}
		if lastBorderIdx == len(lines)-1 {
			t.Errorf("h=%d: bottom border is the last rendered row (line %d/%d). Add chrome margin so footer follows.\n--- view ---\n%s", h, lastBorderIdx, len(lines), view)
		}
	}
}

func TestActivityPanelGrowsWithAvailableWidth(t *testing.T) {
	// Skip NewModel — its refresh path needs a fully wired repo set. We only
	// exercise pure sizing math, so a zero-value Model with width set is enough.
	model := Model{}

	model.width = 80
	narrow := model.activityPanelWidth()

	model.width = 200
	wide := model.activityPanelWidth()

	if wide <= narrow {
		t.Fatalf("activityPanelWidth did not grow with width: narrow=%d wide=%d", narrow, wide)
	}
	if narrow < taskCommentsPanelMinWidth {
		t.Errorf("narrow width %d below floor %d", narrow, taskCommentsPanelMinWidth)
	}
	if wide > taskCommentsPanelMaxWidth {
		t.Errorf("wide width %d above ceiling %d", wide, taskCommentsPanelMaxWidth)
	}
}
