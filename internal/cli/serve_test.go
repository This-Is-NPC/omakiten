package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"omakiten/internal/agentruntime"
	"omakiten/internal/contract"
)

func TestCLIServeSuppliesSessionAndOptions(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	db, cfg := filepath.Join(root, "state.db"), filepath.Join(root, "config", "omakase.yaml")
	runCLI(t, db, cfg, "init", "--name", "Example", "--slug", "example")
	t.Chdir(t.TempDir())

	finished := errors.New("server runner finished")
	var session agentruntime.Session
	var options contract.ServeOptions
	cmd := NewRootCommand("0.31.0", Runners{Serve: func(_ context.Context, supplied agentruntime.Session, opts contract.ServeOptions) error {
		session, options = supplied, opts
		return finished
	}})
	cmd.SetArgs([]string{"--db", db, "--config", cfg, "serve", "--addr", "127.0.0.1:7766", "--poll", "1s"})
	if err := cmd.Execute(); !errors.Is(err, finished) {
		t.Fatalf("serve err = %v, want the runner's result", err)
	}
	if session.Store == nil || session.Snapshot == nil || session.Project.ID != 0 || session.DBPath != db {
		t.Fatalf("session = %+v, want an opened session outside every project", session)
	}
	if options.Addr != "127.0.0.1:7766" || options.Poll != time.Second || options.Stderr == nil {
		t.Fatalf("options = %+v", options)
	}
}

func TestCLIServeWithoutRunnerFails(t *testing.T) {
	cmd := NewRootCommand("test", Runners{})
	cmd.SetArgs([]string{"serve"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("serve without a runner succeeded")
	}
}
