package agentruntime

import (
	"context"
	"path/filepath"
	"testing"

	"omakiten/internal/contract"
)

func TestBundledKitLeavesCommandsToWorkflowPresets(t *testing.T) {
	ctx := context.Background()
	rt := openCommandTestRuntime(t, ctx)
	listed, err := rt.Service().ListCommands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Commands) != 0 {
		t.Fatalf("bundled kit exposes commands: %+v", listed.Commands)
	}
	if _, err := rt.Service().ResolveCommand(ctx, contract.ResolveCommandInput{Name: "okt-shape"}); err == nil {
		t.Fatal("uninstalled workflow command resolved")
	}
}

func openCommandTestRuntime(t *testing.T, ctx context.Context) *Runtime {
	t.Helper()
	root := t.TempDir()
	rt, err := Open(ctx, Options{DBPath: filepath.Join(root, "data", "omakiten.db"), ConfigPath: filepath.Join(root, "config", "omakase.yaml"), CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}
