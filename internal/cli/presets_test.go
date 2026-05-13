package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigPresetsListsMenu(t *testing.T) {
	output := runCLI(t, filepath.Join(t.TempDir(), "omakiten.db"), filepath.Join(t.TempDir(), "config", "omakase.yaml"), "config", "presets")
	for _, want := range []string{"omakase", "izakaya", "kaiseki", "shokunin", "Chef's choice"} {
		if !strings.Contains(output, want) {
			t.Fatalf("config presets output missing %q: %s", want, output)
		}
	}
}

func TestCLIInitPresetCopiesFlatConfigToGitRoot(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	globalConfigPath := filepath.Join(tmp, "global", "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	deepDir := filepath.Join(projectRoot, "src", "pkg")
	if err := os.MkdirAll(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.git) error = %v", err)
	}
	if err := os.MkdirAll(deepDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(deepDir) error = %v", err)
	}
	t.Chdir(deepDir)

	output := runCLI(t, dbPath, globalConfigPath, "init", "--preset", "izakaya", "--name", "Project", "--slug", "project")
	if !strings.Contains(output, `"preset"`) || !strings.Contains(output, `"root":"`+projectRoot) {
		t.Fatalf("init --preset output = %s, want preset rooted at git root %s", output, projectRoot)
	}

	presetConfigPath := filepath.Join(projectRoot, ".omakiten", "config", "izakaya.yaml")
	data, err := os.ReadFile(presetConfigPath)
	if err != nil {
		t.Fatalf("ReadFile(preset config) error = %v", err)
	}
	if !strings.Contains(string(data), "key: izakaya") {
		t.Fatalf("preset config = %s, want izakaya", string(data))
	}

	runCLIExpectError(t, dbPath, globalConfigPath, "validation_error", "init", "--preset", "izakaya", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, globalConfigPath, "init", "--preset", "omakase", "--preset-force", "--name", "Project", "--slug", "project")
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
		"izakaya": {
			{"comment", "add", "1", "-b", "hypothesis question", "--tag", "hypothesis"},
			{"move", "1", "-t", "dev"},
			{"comment", "add", "1", "-b", "note"},
			{"move", "1", "-t", "done"},
		},
		"kaiseki": {
			{"comment", "add", "1", "-b", "5w2h elicitation", "--tag", "5w2h"},
			{"comment", "add", "1", "-b", "requirements", "--tag", "requirements"},
			{"comment", "add", "1", "-b", "acceptance criteria", "--tag", "acceptance"},
			{"move", "1", "-t", "planning"},
			{"comment", "add", "1", "-b", "branch", "--tag", "self-branch"},
			{"comment", "add", "1", "-b", "design recorded", "--tag", "design"},
			{"move", "1", "-t", "dev"},
			{"comment", "add", "1", "-b", "handoff", "--tag", "resume"},
			{"comment", "add", "1", "-b", "test-evidence", "--tag", "tests-passing"},
			{"move", "1", "-t", "review"},
			{"comment", "add", "1", "-b", "peer reviewed", "--tag", "peer-review"},
			{"move", "1", "-t", "docs"},
			{"comment", "add", "1", "-b", "docs", "--tag", "documentation"},
			{"move", "1", "-t", "done"},
		},
		"shokunin": {
			{"comment", "add", "1", "-b", "5w2h elicitation", "--tag", "5w2h"},
			{"comment", "add", "1", "-b", "requirements", "--tag", "requirements"},
			{"comment", "add", "1", "-b", "acceptance criteria", "--tag", "acceptance"},
			{"move", "1", "-t", "planning"},
			{"comment", "add", "1", "-b", "branch", "--tag", "self-branch"},
			{"comment", "add", "1", "-b", "pre-mortem", "--tag", "pre-mortem"},
			{"comment", "add", "1", "-b", "risk", "--tag", "risk-assessment"},
			{"move", "1", "-t", "dev"},
			{"comment", "add", "1", "-b", "handoff", "--tag", "resume"},
			{"comment", "add", "1", "-b", "tests", "--tag", "tests-passing"},
			{"comment", "add", "1", "-b", "rollback steps", "--tag", "rollback-plan"},
			{"move", "1", "-t", "review"},
			{"comment", "add", "1", "-b", "reviewer A", "--tag", "peer-review"},
			{"comment", "add", "1", "-b", "reviewer B", "--tag", "peer-review"},
			{"move", "1", "-t", "docs"},
			{"comment", "add", "1", "-b", "docs", "--tag", "documentation"},
			{"comment", "add", "1", "-b", "lessons", "--tag", "lessons-learned"},
			{"move", "1", "-t", "done"},
		},
	}

	for preset, steps := range flows {
		t.Run(preset, func(t *testing.T) {
			tmp := t.TempDir()
			dbPath := filepath.Join(tmp, "omakiten.db")
			projectRoot := filepath.Join(tmp, "project")
			if err := os.MkdirAll(projectRoot, 0o755); err != nil {
				t.Fatalf("MkdirAll(projectRoot) error = %v", err)
			}
			t.Chdir(projectRoot)

			globalConfigPath := filepath.Join(tmp, "global", "config", "omakase.yaml")
			runCLI(t, dbPath, globalConfigPath, "init", "--preset", preset, "--name", preset, "--slug", preset)
			presetConfigPath := filepath.Join(projectRoot, ".omakiten", "config", preset+".yaml")
			runCLI(t, dbPath, presetConfigPath, "add", "-t", "Preset task")
			for _, args := range steps {
				runCLI(t, dbPath, presetConfigPath, args...)
			}
		})
	}
}
