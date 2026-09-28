package app

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"omakiten/internal/contract"
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
	store, project := appTestStore(t, appTestBundle(t))
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
	store, project := appTestStore(t, appTestBundle(t))
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
	store, project := appTestStore(t, appTestBundle(t))
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

type completedAtomicProjectRepository struct {
	ProjectRepository
	backupPath string
	calls      int
}

func (r *completedAtomicProjectRepository) DeleteProjectWithBackup(
	context.Context,
	int64,
	func(context.Context, func(string) error) (string, error),
	func(string) error,
	func() error,
) (string, error) {
	r.calls++
	return r.backupPath, nil
}

func TestProjectServiceDelete_LeaseReleaseFailureAfterCommitWarnsWithoutRetryableError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	t.Cleanup(func() { _ = store.Close() })
	releaseErr := errors.New("release failed after commit")
	repo := &completedAtomicProjectRepository{ProjectRepository: store, backupPath: "/recovery.db"}
	backup := &finalizationTestLeaser{lease: &finalizationTestLease{}, err: releaseErr}
	events := &fakeEventRecorder{}
	var warnings bytes.Buffer

	result, err := NewProjectService(repo, backup, events).SetAuditWarnWriter(&warnings).Delete(ctx, project.ID, domain.ProjectDeleteCounters{})
	if err != nil {
		t.Fatalf("Delete() returned retryable error after committed mutation: %v", err)
	}
	if result.BackupPath != repo.backupPath || repo.calls != 1 {
		t.Fatalf("committed delete = result:%+v repo calls:%d", result, repo.calls)
	}
	if !bytes.Contains(warnings.Bytes(), []byte(releaseErr.Error())) {
		t.Fatalf("post-commit lease warning = %q, want %q", warnings.String(), releaseErr)
	}
	if len(events.calls) != 1 || events.calls[0].EventType != domain.EventTypeProjectRemoved {
		t.Fatalf("post-commit audit events = %+v", events.calls)
	}
}

func TestProjectServiceDelete_RequiresBackupRunner(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	svc := NewProjectService(store, nil, nil)
	if _, err := svc.Delete(ctx, project.ID, domain.ProjectDeleteCounters{}); err == nil {
		t.Fatalf("Delete() with nil backup error = nil, want validation error")
	}
}

// countingRepo wraps a ProjectRepository to count ProjectDeleteCounts
// calls. Used to pin the contract that Delete does not re-query the
// counters it accepts from the caller — the regression guard for the
// duplicate round-trip review finding (#191 comment 7946).
type countingRepo struct {
	ProjectRepository
	AtomicProjectDeleteRepository
	countCalls int
}

func (c *countingRepo) ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error) {
	c.countCalls++
	return c.ProjectRepository.ProjectDeleteCounts(ctx, projectID)
}

func TestProjectServiceDelete_AcceptsCounterSnapshotWithoutRequery(t *testing.T) {
	ctx := context.Background()
	store, project := appTestStore(t, appTestBundle(t))
	defer func() { _ = store.Close() }()

	counters, err := store.ProjectDeleteCounts(ctx, project.ID)
	if err != nil {
		t.Fatalf("ProjectDeleteCounts: %v", err)
	}
	repo := &countingRepo{ProjectRepository: store, AtomicProjectDeleteRepository: store}
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
		actual := domain.Slugify(tc.input)
		if actual != tc.expected {
			t.Errorf("domain.Slugify(%q) = %q, want %q", tc.input, actual, tc.expected)
		}
	}
}

type fakeBackupLease struct{ backup *fakeBackup }

func (f *fakeBackup) WithLease(ctx context.Context, run func(contract.BackupLease) error) error {
	return run(fakeBackupLease{f})
}
func (l fakeBackupLease) WriteSnapshot(ctx context.Context, _ func(string) error) (string, error) {
	return l.backup.Run(ctx)
}
func (l fakeBackupLease) Write(ctx context.Context) (string, error) { return l.backup.Run(ctx) }
func (l fakeBackupLease) Discard(string) error                      { return nil }
func (l fakeBackupLease) Validate() error                           { return nil }
func (l fakeBackupLease) PruneRetaining(string) error               { return nil }
func (l fakeBackupLease) PruneFailedRetaining(string) error         { return nil }
