package app

import (
	"omakiten/internal/config"
	"omakiten/internal/hooks/actions"
)

// NotificationService is the application-layer entry point for notification
// catalog reads. It owns the per-project Snapshot pointer the rest of the
// app reads notifications from, so the hooks engine and the TUI dispatcher
// share one source of truth — when ProjectRuntime rotates its Snapshot via
// BundleCache.Reload, the next BundleSnapshot() call reflects the new
// catalog without rebuilding the action registry.
//
// Spec note: Phase 2-bis required app/notification_service.go alongside the
// other Snapshot-consuming services. The runtime composition root used to
// read bundle.Notifications directly when constructing
// actions.NotificationShowAction; routing the read through this service
// keeps the dependency direction inward (cache → app → snapshot) and
// removes the last bundle.* read from the hot wiring path.
type NotificationService struct {
	snap *config.Snapshot
}

// NewNotificationService captures the per-project Snapshot pointer. snap is
// required; production composition always supplies the
// ProjectRuntime.Snapshot pointer so the notification catalog moves in
// lockstep with the rest of the bundle.
func NewNotificationService(snap *config.Snapshot) *NotificationService {
	return &NotificationService{snap: snap}
}

// BundleSnapshot returns the catalog wrapper consumed by
// actions.NewNotificationShowAction / NotificationShowAction.SetBundle.
// Reading through Snapshot.Notifications guarantees the catalog is the
// same view every other service holds; the returned map is a defensive
// copy owned by the snapshot, safe to hand to the action registry.
func (s *NotificationService) BundleSnapshot() actions.NotificationBundleSnapshot {
	return actions.NotificationBundleSnapshot{
		Notifications: s.snap.Notifications(),
		// Per task #82 §15-§17, the notification action expands
		// ${{intl:KEY}} tokens against the catalog so bundled presets can
		// move their hook copy into the language catalog. The
		// composition root in agentruntime is the place that picks the
		// concrete surface — naming SurfaceCLI here would couple app to
		// the catalog type system, which the arch test forbids. The
		// composition root sets Catalog after constructing the snapshot.
		Catalog: nil,
	}
}
