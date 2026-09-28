package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/testfixtures/snapstore"
	"omakiten/internal/token"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/home"
)

// pumpAsync drives any tea.Cmd the model returned through to its
// terminal Msg (and any cascaded Cmds it produces) by folding each
// emitted Msg back into Update. Used by the async Home delete tests
// where ProjectService.Delete now runs off the Update goroutine via
// a returned Cmd — the test driver has to simulate the bubbletea
// runtime's "run Cmd → feed Msg back" loop to land the result.
func pumpAsync(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return m
		}
		next, nextCmd := m.Update(msg)
		m = next.(Model)
		cmd = nextCmd
	}
	return m
}

// projectDeleteStore is the agentruntime.ProjectStore method set without
// sqlite.DeleteProjectWithBackup, so wrapping it hides the atomic delete
// path and ProjectService.Delete uses checkpoint + BackupService.Run.
type projectDeleteStore interface {
	UpsertProject(ctx context.Context, name, slug, rootPath string) (domain.Project, error)
	FindProjectByID(ctx context.Context, id int64) (domain.Project, error)
	FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error)
	FindProjectsContainingPath(ctx context.Context, path string) ([]domain.Project, error)
	ListProjects(ctx context.Context) ([]domain.Project, error)
	ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error)
	DeleteProject(ctx context.Context, projectID int64) error
	UpdateProjectDescription(ctx context.Context, id int64, description string) (domain.Project, error)
	RecordEntityEvent(ctx context.Context, entityType string, entityID, projectID int64, eventType, payload string) error
	DeleteProjectWithBackup(context.Context, int64, func(context.Context, func(string) error) (string, error), func(string) error, func() error) (string, error)
}

// firstCountsFailRepo wraps a project store and fails the first
// ProjectDeleteCounts call (mirroring a transient SQLite hiccup at
// arm-time); subsequent calls delegate so the re-query path lands
// the actual counts.
type firstCountsFailRepo struct {
	projectDeleteStore
	calls int
}

func (r *firstCountsFailRepo) ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error) {
	r.calls++
	if r.calls == 1 {
		return domain.ProjectDeleteCounters{}, errors.New("transient sqlite hiccup")
	}
	return r.projectDeleteStore.ProjectDeleteCounts(ctx, projectID)
}

// recordingEvents captures every RecordEntityEvent payload so tests
// can assert the audit truthfulness of post-commit emissions. Other
// methods delegate to the embedded EventRepository (the real store).
type recordingEvents struct {
	EventStore
	calls []recordingEventCall
}

type recordingEventCall struct {
	EntityType string
	EntityID   int64
	ProjectID  int64
	EventType  string
	Payload    string
}

func (r *recordingEvents) RecordEntityEvent(ctx context.Context, entityType string, entityID int64, projectID int64, eventType string, payload string) error {
	r.calls = append(r.calls, recordingEventCall{
		EntityType: entityType,
		EntityID:   entityID,
		ProjectID:  projectID,
		EventType:  eventType,
		Payload:    payload,
	})
	return r.EventStore.RecordEntityEvent(ctx, entityType, entityID, projectID, eventType, payload)
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,

		Tags: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}
	if model.navigationTop() != screenhost.TopHome {
		t.Fatalf("top = %s, want screenhost.TopHome (%s)", model.navigationTop(), screenhost.TopHome)
	}

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "PROJECTS · 2") {
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,

		Tags: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}

	rendered := ansi.Strip(model.View())
	for _, label := range []string{"01 // TASKS", "02 // STATS", "03 // STUDIO", "04 // SETTINGS"} {
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
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	model, err := newHomeModel(ctx, project.Context(), Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,

		Tags: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}
	if model.navigationTop() != screenhost.TopTasks || model.navigation != screenhost.TasksBoard {
		t.Fatalf("(top, sub) = (%s, %s), want (screenhost.TopTasks, screenhost.TasksBoard)", model.navigationTop(), model.navigation)
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	got := pumpAsync(t, updated.(Model), cmd)
	if got.navigationTop() != screenhost.TopHome {
		t.Fatalf("top = %s after ctrl+h, want screenhost.TopHome (%s)", got.navigationTop(), screenhost.TopHome)
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,

		Tags: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.navigationTop() != screenhost.TopTasks || got.navigation != screenhost.TasksBoard {
		t.Fatalf("(top, sub) = (%s, %s) after enter, want (screenhost.TopTasks, screenhost.TasksBoard)", got.navigationTop(), got.navigation)
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,

		Tags:    store,
		Catalog: newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	got := pumpAsync(t, updated.(Model), cmd)
	if got.navigationTop() != screenhost.TopHome {
		t.Fatalf("top = %s after ctrl+h on home, want screenhost.TopHome (%s)", got.navigationTop(), screenhost.TopHome)
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,
		Tags:         store,
		Events:       store,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}

	// First `d` arms the gate but does not delete.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	armed := updated.(Model)
	if armed.homeScreen.ArmedProjectID() == 0 {
		t.Fatalf("first `d` did not arm home delete gate")
	}
	if armed.homeScreen.ArmedProjectID() != doomed.ID {
		t.Fatalf("pending id = %d, want %d (cursor on first card)", armed.homeScreen.ArmedProjectID(), doomed.ID)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err != nil {
		t.Fatalf("project gone after arm-only press: %v", err)
	}

	// Second `d` confirms; the cascade fires and the project is gone.
	updated, cmd := armed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	deleted := pumpAsync(t, updated.(Model), cmd)
	if _, err := store.FindProjectByID(ctx, doomed.ID); err == nil {
		t.Fatalf("project still present after confirm")
	}
	if deleted.homeScreen.ArmedProjectID() != 0 {
		t.Fatalf("pending id = %d after confirm, want 0", deleted.homeScreen.ArmedProjectID())
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,
		Tags:         store,
		Events:       store,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, binding)
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}
	model.width = 80
	model.height = 24

	// First `d` shows the overlay; project still on disk.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	armed := updated.(Model)
	if armed.notification == nil {
		t.Fatalf("first `d` did not spawn the home-project-delete-confirm overlay")
	}
	if armed.homeScreen.ArmedProjectID() != doomed.ID {
		t.Fatalf("pending id = %d, want %d", armed.homeScreen.ArmedProjectID(), doomed.ID)
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err != nil {
		t.Fatalf("project gone after overlay show: %v", err)
	}

	// `D` (uppercase) on a settled notification returns a Cmd whose
	// payload is the notification.ActionMsg the parent dispatches in
	// the next tick. Mirror bubbletea's runtime: invoke the Cmd, then
	// feed the resulting msg back through Update. The action handler
	// itself returns the async delete Cmd, so pumpAsync drives the
	// whole chain to the terminal result.
	_, cmd := armed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	if cmd == nil {
		t.Fatalf("D on settled overlay returned no Cmd")
	}
	actionMsg := cmd()
	if _, ok := actionMsg.(ActionMsg); !ok {
		t.Fatalf("Cmd produced %T, want ActionMsg", actionMsg)
	}
	updated, actionCmd := armed.Update(actionMsg)
	deleted := pumpAsync(t, updated.(Model), actionCmd)
	if deleted.notification != nil {
		t.Fatalf("notification still set after confirm action")
	}
	if deleted.homeScreen.ArmedProjectID() != 0 {
		t.Fatalf("pending id = %d after confirm, want 0", deleted.homeScreen.ArmedProjectID())
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,
		Tags:         store,
		Events:       store,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, binding)
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
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
	if _, ok := dismissMsg.(DismissedMsg); !ok {
		t.Fatalf("Cmd produced %T, want DismissedMsg", dismissMsg)
	}
	updated, _ = armed.Update(dismissMsg)
	cancelled := updated.(Model)
	if cancelled.notification != nil {
		t.Fatalf("notification still set after esc")
	}
	if cancelled.homeScreen.ArmedProjectID() != 0 {
		t.Fatalf("pending id = %d after esc, want 0", cancelled.homeScreen.ArmedProjectID())
	}
	if _, err := store.FindProjectByID(ctx, doomed.ID); err != nil {
		t.Fatalf("project removed despite esc dismissal: %v", err)
	}
}

// TestHomeProjectDeleteRequeriesZeroCountersForAuditTruth pins the
// degraded-path fix: when arm-time ProjectDeleteCounts fails (transient
// SQLite hiccup), armOrConfirmHomeProjectDelete stashes the zero value
// and the user proceeds via second press. Without the re-query, the
// project.removed audit payload claims "deleted empty project" for a
// project carrying real rows. The re-query inside executeHomeProjectDelete
// pays one extra round-trip on the rare degraded branch and the audit
// reflects the actual pre-delete state.
func TestHomeProjectDeleteRequeriesZeroCountersForAuditTruth(t *testing.T) {
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
	// Three tasks so a real ProjectDeleteCounts call returns
	// Tasks: 3 — distinguishable from the zero-value claim under the
	// degraded path.
	for i := 0; i < 3; i++ {
		if _, err := store.CreateTask(ctx, doomed.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
			t.Fatalf("CreateTask(%d): %v", i, err)
		}
	}

	wrappedRepo := &firstCountsFailRepo{projectDeleteStore: store}
	recorder := &recordingEvents{EventStore: store}

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     wrappedRepo,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,
		Tags:         store,
		Events:       recorder,
		DBPath:       dbDir + "/omakiten.db",
		Catalog:      newTestCatalog(t),
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}

	// First `d`: arm time. wrappedRepo errors the first call → zeros
	// stashed in pending counters. Second `d`: execute path detects
	// zero-value counters and re-queries; second call succeeds (real
	// counts). pumpAsync drives the async delete Cmd through to the
	// result Msg so the recorder captures the post-commit payload.
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, cmd := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_ = pumpAsync(t, updated.(Model), cmd)

	if _, err := store.FindProjectByID(ctx, doomed.ID); err == nil {
		t.Fatalf("project still present after second press")
	}
	if wrappedRepo.calls < 2 {
		t.Fatalf("ProjectDeleteCounts calls = %d, want >= 2 (arm-time + execute-time re-query)", wrappedRepo.calls)
	}

	var removed *recordingEventCall
	for i := range recorder.calls {
		if recorder.calls[i].EventType == domain.EventTypeProjectRemoved {
			removed = &recorder.calls[i]
			break
		}
	}
	if removed == nil {
		t.Fatalf("project.removed event not recorded; calls = %+v", recorder.calls)
	}
	var payload struct {
		Counters domain.ProjectDeleteCounters `json:"counters"`
	}
	if err := json.Unmarshal([]byte(removed.Payload), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Counters.Tasks != 3 {
		t.Fatalf("audit payload counters.Tasks = %d, want 3 — degraded re-query did not land", payload.Counters.Tasks)
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

	model, err := newHomeModel(ctx, domain.ProjectContext{}, Repositories{
		Tasks:        store,
		Projects:     store,
		Cache:        runtimecache.InstallWithStore(0, store),
		Comments:     store,
		Dependencies: store,

		Tags: store,
	}, tuiTestTheme(), token.ApproxCounter{}, config.TokenBadgeThresholds{}, config.MustLoadKitConfig().Priorities, config.MustLoadKitConfig().Severities, NotificationBinding{})
	if err != nil {
		t.Fatalf("newHomeModel() error = %v", err)
	}

	rendered := ansi.Strip(model.View())
	if !strings.Contains(rendered, "GO") {
		t.Fatalf("home card should surface project_tags as upper-cased badges:\n%s", rendered)
	}
}

func TestHomeDropsStaleDeleteResult(t *testing.T) {
	model := Model{navigation: screenhost.Home, homeScreen: home.New().Loading(2), status: "current"}
	model.handleHomeProjectDeleteResult(homeProjectDeleteResultMsg{generation: 1, err: errors.New("stale failure")})
	if model.status != "current" || !model.homeScreen.IsLoading() {
		t.Fatalf("stale delete result applied: status=%q loading=%v", model.status, model.homeScreen.IsLoading())
	}
	model.navigation = firstSub(screenhost.TopTasks)
	model.handleHomeProjectDeleteResult(homeProjectDeleteResultMsg{generation: 2, err: errors.New("wrong route")})
	if model.status != "current" {
		t.Fatalf("off-route delete result applied: status=%q", model.status)
	}
}

func TestHomeFailureFinalRenderSanitizesGlobalStatus(t *testing.T) {
	model := Model{
		styles:     newStyles(config.Theme{}),
		width:      80,
		height:     24,
		navigation: screenhost.Home,
		homeScreen: home.New().Loading(1),
	}
	model.applyHomeReload(homeReloadResultMsg{generation: 1, err: errors.New(hostileGlobalStatus)})

	assertHostileStatusIsSafe(t, model.View())
}

func TestHomeReloadDropsResultAfterRuntimeRotation(t *testing.T) {
	model := Model{navigation: screenhost.Home, homeScreen: home.New().Loading(1), status: "current", studioRuntimeGeneration: 2}
	model.applyHomeReload(homeReloadResultMsg{
		generation:        1,
		runtimeGeneration: 1,
		err:               errors.New("old runtime failure"),
	})
	if model.status != "current" || !model.homeScreen.IsLoading() {
		t.Fatalf("stale runtime home result applied: status=%q loading=%v", model.status, model.homeScreen.IsLoading())
	}
}

func newHomeModel(ctx context.Context, project domain.ProjectContext, repos Repositories, theme config.Theme, counter token.Counter, badge config.TokenBadgeThresholds, priorities []config.PriorityDefinition, severities []config.SeverityDefinition, notifications NotificationBinding) (Model, error) {
	if store, ok := repos.Projects.(agentruntime.ProjectStore); ok {
		retention := 0
		if snap := repos.activeSnapshot(); snap != nil {
			retention = snap.Settings().Backup.RetentionCount
		}
		repos.DeleteProject = agentruntime.ProjectDeleter(store, repos.Events, repos.DBPath, retention)
	}
	return NewModel(ctx, project, repos, theme, counter, badge, priorities, severities, notifications)
}
