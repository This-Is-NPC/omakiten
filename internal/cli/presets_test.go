package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPresetCatalogListsRepositories(t *testing.T) {
	db := filepath.Join(t.TempDir(), "omakiten.db")
	output := runCLI(t, db, filepath.Join(t.TempDir(), "config.yaml"), "preset", "catalog")
	for _, want := range []string{"okt-workflow-omakase.git", "okt-workflow-izakaya.git", "okt-workflow-kaiseki.git", "okt-workflow-shokunin.git", "Chef's choice"} {
		if !strings.Contains(output, want) {
			t.Fatalf("preset catalog output missing %q: %s", want, output)
		}
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("catalog created or required a database: %v", err)
	}
}

func TestCLIPresetWorkflowsEndToEnd(t *testing.T) {
	flows := map[string][][]string{
		"omakase": {
			{"comment", "add", "1", "-b", "branch", "--tag", "self-branch"},
			{"move", "1", "-t", "dev"},
			{"comment", "add", "1", "-b", "handoff", "--tag", "resume"},
			{"comment", "add", "1", "-b", "test-evidence", "--tag", "tests-passing"},
			{"move", "1", "-t", "review"},
			{"comment", "add", "1", "-b", "docs", "--tag", "documentation"},
			{"move", "1", "-t", "done"},
		},
	}

	for preset, steps := range flows {
		t.Run(preset, func(t *testing.T) {
			tmp := t.TempDir()
			dbPath := filepath.Join(tmp, "omakiten.db")
			projectRoot := filepath.Join(tmp, "project")
			if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
				t.Fatalf("MkdirAll(projectRoot) error = %v", err)
			}
			t.Chdir(projectRoot)

			globalConfigPath := filepath.Join(tmp, "global", "config", "omakase.yaml")
			runCLI(t, dbPath, globalConfigPath, "init", "--preset", preset, "--name", preset, "--slug", preset)
			presetConfigPath := filepath.Join(projectRoot, ".omakiten", "config.yaml")
			runCLI(t, dbPath, presetConfigPath, "task", "create", "--confirm", "-t", "Preset task")
			for _, args := range steps {
				runCLI(t, dbPath, presetConfigPath, args...)
			}
		})
	}
}
