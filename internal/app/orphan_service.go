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
//
// RebindOrphanedRootTasks and RebindOrphanedSubtasks split the rebind by
// depth for projects with a sub-task kit configured: the root path keeps
// emitting task.migrated against the root workflow, while the sub-task
// path emits the dedicated task.bucket_orphaned event against the
// sub-kit workflow with the locked from_kit/to_kit/resolved_kit payload.
// Projects without a sub-task kit continue to use RebindOrphanedTasks
// (the "all tasks" entry point) so pre-cascade behaviour is preserved.
type OrphanRepository interface {
	PreviewOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	RebindOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	RebindOrphanedRootTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	RebindOrphanedSubtasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver, fromKit, toKit string) (domain.OrphanReport, error)
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

// Migrate applies the rebind for every orphan task and returns the
// combined report describing what was migrated. When the project has a
// sub-task kit configured (in either the current or previous snapshot),
// the rebind splits by depth: root tasks rebind against the root
// workflow and emit task.migrated; sub-tasks rebind against the
// resolved sub-kit workflow and emit task.bucket_orphaned with the
// locked payload (#281 cascade). Projects without subtask_kit fall back
// to the legacy "all tasks" path so the pre-cascade behaviour stays
// byte-identical.
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

	if s.cascadeActive() {
		report, err = s.migrateCascade(ctx, project.ID)
	} else {
		report, err = s.repo.RebindOrphanedTasks(ctx, project.ID, s.current, s.previous)
	}
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

// cascadeActive reports whether the migration must use the depth-split
// path: any snapshot in the pair (current or previous) declaring a
// sub-task kit triggers the cascade, so enable/disable/swap all route
// through migrateCascade.
func (s *OrphanService) cascadeActive() bool {
	return snapshotHasSubtaskKit(s.current) || snapshotHasSubtaskKit(s.previous)
}

func snapshotHasSubtaskKit(snap *config.Snapshot) bool {
	if snap == nil {
		return false
	}
	_, ok := snap.SubtaskKit()
	return ok
}

// migrateCascade fans the rebind into two scoped passes: root tasks
// against the root workflow (legacy task.migrated) and sub-tasks against
// the resolved sub-kit workflow (task.bucket_orphaned). The from_kit/
// to_kit identities follow the locked semantics: enable goes root→sub,
// disable goes sub→root, swap goes sub→sub.
func (s *OrphanService) migrateCascade(ctx context.Context, projectID int64) (domain.OrphanReport, error) {
	rootReport, err := s.repo.RebindOrphanedRootTasks(ctx, projectID, s.current, s.previous)
	if err != nil {
		return domain.OrphanReport{}, err
	}

	curSub := resolveSubtaskOrRoot(s.current)
	prevSub := resolveSubtaskOrRoot(s.previous)
	fromKit, toKit := resolvedKitIdentities(s.previous, s.current)
	subReport, err := s.repo.RebindOrphanedSubtasks(ctx, projectID, curSub, prevSub, fromKit, toKit)
	if err != nil {
		return domain.OrphanReport{}, err
	}

	combined := rootReport
	if combined.WorkflowKey == "" {
		combined.WorkflowKey = subReport.WorkflowKey
	}
	combined.Groups = append(combined.Groups, subReport.Groups...)
	combined.Total += subReport.Total
	return combined, nil
}

// resolveSubtaskOrRoot returns the sub-kit snapshot when configured,
// otherwise the root snapshot. The migration cascade routes sub-tasks
// through whichever snapshot is the authoritative source of their
// workflow — on disable that collapses back to the root snapshot.
func resolveSubtaskOrRoot(snap *config.Snapshot) domain.BucketResolver {
	if snap == nil {
		return nil
	}
	if sub, ok := snap.SubtaskKit(); ok {
		return sub
	}
	return snap
}

// resolvedKitIdentities derives the from_kit / to_kit identity strings
// the task.bucket_orphaned payload requires. The values come from the
// sub-kit snapshot when configured, otherwise the root kit — which
// matches the locked semantics: enable (root → sub), disable
// (sub → root), swap (sub → sub).
func resolvedKitIdentities(prev, curr *config.Snapshot) (string, string) {
	return kitIdentity(prev), kitIdentity(curr)
}

func kitIdentity(snap *config.Snapshot) string {
	if snap == nil {
		return ""
	}
	if sub, ok := snap.SubtaskKit(); ok {
		return sub.Kit().Key
	}
	return snap.Kit().Key
}
