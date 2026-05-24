package app

import (
	"context"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// OrphanRepository exposes the read + rebind primitives the OrphanService
// composes. PreviewOrphanedTasks is pure read; RebindOrphanedTasks applies
// the rebind and emits task.migrated events. Both accept the current and
// previous BucketResolver views (previous may be nil on the first import)
// so the adapter never imports config.
type OrphanRepository interface {
	PreviewOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	RebindOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
}

type OrphanService struct {
	repo     OrphanRepository
	current  *config.Snapshot
	previous *config.Snapshot
	// onMigrate runs after a successful Migrate. The runtime cache
	// wires it to release the previous snapshot pointer so the carried
	// reference does not pin the prior bundle in RAM indefinitely
	// (task #228 in the code-review plan). nil when no consumer is
	// wired — tests and the CLI standalone path leave it unset.
	onMigrate func()
}

// NewOrphanService wires the orphan service with the current and previous
// per-project Snapshot pointers. previous may be nil when the runtime has
// only seen one bundle for the project — the implementation degrades to
// "no orphans" in that case. The Snapshot type satisfies
// domain.BucketResolver, so the repository contract is unchanged; the
// service-side parameters carry the concrete *config.Snapshot type the
// Phase 2-bis spec mandates.
func NewOrphanService(repo OrphanRepository, current, previous *config.Snapshot) *OrphanService {
	return &OrphanService{repo: repo, current: current, previous: previous}
}

// SetMigrateConsumer installs a callback fired after Migrate completes
// successfully. The agentruntime BundleCache uses it to drop its
// PreviousSnapshot pointer once the rebind round consumed it — keeping
// the carried reference alive afterwards just pins the prior bundle in
// RAM with no remaining reader.
func (s *OrphanService) SetMigrateConsumer(fn func()) {
	s.onMigrate = fn
}

// Preview returns the orphan report for the given project without mutating.
func (s *OrphanService) Preview(ctx context.Context, project domain.ProjectContext) (report domain.OrphanReport, err error) {
	finish := activity.Track(ctx, "app.OrphanService.Preview", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	report, err = s.repo.PreviewOrphanedTasks(ctx, project.ID, s.current, s.previous)
	return
}

// Migrate applies the rebind for every orphan task and returns the report
// describing what was migrated.
func (s *OrphanService) Migrate(ctx context.Context, project domain.ProjectContext) (report domain.OrphanReport, err error) {
	finish := activity.Track(ctx, "app.OrphanService.Migrate", project, nil)
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	report, err = s.repo.RebindOrphanedTasks(ctx, project.ID, s.current, s.previous)
	if err == nil && s.onMigrate != nil {
		// Drop the cache's previous-snapshot reference. The rebind ran
		// against `s.previous` and that pointer carries the prior
		// bundle's full Snapshot tree (workflows, entities, locale
		// packs). No other reader needs it after Migrate returns; if a
		// later rebuild captures a fresh previous, the cache will set
		// it again.
		s.onMigrate()
		s.previous = nil
	}
	return
}
