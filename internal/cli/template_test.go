package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLITemplateListShow exercises the read-only template catalog path
// through operation.Service (not app.TemplateService).
func TestCLITemplateListShow(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	assertTemplateCatalog(t, dbPath, configPath)
}

func assertTemplateCatalog(t *testing.T, dbPath, configPath string) {
	assertTemplateList(t, dbPath, configPath)
	t.Run("list kind filter", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "template", "list", "--kind", "pr")
		if !strings.Contains(out, `"slug":"pull-request"`) {
			t.Fatalf("template list --kind=pr missing pull-request: %s", out)
		}
		if strings.Contains(out, `"slug":"user-story"`) {
			t.Fatalf("template list --kind=pr should exclude user-story: %s", out)
		}
	})
	t.Run("list include-body", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "template", "list", "--kind", "task", "--include-body")
		if !strings.Contains(out, `"body":`) {
			t.Fatalf("template list --include-body missing body: %s", out)
		}
	})
	assertTemplateShow(t, dbPath, configPath)
}

func assertTemplateList(t *testing.T, dbPath, configPath string) {
	t.Run("list", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "template", "list")
		if !strings.Contains(out, `"templates"`) {
			t.Fatalf("template list missing templates key: %s", out)
		}
		// Shipped defaults include the task default (user-story) and PR scaffold.
		if !strings.Contains(out, `"slug":"user-story"`) {
			t.Fatalf("template list missing user-story: %s", out)
		}
		if !strings.Contains(out, `"slug":"pull-request"`) {
			t.Fatalf("template list missing pull-request: %s", out)
		}
		// Bodies omitted by default.
		var envelope map[string]any
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatalf("Unmarshal list envelope: %v (%s)", err, out)
		}
		data, _ := envelope["data"].(map[string]any)
		templates, _ := data["templates"].([]any)
		if len(templates) == 0 {
			t.Fatalf("template list empty: %s", out)
		}
		for _, raw := range templates {
			row, _ := raw.(map[string]any)
			if body, _ := row["body"].(string); body != "" {
				t.Fatalf("default list should omit body, got %q in %+v", body, row)
			}
		}
	})
}

func assertTemplateShow(t *testing.T, dbPath, configPath string) {
	t.Run("show", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "template", "show", "user-story")
		if !strings.Contains(out, `"slug":"user-story"`) || !strings.Contains(out, `"body":`) {
			t.Fatalf("template show envelope missing fields: %s", out)
		}
	})

	t.Run("show missing", func(t *testing.T) {
		envelope := runCLIExpectError(t, dbPath, configPath, "validation_error", "template", "show", "no-such-template")
		if envelope["code"] != "validation_error" {
			t.Fatalf("code = %v, want validation_error (%v)", envelope["code"], envelope)
		}
	})
}
