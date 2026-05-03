package project

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

type Resolver struct {
	repo app.ProjectRepository
}

type ResolveOptions struct {
	ProjectID int64
	Project   string
	CWD       string
}

func NewResolver(repo app.ProjectRepository) *Resolver {
	return &Resolver{repo: repo}
}

func (r *Resolver) Resolve(ctx context.Context, opts ResolveOptions) (domain.ProjectContext, error) {
	if opts.ProjectID > 0 {
		project, err := r.repo.FindProjectByID(ctx, opts.ProjectID)
		if err != nil {
			return domain.ProjectContext{}, err
		}
		return project.Context(), nil
	}

	if strings.TrimSpace(opts.Project) != "" {
		project, err := r.repo.FindProjectBySlug(ctx, opts.Project)
		if err != nil {
			return domain.ProjectContext{}, err
		}
		return project.Context(), nil
	}

	cwd := opts.CWD
	if cwd == "" {
		cwd = "."
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return domain.ProjectContext{}, err
	}

	projects, err := r.repo.FindProjectsContainingPath(ctx, absCWD)
	if err != nil {
		return domain.ProjectContext{}, err
	}
	if len(projects) == 0 {
		return domain.ProjectContext{}, domain.NewError(domain.ErrProjectNotFound, "no registered project matches current directory", map[string]any{"cwd": absCWD})
	}
	sort.SliceStable(projects, func(i, j int) bool {
		return len(projects[i].RootPath) > len(projects[j].RootPath)
	})

	return projects[0].Context(), nil
}
