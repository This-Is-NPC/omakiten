package agentruntime

import (
	"omakiten/internal/config"
	"omakiten/internal/hooks/actions"
)

// notificationBundleSnapshot binds the root and sub-kit notification catalogs.
func notificationBundleSnapshot(snapshot *config.Snapshot) actions.NotificationBundleSnapshot {
	out := actions.NotificationBundleSnapshot{
		Notifications:      snapshot.Notifications(),
		NotificationsByKit: map[string]map[string]config.Notification{},
		Catalog:            nil,
	}
	if rootKey := snapshot.Kit().Key; rootKey != "" {
		out.NotificationsByKit[rootKey] = snapshot.Notifications()
	}
	if sub, ok := snapshot.SubtaskKit(); ok {
		if subKey := sub.Kit().Key; subKey != "" {
			out.NotificationsByKit[subKey] = sub.Notifications()
		}
	}
	return out
}
