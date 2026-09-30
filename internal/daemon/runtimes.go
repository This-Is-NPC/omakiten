package daemon

import (
	"context"

	"omakiten/internal/agentruntime"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/httpapi"
	"omakiten/internal/knowledgefile"
)

// runtimes resolves each project's runtime the way the TUI does: the
// session project keeps the session runtime; another project uses its
// repo-local workflow when present, else the session workflow.
type runtimes struct {
	session agentruntime.Session
}

func (r runtimes) Project(ctx context.Context, slug string) (httpapi.Operations, contract.ProjectSelector, error) {
	runtime, project, err := r.resolve(ctx, slug)
	if err != nil {
		return nil, contract.ProjectSelector{}, err
	}
	return runtime.Service.ForHTTP(), contract.ProjectSelector{ProjectID: project.ID}, nil
}

func (r runtimes) Knowledge(ctx context.Context, slug string) (domain.KnowledgeSnapshot, error) {
	project, err := r.session.Store.FindProjectBySlug(ctx, slug)
	if err != nil {
		return domain.KnowledgeSnapshot{}, err
	}
	return knowledgefile.Load(ctx, project.Context(), true, r.session.Store.FindProjectBySlug), nil
}

func (r runtimes) Snapshot(ctx context.Context, slug string) (*config.Snapshot, error) {
	runtime, _, err := r.resolve(ctx, slug)
	if err != nil {
		return nil, err
	}
	return runtime.Snapshot, nil
}

func (r runtimes) resolve(ctx context.Context, slug string) (*agentruntime.ProjectRuntime, domain.Project, error) {
	project, err := r.session.Store.FindProjectBySlug(ctx, slug)
	if err != nil {
		return nil, domain.Project{}, err
	}
	if project.ID == r.session.Project.ID {
		runtime, err := r.session.Cache.Resolve(ctx, r.session.CacheProjectID, r.session.ConfigPath)
		return runtime, project, err
	}
	path, found, err := config.RepoLocalConfigFile(project.RootPath)
	if err != nil {
		return nil, domain.Project{}, err
	}
	if !found {
		if r.session.RepoLocalDir != "" {
			return nil, domain.Project{}, domain.NewError(domain.ErrConfigInvalid, "project has no workflow reachable from this daemon; start okt serve outside a repo-local install", map[string]any{"project": slug})
		}
		path = r.session.ConfigPath
	}
	runtime, err := r.session.Cache.Resolve(ctx, project.ID, path)
	return runtime, project, err
}

// Global returns the session runtime's facade.
func (r runtimes) Global(ctx context.Context) (httpapi.Operations, error) {
	runtime, err := r.session.Cache.Resolve(ctx, r.session.CacheProjectID, r.session.ConfigPath)
	if err != nil {
		return nil, err
	}
	return runtime.Service.ForHTTP(), nil
}

func (r runtimes) Catalog() *config.Catalog {
	runtime, err := r.session.Cache.Resolve(context.Background(), r.session.CacheProjectID, r.session.ConfigPath)
	if err != nil || runtime.Snapshot == nil {
		return r.session.Snapshot.Catalog(config.SurfaceGUI)
	}
	return runtime.Snapshot.Catalog(config.SurfaceGUI)
}
