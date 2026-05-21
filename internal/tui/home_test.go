package tui

import (
	"context"
	"errors"
	"omakiten/internal/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/token"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/tui/components/notification"
)

// busyCheckpointer always fails Checkpoint with a pinned error so the
// TUI delete flow's auditWarn write path is exercised.
type busyCheckpointer struct {
	err   error
	calls int
}

func (b *busyCheckpointer) Checkpoint(context.Context) error {
	b.calls++
	return b.err
}

// TestNewModelWithEmptyProjectOpensHome covers AC1/AC14: launching the TUI
// without a resolvable project must land on the multi-project Home view
// instead of erroring.
func TestNewModelWithEmptyProjectOpensHome(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject(alpha) error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Bravo", "bravo", "/work/bravo"); err != nil {
		t.Fatalf("UpsertProject(bravo) error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.top != topHome {
		t.Fatalf("top = %d, want topHome (%d)", model.top, topHome)
	}

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "// PROJECTS · 2") {
		t.Fatalf("home should list 2 projects:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Alpha") || !strings.Contains(rendered, "Bravo") {
		t.Fatalf("home missing project names:\n%s", rendered)
	}
}

// TestHomeHidesTabBar covers AC8/AC15: the per-view tab bar is suppressed
// while on Home so tab/digit navigation never lands on Home and the surface
// reads as chromeless.
func TestHomeHidesTabBar(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	rendered := ansi.Strip(model.View())
	for _, label := range []string{"01 // TASKS", "02 // STATS", "03 // SETTINGS"} {
		if strings.Contains(rendered, label) {
			t.Fatalf("home should hide nav bar but found %q:\n%s", label, rendered)
		}
	}
	if !strings.Contains(rendered, "00 // HOME") {
		t.Fatalf("home header kicker missing:\n%s", rendered)
	}
}

// TestCtrlHReturnsToHome covers AC7: the ctrl+h binding goes back to Home
// from any per-project view.
func TestCtrlHReturnsToHome(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", "/work/project")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := NewModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Projects:     store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	if model.top != topTasks || model.sub != subBoard {
		t.Fatalf("(top, sub) = (%d, %d), want (topTasks, subBoard)", model.top, model.sub)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	got := updated.(Model)
	if got.top != topHome {
		t.Fatalf("top = %d after ctrl+h, want topHome (%d)", got.top, topHome)
	}
}

// TestHomeEnterSelectsProject covers AC6: pressing enter on a highlighted
// home card switches the model to the chosen project and lands on Board.
func TestHomeEnterSelectsProject(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject(alpha) error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.top != topTasks || got.sub != subBoard {
		t.Fatalf("(top, sub) = (%d, %d) after enter, want (topTasks, subBoard)", got.top, got.sub)
	}
	if got.project.Slug != "alpha" {
		t.Fatalf("project.Slug = %q, want %q", got.project.Slug, "alpha")
	}
	if got.LastProjectRoot() != "/work/alpha" {
		t.Fatalf("LastProjectRoot() = %q, want /work/alpha", got.LastProjectRoot())
	}
}

// TestCtrlHOnHomeReloads ensures ctrl+h while already on Home triggers a
// reload (refresh tags / pending counts) instead of being swallowed by
// the picker — the per-project handleCommonKey path is not reached when
// the model is already on viewHome, so home-side handling is required.
func TestCtrlHOnHomeReloads(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha"); err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,

		Tags:         store,
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	got := updated.(Model)
	if got.top != topHome {
		t.Fatalf("top = %d after ctrl+h on home, want topHome (%d)", got.top, topHome)
	}
	if got.status != "Refreshed" {
		t.Fatalf("status = %q, want %q", got.status, "Refreshed")
	}
}

// TestHomeProjectDeleteArmThenConfirm covers PR3 of #191: the
// destructive Home delete gate arms on the first `d` (status shows the
// confirmation hint, project still in DB) and fires the cascade on
// the second `d` (project gone, status shows the backup path).
func TestHomeProjectDeleteArmThenConfirm(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)

	store := snapstore.Open(t, dbDir+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	doomed, err := store.UpsertProject(ctx, "Doomed", "doomed", "/work/doomed")
	if err != nil {
		t.Fatalf("UpsertProject(doomed) error = %v", err)
	}
	if _, err := store.UpsertProject(ctx, "Survivor", "survivor", "/work/survivor"); err != nil {
		t.Fatalf("UpsertProject(survivor) error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Tags:         store,
		Events:       store,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	// First `d` arms the gate but does not delete.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	armed := updated.(Model)
	if armed.homeProjectDeletePendingID == 0 {
		t.Fatalf("first `d` did not arm home delete gate")
	}
	if armed.homeProjectDeletePendingID != doomed.ID {
		t.Fatalf("pending id = %d, want %d (cursor on first card)", armed.homeProjectDeletePendingID, doomed.ID)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err != nil {
		t.Fatalf("project gone after arm-only press: %v", err)
	}

	// Second `d` confirms; the cascade fires and the project is gone.
	updated, _ = armed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	deleted := updated.(Model)
	if _, err := store.FindProjectByID(ctx, doomed.ID); err == nil {
		t.Fatalf("project still present after confirm")
	}
	if deleted.homeProjectDeletePendingID != 0 {
		t.Fatalf("pending id = %d after confirm, want 0", deleted.homeProjectDeletePendingID)
	}
	if !strings.Contains(deleted.status, "doomed") || !strings.Contains(deleted.status, "backup") {
		t.Fatalf("post-delete status = %q, want project slug + backup mention", deleted.status)
	}
}

// TestHomeProjectDeleteOverlayConfirm exercises the notification-overlay
// path (PR3 #191 §E): `d` shows the home-project-delete-confirm card
// with pre-resolved counters, `D` action fires the cascade in-process,
// `esc` dismisses without touching state. Mirrors the YAML wiring with
// an inline Notification struct so the test stays hermetic.
func TestHomeProjectDeleteOverlayConfirm(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	store := snapstore.Open(t, dbDir+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	doomed, err := store.UpsertProject(ctx, "Doomed", "doomed", "/work/doomed")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	notif := config.Notification{
		Name:            "home-project-delete-confirm",
		Size:            config.NotificationSize{Width: 60, Height: 12},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.NotificationStyleRounded,
		Border:          config.NotificationBorder{Visible: ptrBool(true), Width: 1, Color: "#ff0000"},
		Animation:       []config.NotificationFrame{{Frame: 0, Value: ""}},
		Bubble:          config.NotificationBubble{TailSide: config.NotificationTailBottom},
		Padding:         zeroNotificationPadding(),
		AutoHeight:      ptrBool(false),
		PaddingInside:   ptrBool(true),
		FooterVisible:   ptrBool(true),
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: ptrInt(0),
		Actions: []config.NotificationAction{
			{Key: "D", ID: "confirm", Label: "Delete"},
		},
	}
	binding := NotificationBinding{Notifications: map[string]config.Notification{notif.Name: notif}}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Tags:         store,
		Events:       store,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, binding)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 80
	model.height = 24

	// First `d` shows the overlay; project still on disk.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	armed := updated.(Model)
	if armed.notification == nil {
		t.Fatalf("first `d` did not spawn the home-project-delete-confirm overlay")
	}
	if armed.homeProjectDeletePendingID != doomed.ID {
		t.Fatalf("pending id = %d, want %d", armed.homeProjectDeletePendingID, doomed.ID)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err != nil {
		t.Fatalf("project gone after overlay show: %v", err)
	}

	// `D` (uppercase) on a settled notification returns a Cmd whose
	// payload is the notification.ActionMsg the parent dispatches in
	// the next tick. Mirror bubbletea's runtime: invoke the Cmd, then
	// feed the resulting msg back through Update.
	_, cmd := armed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	if cmd == nil {
		t.Fatalf("D on settled overlay returned no Cmd")
	}
	actionMsg := cmd()
	if _, ok := actionMsg.(notification.ActionMsg); !ok {
		t.Fatalf("Cmd produced %T, want notification.ActionMsg", actionMsg)
	}
	updated, _ = armed.Update(actionMsg)
	deleted := updated.(Model)
	if deleted.notification != nil {
		t.Fatalf("notification still set after confirm action")
	}
	if deleted.homeProjectDeletePendingID != 0 {
		t.Fatalf("pending id = %d after confirm, want 0", deleted.homeProjectDeletePendingID)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err == nil {
		t.Fatalf("project still present after overlay confirm")
	}
}

// TestHomeProjectDeleteOverlayEscClears verifies that esc inside the
// overlay clears both the notification slot and the pending project
// state (so a later `d` on a different card does not confirm a stale
// delete).
func TestHomeProjectDeleteOverlayEscClears(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	store := snapstore.Open(t, dbDir+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	doomed, err := store.UpsertProject(ctx, "Doomed", "doomed", "/work/doomed")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}

	notif := config.Notification{
		Name:            "home-project-delete-confirm",
		Size:            config.NotificationSize{Width: 60, Height: 12},
		Background:      "transparent",
		FrameIntervalMs: 100,
		Style:           config.NotificationStyleRounded,
		Border:          config.NotificationBorder{Visible: ptrBool(true), Width: 1, Color: "#ff0000"},
		Animation:       []config.NotificationFrame{{Frame: 0, Value: ""}},
		Bubble:          config.NotificationBubble{TailSide: config.NotificationTailBottom},
		Padding:         zeroNotificationPadding(),
		AutoHeight:      ptrBool(false),
		PaddingInside:   ptrBool(true),
		FooterVisible:   ptrBool(true),
		Position:        config.NotificationPositionCenter,
		Dismiss:         config.NotificationDismiss{Mode: config.NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: ptrInt(0),
		Actions: []config.NotificationAction{
			{Key: "D", ID: "confirm", Label: "Delete"},
		},
	}
	binding := NotificationBinding{Notifications: map[string]config.Notification{notif.Name: notif}}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Tags:         store,
		Events:       store,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, binding)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	model.width = 80
	model.height = 24

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	armed := updated.(Model)
	if armed.notification == nil {
		t.Fatalf("overlay did not spawn")
	}

	// esc on the overlay fires DismissedMsg (per the YAML's
	// dismiss.keys list). dispatchNotification clears both the
	// notification slot and the pending project id.
	_, cmd := armed.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatalf("esc on settled overlay returned no Cmd")
	}
	dismissMsg := cmd()
	if _, ok := dismissMsg.(notification.DismissedMsg); !ok {
		t.Fatalf("Cmd produced %T, want notification.DismissedMsg", dismissMsg)
	}
	updated, _ = armed.Update(dismissMsg)
	cancelled := updated.(Model)
	if cancelled.notification != nil {
		t.Fatalf("notification still set after esc")
	}
	if cancelled.homeProjectDeletePendingID != 0 {
		t.Fatalf("pending id = %d after esc, want 0", cancelled.homeProjectDeletePendingID)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err != nil {
		t.Fatalf("project removed despite esc dismissal: %v", err)
	}
}

// TestHomeProjectDeleteSurfacesAuditWarn pins #191 review finding 7959:
// audit-trail warnings emitted from ProjectService.Delete (checkpoint
// failure, payload marshal failure, audit emission failure) must land
// on the TUI status surface rather than os.Stderr — stderr writes
// leak under the bubbletea alt-screen render. Drives a forced
// Checkpoint failure through the status-driven delete path and
// asserts the warning text appears appended to m.status alongside
// the success line.
func TestHomeProjectDeleteSurfacesAuditWarn(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	store := snapstore.Open(t, dbDir+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	doomed, err := store.UpsertProject(ctx, "Doomed", "doomed", "/work/doomed")
	if err != nil {
		t.Fatalf("UpsertProject(doomed): %v", err)
	}

	cp := &busyCheckpointer{err: errors.New("SQLITE_BUSY: foreign writer holds the WAL")}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.Install(0, store.Snapshot()),
		Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		Tags:         store,
		Events:       store,
		Checkpointer: cp,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	// Empty NotificationBinding forces the status-driven fallback;
	// two `d` presses arm then confirm.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	final := updated.(Model)

	if cp.calls != 1 {
		t.Fatalf("Checkpointer.Checkpoint calls = %d, want 1", cp.calls)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err == nil {
		t.Fatalf("project still present after confirm; auditWarn fix must not abort the cascade")
	}
	if !strings.Contains(final.status, "wal_checkpoint") {
		t.Fatalf("status missing checkpoint warning; auditWarn must land on m.status not stderr.\nstatus = %q", final.status)
	}
	if !strings.Contains(final.status, "doomed") {
		t.Fatalf("status missing project slug — success line dropped?\nstatus = %q", final.status)
	}
}

// TestHomeRendersProjectTagBadges covers AC4: project_tags become the badges
// on the Home cards, reusing the chip component.
func TestHomeRendersProjectTagBadges(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, t.TempDir()+"/omakiten.db")
	if err := store.ImportBundle(ctx, tuiTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Alpha", "alpha", "/work/alpha")
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	tag, err := store.FindOrCreateTag(ctx, "go", "Go")
	if err != nil {
		t.Fatalf("FindOrCreateTag() error = %v", err)
	}
	if err := store.AddProjectTag(ctx, project.ID, tag.ID); err != nil {
		t.Fatalf("AddProjectTag() error = %v", err)
	}

	model, err := NewModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache: runtimecache.Install(0, store.Snapshot()), Workflow:     app.NewWorkflowServiceFromStore(store, testfixtures.CanonicalRegistry(), store.Snapshot()),
		Comments:     store,
		Dependencies: store,
		Entries:      store,
		
		Tags:         store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "GO") {
		t.Fatalf("home card should surface project_tags as upper-cased badges:\n%s", rendered)
	}
}
