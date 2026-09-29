package installer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/testfixtures"
)

func TestCatalogPresetUpdatesAndWorksOffline(t *testing.T) {
	fixture := t.TempDir()
	gitConfig, err := testfixtures.PresetGitConfig(fixture)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitConfig)
	scope := t.TempDir()
	ctx := context.Background()
	first, err := InstallPreset(ctx, scope, "omakase", false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(fixture, "repository")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("Updated workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitPresetFixture(t, repo)
	updated, err := InstallPreset(ctx, scope, "omakase", true)
	if err != nil || !updated.Refreshed {
		t.Fatalf("refresh: %+v, %v", updated, err)
	}
	after, err := os.ReadFile(updated.Path)
	if err != nil || string(after) == string(before) {
		t.Fatalf("new repository revision was not selected: %v", err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	offline, err := InstallPreset(ctx, scope, "omakase", false)
	if err != nil || !offline.NoOp {
		t.Fatalf("offline reuse: %+v, %v", offline, err)
	}
	assertModifiedPresetSurvivesUpdate(t, ctx, scope, first.Path)
}

func assertModifiedPresetSurvivesUpdate(t *testing.T, ctx context.Context, scope, selection string) {
	t.Helper()
	bundle, err := config.LoadBundle(selection)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Config.Agent.MaxCommentChars = 321
	if err := config.SaveBundle(selection, bundle); err != nil {
		t.Fatal(err)
	}
	modified, err := InstallPreset(ctx, scope, "omakase", true)
	if err != nil || !modified.NoOp || modified.PresetName != "omakase-local" {
		t.Fatalf("modified preset was replaced: %+v, %v", modified, err)
	}
	loaded, err := config.LoadBundle(selection)
	if err != nil || loaded.Config.Agent.MaxCommentChars != 321 {
		t.Fatalf("custom setting lost: %v", err)
	}
}

func TestPresetFetchFailureLeavesSelectionIntact(t *testing.T) {
	root := t.TempDir()
	source := "file://" + filepath.ToSlash(filepath.Join(root, "missing"))
	_, err := InstallPreset(context.Background(), root, source, false)
	if err == nil || !strings.Contains(err.Error(), "okt preset add <directory>") {
		t.Fatalf("fetch error lacks recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, config.PresetSelectionFile)); !os.IsNotExist(err) {
		t.Fatalf("failed fetch published a selection: %v", err)
	}
}

func commitPresetFixture(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{
		{"-C", repo, "add", "."},
		{"-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "test: update fixture"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture commit: %v: %s", err, output)
		}
	}
}
