package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLILawSkillPersonaListShow routes the read-only catalog commands through
// operation.Service (ListLaws/ShowLaw, ListSkills/ShowSkill, ListPersonas/ShowPersona).
// Add/edit/remove also go through the facade (AddLaw/EditLaw/RemoveLaw, …).
func TestCLILawSkillPersonaListShow(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")

	assertLawCatalog(t, dbPath, configPath)
	assertSkillCatalog(t, dbPath, configPath)
	assertPersonaCatalog(t, dbPath, configPath)
}

func assertLawCatalog(t *testing.T, dbPath, configPath string) {
	assertLawList(t, dbPath, configPath)
	assertLawShow(t, dbPath, configPath)
	assertLawScope(t, dbPath, configPath)
	runCLIExpectError(t, dbPath, configPath, "law_not_found", "law", "show", "no-such-law")
}

func assertLawList(t *testing.T, dbPath, configPath string) {
	out := runCLI(t, dbPath, configPath, "law", "list")
	if !strings.Contains(out, `"laws"`) {
		t.Fatalf("law list missing laws key: %s", out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("Unmarshal list envelope: %v (%s)", err, out)
	}
	data, _ := envelope["data"].(map[string]any)
	laws, _ := data["laws"].([]any)
	if len(laws) == 0 {
		t.Fatalf("law list empty: %s", out)
	}
	for _, raw := range laws {
		row, _ := raw.(map[string]any)
		if body, _ := row["body"].(string); body != "" {
			t.Fatalf("default list should omit body, got %q in %+v", body, row)
		}
		if _, ok := row["slug"]; !ok {
			t.Fatalf("law list row missing slug: %+v", row)
		}
	}

}

func assertLawShow(t *testing.T, dbPath, configPath string) {
	out := runCLI(t, dbPath, configPath, "law", "show", "project-scope-only")
	if !strings.Contains(out, `"slug":"project-scope-only"`) || !strings.Contains(out, `"body":`) {
		t.Fatalf("law show envelope missing fields: %s", out)
	}

}

func assertLawScope(t *testing.T, dbPath, configPath string) {
	out := runCLI(t, dbPath, configPath, "law", "list", "--scope", "global")
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("Unmarshal scoped list: %v (%s)", err, out)
	}
	data, _ := envelope["data"].(map[string]any)
	laws, _ := data["laws"].([]any)
	if len(laws) == 0 {
		t.Fatalf("scoped law list empty: %s", out)
	}
	for _, raw := range laws {
		row, _ := raw.(map[string]any)
		if scope, _ := row["scope"].(string); scope != "global" {
			t.Fatalf("law list --scope=global returned non-global row: %+v", row)
		}
	}
}

func assertSkillCatalog(t *testing.T, dbPath, configPath string) {
	t.Run("skill list and show", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "skill", "list")
		if !strings.Contains(out, `"slug":"implementation"`) {
			t.Fatalf("skill list missing implementation: %s", out)
		}
		out = runCLI(t, dbPath, configPath, "skill", "show", "implementation")
		if !strings.Contains(out, `"slug":"implementation"`) || !strings.Contains(out, `"body":`) {
			t.Fatalf("skill show envelope missing fields: %s", out)
		}
	})
}

func assertPersonaCatalog(t *testing.T, dbPath, configPath string) {
	t.Run("persona list and show", func(t *testing.T) {
		out := runCLI(t, dbPath, configPath, "persona", "list")
		if !strings.Contains(out, `"slug":"naruto-uzumaki"`) {
			t.Fatalf("persona list missing naruto-uzumaki: %s", out)
		}
		var envelope map[string]any
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatalf("Unmarshal persona list: %v (%s)", err, out)
		}
		data, _ := envelope["data"].(map[string]any)
		personas, _ := data["personas"].([]any)
		for _, raw := range personas {
			row, _ := raw.(map[string]any)
			if body, _ := row["body"].(string); body != "" {
				t.Fatalf("persona list should omit body, got %q in %+v", body, row)
			}
		}
		out = runCLI(t, dbPath, configPath, "persona", "show", "naruto-uzumaki")
		if !strings.Contains(out, `"slug":"naruto-uzumaki"`) || !strings.Contains(out, `"body":`) {
			t.Fatalf("persona show envelope missing fields: %s", out)
		}
		if !strings.Contains(out, `"skill_repertoire"`) && !strings.Contains(out, `"skills"`) {
			t.Fatalf("persona show missing expanded skill refs: %s", out)
		}
	})
}
