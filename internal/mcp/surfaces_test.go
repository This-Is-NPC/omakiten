package mcp

import (
	"context"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/operation"
)

func TestAdapterToolsWithoutSnapshotListsAll(t *testing.T) {
	got := NewAdapter(nil).Tools()
	want := Tools()
	if len(got) != len(want) {
		t.Fatalf("NewAdapter(nil).Tools() count = %d, want %d (unfiltered)", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name {
			t.Fatalf("Tools()[%d] = %q, want %q", i, got[i].Name, want[i].Name)
		}
	}
}

func TestAdapterToolsOmitsDeniedMCP(t *testing.T) {
	ctx := context.Background()
	svc := newMCPTestService(t, ctx)
	table := config.CanonicalSurfaceTable()
	denied := false
	row := table["task.delete"]
	row.MCP = &denied
	row.Reason = "agents must not delete"
	table["task.delete"] = row

	bundle := mcpTestBundle(t)
	bundle.Surfaces = table
	svc.SetSnapshot(config.BuildSnapshot(bundle))

	adapter := NewAdapter(svc)
	tools := adapter.Tools()

	names := make(map[string]struct{}, len(tools))
	for _, td := range tools {
		names[td.Name] = struct{}{}
	}
	if _, ok := names["tasks.delete"]; ok {
		t.Fatal("Adapter.Tools() still lists tasks.delete when task.delete mcp:false")
	}
	if _, ok := names["commands.list"]; !ok {
		t.Fatal("Adapter.Tools() dropped commands.list (sibling of the denied slug)")
	}
	if _, ok := names["tasks.list"]; !ok {
		t.Fatal("Adapter.Tools() dropped tasks.list")
	}
	if _, ok := names["tasks.move"]; !ok {
		t.Fatal("Adapter.Tools() dropped tasks.move")
	}
	if got, want := len(tools), len(Tools())-1; got != want {
		t.Fatalf("Adapter.Tools() count = %d, want %d (one omitted)", got, want)
	}

	result, err := adapter.CallTool(ctx, "tasks.delete", withModel(map[string]any{
		"task_id":   1,
		"confirmed": true,
	}))
	if err == nil && !result.IsError {
		t.Fatal("CallTool(tasks.delete) succeeded; omitted tools must fail visibly")
	}
	if err != nil {
		if !strings.Contains(err.Error(), "denied") && !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("CallTool(tasks.delete) err = %v, want denied or unknown", err)
		}
		return
	}
	text := ""
	if len(result.Content) > 0 {
		text = result.Content[0].Text
	}
	if !strings.Contains(text, "denied") && !strings.Contains(text, "unknown") && !strings.Contains(text, "task.delete") {
		t.Fatalf("CallTool(tasks.delete) error payload %q is not a visible denial", text)
	}
}

func TestAdapterToolsReflectsSetSnapshot(t *testing.T) {
	ctx := context.Background()
	svc := newMCPTestService(t, ctx)
	adapter := NewAdapter(svc)

	if !toolNamed(adapter.Tools(), "tasks.delete") {
		t.Fatal("canonical snapshot should list tasks.delete")
	}

	table := config.CanonicalSurfaceTable()
	denied := false
	row := table["task.delete"]
	row.MCP = &denied
	row.Reason = "agents must not delete"
	table["task.delete"] = row
	bundle := mcpTestBundle(t)
	bundle.Surfaces = table
	svc.SetSnapshot(config.BuildSnapshot(bundle))

	if toolNamed(adapter.Tools(), "tasks.delete") {
		t.Fatal("Adapter.Tools() did not drop tasks.delete after SetSnapshot")
	}
	if !toolNamed(adapter.Tools(), "commands.list") {
		t.Fatal("commands.list missing after SetSnapshot")
	}
}

func TestAdapterToolsOmitsDeniedCommandsList(t *testing.T) {
	ctx := context.Background()
	svc := newMCPTestService(t, ctx)
	table := config.CanonicalSurfaceTable()
	denied := false
	row := table["command.list"]
	row.MCP = &denied
	row.Reason = "agents must not list commands"
	table["command.list"] = row

	bundle := mcpTestBundle(t)
	bundle.Surfaces = table
	svc.SetSnapshot(config.BuildSnapshot(bundle))

	adapter := NewAdapter(svc)
	if toolNamed(adapter.Tools(), "commands.list") {
		t.Fatal("Adapter.Tools() still lists commands.list when command.list mcp:false")
	}
	if !toolNamed(adapter.Tools(), "commands.resolve") {
		t.Fatal("Adapter.Tools() dropped commands.resolve")
	}
}

func TestCensusSlugForMCPToolCoversEveryTool(t *testing.T) {
	for _, td := range Tools() {
		if _, ok := operation.CensusSlugForMCPTool[td.Name]; !ok {
			t.Errorf("MCP tool %q has no census slug", td.Name)
		}
	}
}

func toolNamed(tools []ToolDefinition, name string) bool {
	for _, td := range tools {
		if td.Name == name {
			return true
		}
	}
	return false
}
