package project

import (
	"context"
	"testing"

	"omakiten/internal/domain"
)

func TestResolverResolve(t *testing.T) {
	repo := fakeProjectRepo{
		byID: map[int64]domain.Project{
			1: {ID: 1, Name: "Omakiten", Slug: "omakiten", RootPath: "/work/omakiten"},
		},
		bySlug: map[string]domain.Project{
			"omakiten": {ID: 1, Name: "Omakiten", Slug: "omakiten", RootPath: "/work/omakiten"},
		},
		containing: []domain.Project{
			{ID: 2, Name: "Parent", Slug: "parent", RootPath: "/work"},
			{ID: 1, Name: "Omakiten", Slug: "omakiten", RootPath: "/work/omakiten"},
		},
	}

	tests := []struct {
		name string
		opts ResolveOptions
		want int64
	}{
		{name: "by id", opts: ResolveOptions{ProjectID: 1}, want: 1},
		{name: "by slug", opts: ResolveOptions{Project: "omakiten"}, want: 1},
		{name: "by cwd", opts: ResolveOptions{CWD: "/work/omakiten/internal"}, want: 1},
	}

	resolver := NewResolver(repo)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Resolve(context.Background(), tt.opts)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.ID != tt.want {
				t.Fatalf("Resolve() id = %d, want %d", got.ID, tt.want)
			}
		})
	}
}

type fakeProjectRepo struct {
	byID       map[int64]domain.Project
	bySlug     map[string]domain.Project
	containing []domain.Project
}

func (f fakeProjectRepo) UpsertProject(context.Context, string, string, string) (domain.Project, error) {
	panic("not used")
}

func (f fakeProjectRepo) FindProjectByID(_ context.Context, id int64) (domain.Project, error) {
	return f.byID[id], nil
}

func (f fakeProjectRepo) FindProjectBySlug(_ context.Context, slug string) (domain.Project, error) {
	return f.bySlug[slug], nil
}

func (f fakeProjectRepo) FindProjectsContainingPath(context.Context, string) ([]domain.Project, error) {
	return f.containing, nil
}

func (f fakeProjectRepo) ListProjects(context.Context) ([]domain.Project, error) {
	return nil, nil
}
