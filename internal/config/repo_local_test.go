package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRepoLocal_FindsFromNestedSubdir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	repo := filepath.Join(tmp, "repo")
	nested := filepath.Join(repo, "sub", "nested", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	wantDir := filepath.Join(repo, RepoLocalDirName)
	if err := os.MkdirAll(wantDir, 0o755); err != nil {
		t.Fatalf("mkdir .omakiten: %v", err)
	}

	got, ok, err := FindRepoLocal(nested)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if !ok {
		t.Fatalf("FindRepoLocal: not found; want %s", wantDir)
	}
	if got != wantDir {
		t.Fatalf("FindRepoLocal: got %s, want %s", got, wantDir)
	}
}

func TestFindRepoLocal_NotFoundUnderHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	sub := filepath.Join(tmp, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, ok, err := FindRepoLocal(sub)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if ok {
		t.Fatalf("FindRepoLocal: got %s, want not found", got)
	}
}

func TestFindRepoLocal_StopsAtHome(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	home := filepath.Join(parent, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(parent, RepoLocalDirName), 0o755); err != nil {
		t.Fatalf("mkdir parent/.omakiten: %v", err)
	}
	t.Setenv("HOME", home)

	got, ok, err := FindRepoLocal(home)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if ok {
		t.Fatalf("FindRepoLocal walked above $HOME, returned %s", got)
	}
}

func TestFindRepoLocal_MatchesAtHome(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	wantDir := filepath.Join(home, RepoLocalDirName)
	if err := os.MkdirAll(wantDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj", "sub")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}

	got, ok, err := FindRepoLocal(proj)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if !ok {
		t.Fatalf("FindRepoLocal: not found; want %s", wantDir)
	}
	if got != wantDir {
		t.Fatalf("FindRepoLocal: got %s, want %s", got, wantDir)
	}
}

func TestFindRepoLocal_StartDirIsRepoRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	repo := filepath.Join(tmp, "repo")
	wantDir := filepath.Join(repo, RepoLocalDirName)
	if err := os.MkdirAll(wantDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, ok, err := FindRepoLocal(repo)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if !ok || got != wantDir {
		t.Fatalf("FindRepoLocal: got (%s, %v), want (%s, true)", got, ok, wantDir)
	}
}

func TestFindRepoLocal_FileNamedDotOmakitenIgnored(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A plain file (not a directory) must not match.
	if err := os.WriteFile(filepath.Join(repo, RepoLocalDirName), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, ok, err := FindRepoLocal(repo)
	if err != nil {
		t.Fatalf("FindRepoLocal err: %v", err)
	}
	if ok {
		t.Fatalf("FindRepoLocal matched a file: %s", got)
	}
}

func TestFindRepoLocal_EmptyStartDir(t *testing.T) {
	got, ok, err := FindRepoLocal("")
	if err != nil {
		t.Fatalf("FindRepoLocal(\"\") err: %v", err)
	}
	if ok {
		t.Fatalf("FindRepoLocal(\"\"): got %s, want no hit", got)
	}
}
