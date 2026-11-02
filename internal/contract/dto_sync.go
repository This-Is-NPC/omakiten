package contract

import "omakiten/internal/domain"

// SetSyncRemoteInput names the sync server to store.
type SetSyncRemoteInput struct {
	URL string `json:"url"`
}

// SyncRemoteResponse carries the configured sync server; URL is empty when none is set.
type SyncRemoteResponse struct {
	URL string `json:"url"`
}

// SyncRemoteProjectsResponse lists the projects the sync server holds.
type SyncRemoteProjectsResponse struct {
	Projects []domain.SyncProjectInfo `json:"projects"`
}

// SyncInput selects what to sync. An empty selector syncs every synced project.
type SyncInput struct {
	ProjectSelector
}

// CloneProjectInput names a remote project and the local directory to root it in.
type CloneProjectInput struct {
	UID      string `json:"uid"`
	RootPath string `json:"root_path"`
}

// CloneProjectResponse returns the project created from the remote.
type CloneProjectResponse struct {
	Project ProjectSummary `json:"project"`
}
