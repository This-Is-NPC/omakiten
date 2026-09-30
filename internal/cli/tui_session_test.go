package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"omakiten/internal/agentruntime"
	"omakiten/internal/domain"
)

func TestCLITUISuppliesTheInteractiveRunnerSession(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
	for name, project := range map[string]string{"project": "example", "home": "", "unknown project": "missing"} {
		t.Run(name, func(t *testing.T) {
			checkInteractiveSession(t, db, cfg, name, project)
		})
	}
}

func checkInteractiveSession(t *testing.T, db, cfg, name, project string) {
	t.Helper()
	t.Chdir(t.TempDir())
	called := false
	finished := errors.New("interactive runner finished")
	var session agentruntime.Session
	cmd := NewRootCommand("0.31.0", Runners{Interactive: func(_ context.Context, supplied agentruntime.Session) error {
		called, session = true, supplied
		return finished
	}})
	cmd.SetArgs([]string{"--db", db, "--config", cfg, "--project", project, "tui"})
	err := cmd.Execute()
	if name == "unknown project" {
		var coded *domain.CodedError
		if called || !errors.As(err, &coded) || coded.Code != domain.ErrProjectNotFound {
			t.Fatalf("unknown project launched runner: called=%t, err=%v", called, err)
		}
		return
	}
	if !called || !errors.Is(err, finished) || session.ConfigPath != cfg || session.DBPath != db || session.Version != "0.31.0" || session.Store == nil || session.Snapshot == nil {
		t.Fatalf("session: %+v, err=%v", session, err)
	}
	if name == "home" && session.Project.ID != 0 {
		t.Fatalf("home selected a project: %+v", session.Project)
	}
	if name == "project" && session.Project.Slug != "example" {
		t.Fatalf("explicit project lost: %+v", session.Project)
	}
}
