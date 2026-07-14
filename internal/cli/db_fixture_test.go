package cli

import (
	"os"
	"path/filepath"
	"testing"
)

type cliDBFixture struct {
	root       string
	dbPath     string
	configPath string
}

func newCLIDBFixture(t *testing.T, databaseName string) cliDBFixture {
	t.Helper()
	root := t.TempDir()
	fixture := cliDBFixture{
		root:       root,
		dbPath:     filepath.Join(root, databaseName),
		configPath: filepath.Join(root, "config", "omakase.yaml"),
	}
	projectRoot := filepath.Join(root, "project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll project root: %v", err)
	}
	t.Chdir(projectRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	runCLI(t, fixture.dbPath, fixture.configPath, "init", "--name", "Project", "--slug", "project")
	return fixture
}
