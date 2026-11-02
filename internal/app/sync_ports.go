package app

import (
	"context"

	"omakiten/internal/domain"
)

// SyncStore is the local persistence the sync engine reads and writes.
type SyncStore interface {
	// SyncedProjects lists the local projects whose scope is synced.
	SyncedProjects(ctx context.Context) ([]domain.SyncProject, error)
	// PendingChanges returns up to limit recorded changes of the project that are newer than its pushed HLC.
	PendingChanges(ctx context.Context, projectID int64, limit int) ([]domain.SyncChange, error)
	// MarkPushed records hlc as the project's push watermark.
	MarkPushed(ctx context.Context, projectID int64, hlc string) error
	// ApplyChanges applies remote rows of the project last-writer-wins, stores the pull cursor and observes the
	// clock in one transaction. It returns how many changes it rejected: invalid, foreign to the project or
	// violating a constraint. Only an infrastructure failure is an error.
	ApplyChanges(ctx context.Context, projectID int64, changes []domain.SyncChange, cursor int64) (rejected int, err error)
	// CreateClonedProject creates a synced local project from remote info, rooted at root.
	CreateClonedProject(ctx context.Context, info domain.SyncProjectInfo, root string) (domain.Project, error)
	// SyncRemoteURL returns the configured server URL.
	SyncRemoteURL(ctx context.Context) (string, error)
	// SetSyncRemoteURL stores the server URL.
	SetSyncRemoteURL(ctx context.Context, url string) error
}

// SyncRemote is the server the sync engine exchanges changes with.
type SyncRemote interface {
	// ListProjects returns the projects the server holds.
	ListProjects(ctx context.Context) ([]domain.SyncProjectInfo, error)
	// Push sends changes for a project and returns the server cursor.
	Push(ctx context.Context, info domain.SyncProjectInfo, changes []domain.SyncChange) (cursor int64, err error)
	// Pull returns up to limit changes after the cursor and whether more follow.
	Pull(ctx context.Context, projectUID domain.UID, after int64, limit int) (changes []domain.SyncChange, cursor int64, more bool, err error)
}

// SyncKits names the workflow kit of a project, which both machines of a synced project must share.
type SyncKits interface {
	// KitKeyOf returns the kit key a registered project resolves to.
	KitKeyOf(ctx context.Context, projectID int64) (string, error)
	// KitKeyAt returns the kit key a project rooted at root would resolve to.
	KitKeyAt(ctx context.Context, root string) (string, error)
}
