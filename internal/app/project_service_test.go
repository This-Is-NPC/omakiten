package app

import (
	"context"
	"errors"
	"io"
	"testing"

	"omakiten/internal/domain"
)

// fakeBackup records every call and optionally returns a pinned
// error. Lets tests exercise both the happy path and the backup
// failure aborts delete invariant without spinning up a real
// BackupService.
type fakeBackup struct {
	path  string
	err   error
	calls int
}

func (f *fakeBackup) Run(_ context.Context) (string, error) {
	f.calls++
	return f.path, f.err
}

// projectRecordedEvent captures one RecordEntityEvent call so tests can
// assert the project.removed payload landed in the audit trail.
type projectRecordedEvent struct {
	EntityType string
	EntityID   int64
	ProjectID  int64
	EventType  string
	Payload    string
}

type fakeEventRecorder struct {
	calls []projectRecordedEvent
}

func (f *fakeEventRecorder) RecordEntityEvent(_ context.Context, entityType string, entityID, projectID int64, eventType, payload string) error {
	f.calls = append(f.calls, projectRecordedEvent{
		EntityType: entityType,
		EntityID:   entityID,
		ProjectID:  projectID,
		EventType:  eventType,
		Payload:    payload,
	})
	return nil
}

func TestProjectServiceInit(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	service := NewProjectService(store, nil, nil)

	// Empty name falls back to filepath.Base(absRoot)
	p, err := service.Init(ctx, "", "", "/work/my-project")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p.Name != "my-project" {
		t.Fatalf("Init().Name = %q, want %q", p.Name, "my-project")
	}
	if p.Slug != "my-project" {
		t.Fatalf("Init().Slug = %q, want %q", p.Slug, "my-project")
	}

	// Normalize slug from name
	p2, err := service.Init(ctx, "", "", "/work/Another Project")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p2.Slug != "another-project" {
		t.Fatalf("Init().Slug = %q, want %q", p2.Slug, "another-project")
	}

	// Custom slug
	p3, err := service.Init(ctx, "Custom", "custom-slug", "/work/ignored")
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if p3.Name != "Custom" {
		t.Fatalf("Init().Name = %q, want %q", p3.Name, "Custom")
	}
	if p3.Slug != "custom-slug" {
		t.Fatalf("Init().Slug = %q, want %q", p3.Slug, "custom-slug")
	}

	// Slug becomes empty after normalization -> error
	_, err = service.Init(ctx, "!!!", "!!!", "/work/root")
	if err == nil {
		t.Fatal("Init() error = nil, want validation error")
	}
	assertCodedError(t, err, domain.ErrValidation)

	// Cannot reuse existing project slug
	_, err = service.Init(ctx, "", project.Slug, project.RootPath)
	if err != nil {
		t.Fatalf("Init() existing slug error = %v", err)
	}
}

func TestProjectServiceDelete_HappyPath(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	backup := &fakeBackup{path: "/var/state/omakiten/backups/2026-05-21T18-00-00Z.db"}
	events := &fakeEventRecorder{}
	svc := NewProjectService(store, backup, events)

	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	result, err := svc.Delete(ctx, project.ID, counters)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if result.BackupPath != backup.path {
		t.Fatalf("Delete().BackupPath = %q, want %q", result.BackupPath, backup.path)
	}
	if result.Project.Slug != project.Slug {
		t.Fatalf("Delete().Project.Slug = %q, want %q", result.Project.Slug, project.Slug)
	}
	if backup.calls != 1 {
		t.Fatalf("backup.Run calls = %d, want 1", backup.calls)
	}
	if len(events.calls) != 1 || events.calls[0].EventType != domain.EventTypeProjectRemoved {
		t.Fatalf("events recorded = %+v, want one project.removed", events.calls)
	}
	if events.calls[0].EntityID != project.ID {
		t.Fatalf("project.removed entity_id = %d, want %d", events.calls[0].EntityID, project.ID)
	}

	// Project row is gone — FindProjectByID returns ErrProjectNotFound.
	if _, err := store.FindProjectByID(ctx, project.ID); err == nil {
		t.Fatalf("FindProjectByID after delete = nil, want ErrProjectNotFound")
	}
}

func TestProjectServiceDelete_BackupFailureAbortsDelete(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	sentinel := errors.New("disk full")
	backup := &fakeBackup{err: sentinel}
	events := &fakeEventRecorder{}
	svc := NewProjectService(store, backup, events)

	_, err := svc.Delete(ctx, project.ID, domain.ProjectDeleteCounters{})
	if err == nil {
		t.Fatalf("Delete() error = nil, want backup failure")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Delete() error = %v, want chain containing %v", err, sentinel)
	}
	// No events emitted: the destructive flow never reached the
	// transaction. The project row must still be on disk.
	if len(events.calls) != 0 {
		t.Fatalf("events recorded after backup failure = %d, want 0", len(events.calls))
	}
	if _, err := store.FindProjectByID(ctx, project.ID); err != nil {
		t.Fatalf("FindProjectByID after aborted delete error = %v, want project still present", err)
	}
}

func TestProjectServiceDelete_RequiresBackupRunner(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	svc := NewProjectService(store, nil, nil)
	if _, err := svc.Delete(ctx, project.ID, domain.ProjectDeleteCounters{}); err == nil {
		t.Fatalf("Delete() with nil backup error = nil, want validation error")
	}
}

// fakeCheckpointer records every Checkpoint call and lets tests pin
// an error. Used to assert ProjectService.Delete invokes the
// Checkpointer BEFORE BackupService.Run so the snapshot reflects
// every committed WAL frame this process wrote.
type fakeCheckpointer struct {
	calls int
	err   error
}

func (f *fakeCheckpointer) Checkpoint(context.Context) error {
	f.calls++
	return f.err
}

// orderingBackup records the call order against a shared ledger so a
// test can pin "checkpoint happened before backup".
type orderingBackup struct {
	ledger *[]string
	path   string
}

func (o *orderingBackup) Run(context.Context) (string, error) {
	*o.ledger = append(*o.ledger, "backup")
	return o.path, nil
}

type orderingCheckpointer struct {
	ledger *[]string
}

func (o *orderingCheckpointer) Checkpoint(context.Context) error {
	*o.ledger = append(*o.ledger, "checkpoint")
	return nil
}

func TestProjectServiceDelete_RunsCheckpointBeforeBackup(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	ledger := make([]string, 0, 2)
	svc := NewProjectService(store, &orderingBackup{ledger: &ledger, path: "/tmp/snap.db"}, &fakeEventRecorder{}).
		WithCheckpointer(&orderingCheckpointer{ledger: &ledger})

	if _, err := svc.Delete(ctx, project.ID, counters); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(ledger) != 2 || ledger[0] != "checkpoint" || ledger[1] != "backup" {
		t.Fatalf("call order = %v, want [checkpoint backup] (WAL must land in main DB before file copy)", ledger)
	}
}

func TestProjectServiceDelete_ContinuesWhenCheckpointFails(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	cp := &fakeCheckpointer{err: errors.New("SQLITE_BUSY: foreign writer holds the WAL")}
	backup := &fakeBackup{path: "/var/state/snap.db"}
	svc := NewProjectService(store, backup, &fakeEventRecorder{}).
		SetAuditWarnWriter(io.Discard).
		WithCheckpointer(cp)

	result, err := svc.Delete(ctx, project.ID, counters)
	if err != nil {
		t.Fatalf("Delete error = %v, want nil — checkpoint failure must not abort the destructive flow", err)
	}
	if cp.calls != 1 {
		t.Fatalf("Checkpoint calls = %d, want 1", cp.calls)
	}
	if backup.calls != 1 {
		t.Fatalf("backup invoked = %d, want 1 — best-effort checkpoint must still let snapshot run", backup.calls)
	}
	if result.BackupPath == "" {
		t.Fatalf("BackupPath empty after best-effort checkpoint failure")
	}
}

// countingRepo wraps a ProjectRepository to count ProjectDeleteCounts
// calls. Used to pin the contract that Delete does not re-query the
// counters it accepts from the caller — the regression guard for the
// duplicate round-trip review finding (#191 comment 7946).
type countingRepo struct {
	ProjectRepository
	countCalls int
}

func (c *countingRepo) ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error) {
	c.countCalls++
	return c.ProjectRepository.ProjectDeleteCounts(ctx, projectID)
}

func TestProjectServiceDelete_AcceptsCounterSnapshotWithoutRequery(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t, 1000))
	defer func() { _ = store.Close() }()

	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	repo := &countingRepo{ProjectRepository: store}
	backup := &fakeBackup{path: "/var/state/omakiten/backups/snap.db"}
	events := &fakeEventRecorder{}

	svc := NewProjectService(repo, backup, events)
	if _, err := svc.Delete(ctx, project.ID, counters); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repo.countCalls != 0 {
		t.Fatalf("ProjectDeleteCounts calls inside Delete = %d, want 0 (caller-provided counters used)", repo.countCalls)
	}
	if len(events.calls) != 1 {
		t.Fatalf("audit emissions = %d, want 1", len(events.calls))
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello-world"},
		{"  Hello World  ", "hello-world"},
		{"Hello-World", "hello-world"},
		{"hello---world", "hello-world"},
		{"---hello---world---", "hello-world"},
		{"hello123world", "hello123world"},
		{"hello world!@#$%", "hello-world"},
		{"", ""},
		{"!!!", ""},
	}

	for _, tc := range tests {
		actual := normalizeSlug(tc.input)
		if actual != tc.expected {
			t.Errorf("normalizeSlug(%q) = %q, want %q", tc.input, actual, tc.expected)
		}
	}
}
