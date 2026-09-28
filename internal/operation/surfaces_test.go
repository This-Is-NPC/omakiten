package operation

import (
	"context"
	"errors"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
)

func snapshotWithSurfaces(t *testing.T, table config.SurfaceTable) *config.Snapshot {
	t.Helper()
	bundle := agentTestBundle(t)
	bundle.Surfaces = table
	return config.BuildSnapshot(bundle)
}

func TestForCLISetSnapshotUngated(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	row := table["wiring.set_snapshot"]
	if row.CLI == nil || *row.CLI {
		t.Fatal("canonical wiring.set_snapshot must be cli:false")
	}
	orig := NewService(nil, contract.ProjectSelector{})
	cli := orig.ForCLI()
	cli.SetSnapshot(snapshotWithSurfaces(t, table))
	if orig.Snapshot() != nil {
		t.Fatal("ForCLI copy must not mutate the original Service")
	}
	if cli.Snapshot() == nil {
		t.Fatal("SetSnapshot on the agent copy did not stick")
	}
}

func TestForCLIProductDenied(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	deniedFlag := false
	row := table["template.list"]
	row.CLI = &deniedFlag
	row.Reason = "${{intl:operations.denied.agent_delete}}"
	table["template.list"] = row

	svc := NewService(nil, contract.ProjectSelector{}).ForCLI()
	svc.SetSnapshot(snapshotWithSurfaces(t, table))
	_, err := svc.ListTemplates(context.Background(), contract.ListTemplatesInput{})
	var denied OperationDenied
	if !errors.As(err, &denied) {
		t.Fatalf("ListTemplates err = %v, want OperationDenied", err)
	}
	if denied.Surface != SurfaceCLI || denied.Op != "template.list" {
		t.Fatalf("denied = %+v", denied)
	}
	if denied.Reason != "${{intl:operations.denied.agent_delete}}" {
		t.Fatalf("Reason = %q", denied.Reason)
	}
}

func TestNewServiceZeroSurfaceAllowsProduct(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	deniedFlag := false
	row := table["template.list"]
	row.CLI = &deniedFlag
	row.TUI = &deniedFlag
	row.CLI = &deniedFlag
	row.Reason = "all off"
	table["template.list"] = row

	svc := NewService(nil, contract.ProjectSelector{})
	svc.SetSnapshot(snapshotWithSurfaces(t, table))
	if _, err := svc.ListTemplates(context.Background(), contract.ListTemplatesInput{}); err != nil {
		t.Fatalf("zero-surface ListTemplates = %v", err)
	}
}

func TestForTUISyncBlockersDenied(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	deniedFlag := false
	row := table["dependency.add"]
	row.TUI = &deniedFlag
	row.Reason = "blockers hidden in tui"
	table["dependency.add"] = row

	svc := NewService(nil, contract.ProjectSelector{}).ForTUI()
	svc.SetSnapshot(snapshotWithSurfaces(t, table))
	err := svc.SyncBlockers(context.Background(), contract.SyncBlockersInput{TaskID: 1})
	var denied OperationDenied
	if !errors.As(err, &denied) {
		t.Fatalf("SyncBlockers err = %v, want OperationDenied", err)
	}
	if denied.Surface != SurfaceTUI || denied.Op != "dependency.add" {
		t.Fatalf("denied = %+v", denied)
	}
}

func TestForCLIVsForTUI(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	cliOn, tuiOff := true, false
	row := table["template.list"]
	row.CLI = &cliOn
	row.TUI = &tuiOff
	row.Reason = "tui off"
	table["template.list"] = row

	svc := NewService(nil, contract.ProjectSelector{})
	svc.SetSnapshot(snapshotWithSurfaces(t, table))

	if _, err := svc.ForCLI().ListTemplates(context.Background(), contract.ListTemplatesInput{}); err != nil {
		t.Fatalf("ForCLI ListTemplates = %v", err)
	}
	_, err := svc.ForTUI().ListTemplates(context.Background(), contract.ListTemplatesInput{})
	var denied OperationDenied
	if !errors.As(err, &denied) {
		t.Fatalf("ForTUI err = %v, want OperationDenied", err)
	}
	if denied.Surface != SurfaceTUI || denied.Op != "template.list" || denied.Reason != "tui off" {
		t.Fatalf("denied = %+v", denied)
	}
}

func TestForCLIListCommandsDenied(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	deniedFlag := false
	row := table["command.list"]
	row.CLI = &deniedFlag
	row.Reason = "commands hidden from agents"
	table["command.list"] = row

	svc := NewService(nil, contract.ProjectSelector{}).ForCLI()
	svc.SetSnapshot(snapshotWithSurfaces(t, table))
	_, err := svc.ListCommands(context.Background())
	var denied OperationDenied
	if !errors.As(err, &denied) {
		t.Fatalf("ListCommands err = %v, want OperationDenied", err)
	}
	if denied.Surface != SurfaceCLI || denied.Op != "command.list" {
		t.Fatalf("denied = %+v", denied)
	}
}

func TestForCLIAssignTaskDenied(t *testing.T) {
	table := config.CanonicalSurfaceTable()
	deniedFlag := false
	row := table["task.assign"]
	row.CLI = &deniedFlag
	row.Reason = "assignee hidden on cli"
	table["task.assign"] = row

	svc := NewService(nil, contract.ProjectSelector{}).ForCLI()
	svc.SetSnapshot(snapshotWithSurfaces(t, table))
	_, err := svc.AssignTask(context.Background(), contract.AssignTaskInput{TaskID: 1, Assignee: "alice"})
	var denied OperationDenied
	if !errors.As(err, &denied) {
		t.Fatalf("AssignTask err = %v, want OperationDenied", err)
	}
	if denied.Surface != SurfaceCLI || denied.Op != "task.assign" {
		t.Fatalf("denied = %+v", denied)
	}
}
