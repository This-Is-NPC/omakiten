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
// PreviewOrphanedCascade + RebindOrphanedCascade serve the sub-task kit
// cascade migration (#281 / #285). Both consume the same
// `domain.OrphanCascadePlan` so the preview shown in the bundle-swap
// prompt and the confirmed migrate report the same root/sub-task sets —
// preview/migrate parity is the locked contract on task #301 (review
// §11557 finding A1). The cascade rebind is atomic: root + sub passes
// run inside one transaction in the adapter so a sub-task failure
// rolls back the root-pass writes too (no partial progress, locked
// decision on task #301 review §11557 finding A7).
type OrphanRepository interface {
	PreviewOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	RebindOrphanedTasks(ctx context.Context, projectID int64, current, previous domain.BucketResolver) (domain.OrphanReport, error)
	PreviewOrphanedCascade(ctx context.Context, projectID int64, plan domain.OrphanCascadePlan) (domain.OrphanReport, error)
	RebindOrphanedCascade(ctx context.Context, projectID int64, plan domain.OrphanCascadePlan) (domain.OrphanReport, error)
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

// Preview returns the orphan report for the given project without
// mutating. When the cascade is active (either snapshot in the pair
// declares a sub-task kit) the preview routes through
// PreviewOrphanedCascade so the rows shown match what Migrate would
// rebind — preview/migrate parity (#301 review §11557 finding A1).
// Projects without a sub-task kit keep using the legacy "all tasks"
// preview to preserve pre-cascade behaviour byte-for-byte.
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

	if s.cascadeActive() {
		report, err = s.repo.PreviewOrphanedCascade(ctx, project.ID, s.cascadePlan())
		return
	}
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
		report, err = s.repo.RebindOrphanedCascade(ctx, project.ID, s.cascadePlan())
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
// through RebindOrphanedCascade.
func (s *OrphanService) cascadeActive() bool {
	return CascadeActive(s.current, s.previous)
}

// CascadeActive is the package-level predicate the TUI bundle-swap
// preview shares with OrphanService.Preview so both surfaces enter the
// cascade-aware path under the same rule.
func CascadeActive(current, previous *config.Snapshot) bool {
	return snapshotHasSubtaskKit(current) || snapshotHasSubtaskKit(previous)
}

// NewOrphanCascadePlan builds the plan the cascade preview / rebind
// path consumes from a (current, previous) snapshot pair. Exposed so
// the TUI bundle-swap preview can reuse the exact same plan
// OrphanService.Migrate builds — preview/migrate parity (#301 review
// §11557 finding A1).
func NewOrphanCascadePlan(current, previous *config.Snapshot) domain.OrphanCascadePlan {
	fromKit, toKit := resolvedKitIdentities(previous, current)
	return domain.OrphanCascadePlan{
		CurrentRoot:  current,
		PreviousRoot: previous,
		CurrentSub:   resolveSubtaskOrRoot(current),
		PreviousSub:  resolveSubtaskOrRoot(previous),
		FromKit:      fromKit,
		ToKit:        toKit,
	}
}

func snapshotHasSubtaskKit(snap *config.Snapshot) bool {
	if snap == nil {
		return false
	}
	_, ok := snap.SubtaskKit()
	return ok
}

// cascadePlan delegates to the package-level helper so the TUI
// bundle-swap preview can share the same builder.
func (s *OrphanService) cascadePlan() domain.OrphanCascadePlan {
	return NewOrphanCascadePlan(s.current, s.previous)
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
