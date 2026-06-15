package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

// TestAdapterCommandsResolveByteIdenticalToPrompt pins AC#4 (byte-stability):
// the `commands.resolve` tool must return the exact same composed markdown the
// prompt path (`GetPrompt`) produces for the same name + arguments, so the
// Anthropic prompt-cache hit is preserved whether a playbook is reached as a
// user slash prompt or an agent tool call.
func TestAdapterCommandsResolveByteIdenticalToPrompt(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	cases := []struct {
		name string
		args map[string]any
	}{
		{name: "okt"},
		{name: "okt-audit"},
		{name: "okt-run", args: map[string]any{"target": "1175"}},
		{name: "okt-task-implement", args: map[string]any{"task_id": 42}},
		{name: "okt-task-continue", args: map[string]any{"task_id": 7}},
	}

	for _, tc := range cases {
		prompt, err := adapter.GetPrompt(ctx, tc.name, tc.args)
		if err != nil {
			t.Fatalf("GetPrompt(%s) error = %v", tc.name, err)
		}
		want := prompt.Messages[0].Content.Text

		toolArgs := map[string]any{"name": tc.name}
		if tc.args != nil {
			toolArgs["arguments"] = tc.args
		}
		res, err := adapter.CallTool(ctx, "commands.resolve", withModel(toolArgs))
		if err != nil {
			t.Fatalf("CallTool(commands.resolve, %s) error = %v", tc.name, err)
		}
		if res.IsError {
			t.Fatalf("CallTool(commands.resolve, %s) IsError = true: %s", tc.name, res.Content[0].Text)
		}
		if len(res.Content) == 0 {
			t.Fatalf("CallTool(commands.resolve, %s) Content empty", tc.name)
		}
		got := res.Content[0].Text
		if got != want {
			t.Fatalf("commands.resolve(%s) markdown not byte-identical to prompt path\n--- tool ---\n%s\n--- prompt ---\n%s", tc.name, got, want)
		}
	}
}

// TestAdapterCommandsResolveAllReachable pins AC#2: every name in
// command_table.go is reachable through the tool path with no allowlist subset
// and none rejected as unknown. Reachability is `!IsError` (the prompt path's
// own contract) — the rendered body's content fidelity is pinned separately by
// the byte-identical test; the bare test bundle wires no playbook skills, so a
// command with no bindings legitimately renders empty.
func TestAdapterCommandsResolveAllReachable(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	names := agent.CommandNames()
	if len(names) == 0 {
		t.Fatal("CommandNames() empty")
	}
	for _, name := range names {
		res, err := adapter.CallTool(ctx, "commands.resolve", withModel(map[string]any{"name": name}))
		if err != nil {
			t.Fatalf("CallTool(commands.resolve, %s) error = %v", name, err)
		}
		if res.IsError {
			t.Fatalf("CallTool(commands.resolve, %s) IsError = true: %s", name, res.Content[0].Text)
		}
		if len(res.Content) == 0 {
			t.Fatalf("CallTool(commands.resolve, %s) returned no content", name)
		}
	}
}

// TestAdapterCommandsListEnumeratesAll pins AC#2 discovery: commands.list
// surfaces every command_table slug so an agent can discover the catalog from
// its tool-list without a human-typed slash.
func TestAdapterCommandsListEnumeratesAll(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	res, err := adapter.CallTool(ctx, "commands.list", withModel(nil))
	if err != nil {
		t.Fatalf("CallTool(commands.list) error = %v", err)
	}
	if res.IsError {
		t.Fatalf("CallTool(commands.list) IsError = true: %s", res.Content[0].Text)
	}
	var listed []PromptDefinition
	if err := json.Unmarshal([]byte(res.Content[0].Text), &listed); err != nil {
		t.Fatalf("commands.list payload not []PromptDefinition: %v\n%s", err, res.Content[0].Text)
	}
	got := make(map[string]struct{}, len(listed))
	for _, d := range listed {
		got[d.Name] = struct{}{}
	}
	for _, name := range agent.CommandNames() {
		if _, ok := got[name]; !ok {
			t.Fatalf("commands.list missing %q", name)
		}
	}
	if len(listed) != len(agent.CommandNames()) {
		t.Fatalf("commands.list count = %d, want %d", len(listed), len(agent.CommandNames()))
	}
}

// TestAdapterCommandsResolvePassesArguments pins AC#2 argument pass-through:
// the tool path renders invocation arguments identically to the prompt path.
func TestAdapterCommandsResolvePassesArguments(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	res, err := adapter.CallTool(ctx, "commands.resolve", withModel(map[string]any{
		"name":      "okt-task-continue",
		"arguments": map[string]any{"task_id": 42},
	}))
	if err != nil {
		t.Fatalf("CallTool(commands.resolve) error = %v", err)
	}
	body := res.Content[0].Text
	if !strings.Contains(body, "## Invocation Args\n") {
		t.Fatalf("resolved playbook missing invocation args section:\n%s", body)
	}
	if !strings.Contains(body, "- `task_id`: 42") {
		t.Fatalf("resolved playbook missing task_id argument:\n%s", body)
	}
}

// TestAdapterCommandsResolveUnknown pins that an unknown command name fails as
// a structured IsError tool result (not a transport error), mirroring how the
// prompt path rejects unknown names.
func TestAdapterCommandsResolveUnknown(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	res, err := adapter.CallTool(ctx, "commands.resolve", withModel(map[string]any{"name": "okt-not-a-real-command"}))
	if err != nil {
		t.Fatalf("CallTool(commands.resolve, unknown) transport error = %v", err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(commands.resolve, unknown) IsError = false, want true:\n%s", res.Content[0].Text)
	}
}

// TestAdapterCommandsToolsRegistered pins AC#1: both command tools live in the
// agent-callable tool-list (so the Skill/tool search can find them), unlike the
// prompt-only surface that caused the original `Unknown skill` repro.
func TestAdapterCommandsToolsRegistered(t *testing.T) {
	want := map[string]bool{"commands.resolve": false, "commands.list": false}
	for _, tool := range Tools() {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("tool %q not registered in Tools()", name)
		}
	}
}
