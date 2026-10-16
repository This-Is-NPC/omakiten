package project

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func TestResolverMapsGitWorktreeToRegisteredRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	main := filepath.Join(base, "proj")
	worktree := filepath.Join(base, "book-storage")
	git(t, base, "init", "-q", main)
	git(t, main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	git(t, main, "worktree", "add", "-q", worktree)
	if err := os.Mkdir(filepath.Join(worktree, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}

	repo := rootsRepo{fakeProjectRepo: fakeProjectRepo{}, roots: []domain.Project{
		{ID: 1, Slug: "bencha", RootPath: main},
		{ID: 2, Slug: "nested", RootPath: filepath.Join(main, "pkg")},
	}}
	resolver := NewResolver(repo)

	tests := []struct {
		name string
		cwd  string
		want int64
	}{
		{name: "worktree root", cwd: worktree, want: 1},
		{name: "nested root inside worktree", cwd: filepath.Join(worktree, "pkg"), want: 2},
		{name: "main checkout", cwd: main, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Resolve(context.Background(), ResolveOptions{CWD: tt.cwd})
			if err != nil {
				t.Fatalf("Resolve(%s) error = %v", tt.cwd, err)
			}
			if got.ID != tt.want {
				t.Fatalf("Resolve(%s) id = %d, want %d", tt.cwd, got.ID, tt.want)
			}
		})
	}

	outside := t.TempDir()
	var coded *domain.CodedError
	if _, err := resolver.Resolve(context.Background(), ResolveOptions{CWD: outside}); !errors.As(err, &coded) || coded.Code != domain.ErrProjectNotFound {
		t.Fatalf("Resolve(outside) error = %v, want %s", err, domain.ErrProjectNotFound)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// rootsRepo matches registered roots against the queried path.
type rootsRepo struct {
	fakeProjectRepo
	roots []domain.Project
}

func (r rootsRepo) FindProjectsContainingPath(_ context.Context, path string) ([]domain.Project, error) {
	var matches []domain.Project
	for _, project := range r.roots {
		if rel, err := filepath.Rel(project.RootPath, path); err == nil && !strings.HasPrefix(rel, "..") {
			matches = append(matches, project)
		}
	}
	return matches, nil
}
