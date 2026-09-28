package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInsightsAndMetricsSummary(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "omakiten.db")
	configPath := filepath.Join(tmp, "config", "omakase.yaml")
	projectRoot := filepath.Join(tmp, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectRoot) error = %v", err)
	}
	t.Chdir(projectRoot)

	runCLI(t, dbPath, configPath, "init", "--name", "Project", "--slug", "project")
	runCLI(t, dbPath, configPath, "add", "--title", "Insightable task")

	insights := runCLI(t, dbPath, configPath, "insights", "summary", "--stuck-days", "7")
	if !strings.Contains(insights, `"schema_version"`) {
		t.Fatalf("insights summary missing schema_version: %s", insights)
	}
	if !strings.Contains(insights, `"insights"`) && !strings.Contains(insights, `"stuck_days"`) {
		t.Fatalf("insights summary missing insights payload: %s", insights)
	}

	metrics := runCLI(t, dbPath, configPath, "metrics", "summary", "--period", "7d")
	if !strings.Contains(metrics, `"summary"`) {
		t.Fatalf("metrics summary missing summary: %s", metrics)
	}
}
