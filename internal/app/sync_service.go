package app

import (
	"context"
	"fmt"

	"omakiten/internal/domain"
)

const (
	// syncPushPage is the most changes sent in one push request.
	syncPushPage = 500
	// syncPullPage is the most changes requested in one pull request.
	syncPullPage = 500
)

// SyncService exchanges changes of synced projects with the remote server.
type SyncService struct {
	store  SyncStore
	remote SyncRemote
}

// NewSyncService wires the sync flows. remote is nil when no server URL is
// configured; the flows that need it then fail with a validation error.
func NewSyncService(store SyncStore, remote SyncRemote) *SyncService {
	return &SyncService{store: store, remote: remote}
}

func (s *SyncService) requireRemote() error {
	if s.remote == nil {
		return domain.NewError(domain.ErrValidation,
			"no sync remote configured; run `okt remote set <url>`", nil)
	}
	return nil
}

// Sync pushes then pulls every synced project, or only projectID when set.
func (s *SyncService) Sync(ctx context.Context, projectID *int64) (domain.SyncReport, error) {
	if err := s.requireRemote(); err != nil {
		return domain.SyncReport{}, err
	}
	projects, err := s.store.SyncedProjects(ctx)
	if err != nil {
		return domain.SyncReport{}, err
	}
	if projectID != nil {
		projects, err = selectSyncProject(projects, *projectID)
		if err != nil {
			return domain.SyncReport{}, err
		}
	}
	report := domain.SyncReport{Projects: make([]domain.SyncProjectReport, 0, len(projects))}
	for _, p := range projects {
		pushed, err := s.push(ctx, p)
		if err != nil {
			return report, fmt.Errorf("sync %s: push: %w", p.Info.Slug, err)
		}
		pulled, err := s.pull(ctx, p.ID, p.Info.UID, p.PullCursor)
		if err != nil {
			return report, fmt.Errorf("sync %s: pull: %w", p.Info.Slug, err)
		}
		report.Projects = append(report.Projects, domain.SyncProjectReport{Slug: p.Info.Slug, Pushed: pushed, Pulled: pulled})
	}
	return report, nil
}

func selectSyncProject(projects []domain.SyncProject, id int64) ([]domain.SyncProject, error) {
	for _, p := range projects {
		if p.ID == id {
			return []domain.SyncProject{p}, nil
		}
	}
	return nil, domain.NewError(domain.ErrProjectNotFound,
		fmt.Sprintf("project %d is not a synced project", id), map[string]any{"project_id": id})
}

// push sends pending changes page by page and advances the push watermark.
func (s *SyncService) push(ctx context.Context, p domain.SyncProject) (int, error) {
	total := 0
	for {
		changes, err := s.store.PendingChanges(ctx, p.ID, syncPushPage)
		if err != nil {
			return total, err
		}
		if len(changes) == 0 {
			return total, nil
		}
		if _, err := s.remote.Push(ctx, p.Info, changes); err != nil {
			return total, err
		}
		if err := s.store.MarkPushed(ctx, p.ID, changes[len(changes)-1].HLC); err != nil {
			return total, err
		}
		total += len(changes)
		if len(changes) < syncPushPage {
			return total, nil
		}
	}
}

// pull applies remote changes page by page from cursor until none follow.
func (s *SyncService) pull(ctx context.Context, projectID int64, uid domain.UID, cursor int64) (int, error) {
	total := 0
	for {
		changes, next, more, err := s.remote.Pull(ctx, uid, cursor, syncPullPage)
		if err != nil {
			return total, err
		}
		if len(changes) > 0 || next != cursor {
			if err := s.store.ApplyChanges(ctx, projectID, changes, next); err != nil {
				return total, err
			}
		}
		total += len(changes)
		cursor = next
		if !more {
			return total, nil
		}
	}
}

// Clone creates a synced local project for a remote project and pulls its rows.
func (s *SyncService) Clone(ctx context.Context, uid domain.UID, root string) (domain.Project, error) {
	if err := s.requireRemote(); err != nil {
		return domain.Project{}, err
	}
	infos, err := s.remote.ListProjects(ctx)
	if err != nil {
		return domain.Project{}, err
	}
	for _, info := range infos {
		if info.UID != uid {
			continue
		}
		project, err := s.store.CreateClonedProject(ctx, info, root)
		if err != nil {
			return domain.Project{}, err
		}
		if _, err := s.pull(ctx, project.ID, uid, 0); err != nil {
			return project, fmt.Errorf("clone %s: pull: %w", info.Slug, err)
		}
		return project, nil
	}
	return domain.Project{}, domain.NewError(domain.ErrProjectNotFound,
		fmt.Sprintf("remote has no project %s", uid), map[string]any{"uid": string(uid)})
}

// RemoteProjects lists the projects the remote server holds.
func (s *SyncService) RemoteProjects(ctx context.Context) ([]domain.SyncProjectInfo, error) {
	if err := s.requireRemote(); err != nil {
		return nil, err
	}
	return s.remote.ListProjects(ctx)
}

// Remote returns the configured server URL.
func (s *SyncService) Remote(ctx context.Context) (string, error) {
	return s.store.SyncRemoteURL(ctx)
}

// SetRemote validates and stores the server URL.
func (s *SyncService) SetRemote(ctx context.Context, rawURL string) error {
	if err := domain.ValidateSyncRemoteURL(rawURL); err != nil {
		return err
	}
	return s.store.SetSyncRemoteURL(ctx, rawURL)
}
