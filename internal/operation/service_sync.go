package operation

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// SyncRemoteFactory connects to the sync server at url. The composition root
// supplies it, so the operation layer reaches no transport adapter.
type SyncRemoteFactory func(url string) (app.SyncRemote, error)

// SetSync injects the sync server connector and the per-project
// kit resolver. A Service without them refuses every operation that talks to
// the server.
func (s *Service) SetSync(factory SyncRemoteFactory, kits app.SyncKits) {
	s.syncRemote = factory
	s.syncKits = kits
}

// syncService builds the sync flows over the repository. The remote is
// connected only when withRemote is set and a server URL is stored.
func (s *Service) syncService(ctx context.Context, withRemote bool) (*app.SyncService, error) {
	var remote app.SyncRemote
	if withRemote {
		url, err := s.repo.SyncRemoteURL(ctx)
		if err != nil {
			return nil, err
		}
		if url != "" {
			if s.syncRemote == nil || s.syncKits == nil {
				return nil, domain.NewError(domain.ErrValidation, "sync is not available in this runtime", nil)
			}
			if remote, err = s.syncRemote(url); err != nil {
				return nil, err
			}
		}
	}
	return app.NewSyncService(s.repo, remote, s.syncKits), nil
}

// SetSyncRemote validates and stores the sync server URL.
func (s *Service) SetSyncRemote(ctx context.Context, input contract.SetSyncRemoteInput) (contract.SyncRemoteResponse, error) {
	if err := s.allow("remote.set"); err != nil {
		return contract.SyncRemoteResponse{}, err
	}
	sync, err := s.syncService(ctx, false)
	if err != nil {
		return contract.SyncRemoteResponse{}, err
	}
	url := strings.TrimSpace(input.URL)
	if err := sync.SetRemote(ctx, url); err != nil {
		return contract.SyncRemoteResponse{}, err
	}
	return contract.SyncRemoteResponse{URL: url}, nil
}

// ShowSyncRemote returns the stored sync server URL.
func (s *Service) ShowSyncRemote(ctx context.Context) (contract.SyncRemoteResponse, error) {
	if err := s.allow("remote.show"); err != nil {
		return contract.SyncRemoteResponse{}, err
	}
	sync, err := s.syncService(ctx, false)
	if err != nil {
		return contract.SyncRemoteResponse{}, err
	}
	url, err := sync.Remote(ctx)
	return contract.SyncRemoteResponse{URL: url}, err
}

// ListSyncRemoteProjects lists the projects the sync server holds.
func (s *Service) ListSyncRemoteProjects(ctx context.Context) (contract.SyncRemoteProjectsResponse, error) {
	if err := s.allow("remote.projects"); err != nil {
		return contract.SyncRemoteProjectsResponse{}, err
	}
	sync, err := s.syncService(ctx, true)
	if err != nil {
		return contract.SyncRemoteProjectsResponse{}, err
	}
	projects, err := sync.RemoteProjects(ctx)
	return contract.SyncRemoteProjectsResponse{Projects: projects}, err
}

// Sync pushes then pulls every synced project, or only the one the input
// selector names.
func (s *Service) Sync(ctx context.Context, input contract.SyncInput) (domain.SyncReport, error) {
	if err := s.allow("sync"); err != nil {
		return domain.SyncReport{}, err
	}
	sync, err := s.syncService(ctx, true)
	if err != nil {
		return domain.SyncReport{}, err
	}
	var projectID *int64
	if input.ProjectID > 0 || strings.TrimSpace(input.Project) != "" {
		project, err := s.resolveProject(ctx, input.ProjectSelector)
		if err != nil {
			return domain.SyncReport{}, err
		}
		projectID = &project.ID
	}
	return sync.Sync(ctx, projectID)
}

// CloneProject creates a synced local project from a remote project and pulls its rows.
func (s *Service) CloneProject(ctx context.Context, input contract.CloneProjectInput) (contract.CloneProjectResponse, error) {
	if err := s.allow("project.clone"); err != nil {
		return contract.CloneProjectResponse{}, err
	}
	uid, err := domain.ParseUID(strings.TrimSpace(input.UID))
	if err != nil {
		return contract.CloneProjectResponse{}, err
	}
	sync, err := s.syncService(ctx, true)
	if err != nil {
		return contract.CloneProjectResponse{}, err
	}
	project, err := sync.Clone(ctx, uid, input.RootPath)
	if err != nil {
		return contract.CloneProjectResponse{}, err
	}
	return contract.CloneProjectResponse{Project: projectSummary(project.Context())}, nil
}
