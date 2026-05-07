package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
	"omakiten/internal/config"
	"omakiten/internal/sqlite"
)

func TestToolsIncludePlannedSurface(t *testing.T) {
	want := map[string]bool{
		"project.overview":    false,
		"tasks.create_intent": false,
		"tasks.continue":      false,
		"project.resume":      false,
		"tasks.list":          false,
		"tasks.create":        false,
		"tasks.move":          false,
		"comments.add":        false,
		"comments.list":       false,
		"dependencies.add":    false,
		"dependencies.remove": false,
		"dependencies.list":   false,
		"context.dump":        false,
		"context.add":         false,
		"workflow.show":       false,
		"progress.record":     false,
		"errors.record":       false,
		"errors.search":       false,
		"solutions.add":       false,
		"solutions.confirm":   false,
	}
	for _, tool := range Tools() {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("Tools() missing %s", name)
		}
	}
}

func TestAdapterCallToolReturnsCompactJSONText(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "project.overview", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool().IsError = true, content = %#v", result.Content)
	}
	if len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("CallTool().Content = %#v, want one text item", result.Content)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("content is not JSON: %v", err)
	}
	if payload["project"] == nil || payload["workflow"] == nil || payload["next_step_prompt"] == nil {
		t.Fatalf("overview payload = %#v, want compact project/workflow/prompt fields", payload)
	}
}

func TestAdapterMapsDomainErrorsToToolFailures(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "tasks.continue", map[string]any{"task_id": 9999})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("CallTool().IsError = false, want true")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("content is not JSON: %v", err)
	}
	if payload["code"] != "task_not_found" {
		t.Fatalf("failure code = %v, want task_not_found", payload["code"])
	}
}

func TestAdapterCallToolAllTools(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	store := service /* agent.Service doesn't expose store directly; use adapter tests via service */
	_ = store
	adapter := NewAdapter(service)

	tools := []string{
		"project.overview",
		"project.resume",
		"tasks.continue",
		"tasks.list",
		"tasks.create_intent",
		"tasks.create",
		"tasks.move",
		"comments.add",
		"comments.list",
		"task_activity.list",
		"dependencies.add",
		"dependencies.remove",
		"dependencies.list",
		"context.add",
		"context.dump",
		"workflow.show",
		"progress.record",
		"errors.record",
		"errors.search",
		"solutions.add",
		"solutions.confirm",
	}

	for _, name := range tools {
		var args map[string]any
		switch name {
		case "tasks.continue", "comments.add", "comments.list", "task_activity.list", "dependencies.add", "dependencies.remove", "dependencies.list":
			args = map[string]any{"task_id": 1}
		case "tasks.move":
			args = map[string]any{"task_id": 1, "bucket_key": "dev"}
		case "tasks.create_intent", "tasks.create":
			args = map[string]any{"description": "test task"}
		case "context.add":
			args = map[string]any{"body": "entry"}
		case "context.dump":
			args = map[string]any{"level": 1}
		case "progress.record":
			args = map[string]any{"task_id": 1, "comment": "note"}
		case "errors.record":
			args = map[string]any{"description": "boom", "tags": []any{"sqlite"}}
		case "errors.search":
			args = map[string]any{"tags": []any{"sqlite"}}
		case "solutions.add":
			args = map[string]any{"error_id": 1, "description": "try X"}
		case "solutions.confirm":
			args = map[string]any{"solution_id": 1, "success": true}
		}
		_, err := adapter.CallTool(ctx, name, args)
		if err != nil {
			t.Fatalf("CallTool(%s) error = %v", name, err)
		}
	}
}

func TestAdapterNilService(t *testing.T) {
	ctx := context.Background()
	_, err := NewAdapter(nil).CallTool(ctx, "project.overview", nil)
	if err == nil {
		t.Fatal("CallTool(nil service) error = nil")
	}
}

func TestAdapterUnknownTool(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	_, err := NewAdapter(service).CallTool(ctx, "unknown.tool", nil)
	if err == nil {
		t.Fatal("CallTool(unknown) error = nil")
	}
}

func TestAdapterReadResource(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	result, err := adapter.ReadResource(ctx, "omakiten://project/overview")
	if err != nil {
		t.Fatalf("ReadResource() error = %v", err)
	}
	if result.IsError {
		t.Fatal("ReadResource() IsError = true")
	}

	_, err = adapter.ReadResource(ctx, "omakiten://unknown")
	if err == nil {
		t.Fatal("ReadResource(unknown) error = nil")
	}
}

func TestAdapterGetPrompt(t *testing.T) {
	ctx := context.Background()
	var adapter *Adapter // nil adapter exercises the fallback path
	prompts := []string{"okt", "okt-create", "okt-continue", "okt-resume", "okt-imagine", "okt-implement"}
	for _, name := range prompts {
		result, err := adapter.GetPrompt(ctx, name, nil)
		if err != nil {
			t.Fatalf("GetPrompt(%s) error = %v", name, err)
		}
		if len(result.Messages) == 0 {
			t.Fatalf("GetPrompt(%s) Messages empty", name)
		}
		// The fallback path always emits the cache hint — the prompt is
		// byte-stable so caching is always safe and the toggle only matters
		// once a service is wired.
		if result.Messages[0].Content.Meta == nil {
			t.Fatalf("GetPrompt(%s) Content.Meta = nil, want cache hint on fallback path", name)
		}
	}

	if _, err := adapter.GetPrompt(ctx, "unknown", nil); err == nil {
		t.Fatal("GetPrompt(unknown) error = nil")
	}
}

// TestAdapterGetPromptCacheHintToggle pins the contract for the
// `config.mcp.cache_prompts` flag: when the wired service reports
// SettingsCachePrompts()==true, the rendered content carries
// `_meta.anthropic.cache_control` ; when false, the field is omitted so
// clients sensitive to the metadata key see no hint at all.
func TestAdapterGetPromptCacheHintToggle(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	service.SetSettings(agent.ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    true,
		CachePrompts:       true,
	})
	on, err := adapter.GetPrompt(ctx, "okt", nil)
	if err != nil {
		t.Fatalf("GetPrompt(on) error = %v", err)
	}
	if on.Messages[0].Content.Meta == nil {
		t.Fatal("Meta nil with CachePrompts=true, want cache hint")
	}
	cc, ok := on.Messages[0].Content.Meta["anthropic.cache_control"]
	if !ok {
		t.Fatalf("Meta missing anthropic.cache_control: %+v", on.Messages[0].Content.Meta)
	}
	if m, ok := cc.(map[string]string); !ok || m["type"] != "ephemeral" {
		t.Fatalf("cache_control payload = %+v, want {type: ephemeral}", cc)
	}

	service.SetSettings(agent.ServiceSettings{
		RecentCommentLimit: 5,
		IncludeWorkflow:    true,
		CachePrompts:       false,
	})
	off, err := adapter.GetPrompt(ctx, "okt", nil)
	if err != nil {
		t.Fatalf("GetPrompt(off) error = %v", err)
	}
	if off.Messages[0].Content.Meta != nil {
		t.Fatalf("Meta = %+v with CachePrompts=false, want nil", off.Messages[0].Content.Meta)
	}
}

func TestAdapterDecodeArgs(t *testing.T) {
	var out struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(map[string]any{"name": "test"}, &out); err != nil {
		t.Fatalf("decodeArgs() error = %v", err)
	}
	if out.Name != "test" {
		t.Fatalf("decodeArgs() Name = %q, want test", out.Name)
	}

	// nil args should default to empty map
	if err := decodeArgs(nil, &out); err != nil {
		t.Fatalf("decodeArgs(nil) error = %v", err)
	}
}

func TestAdapterResultFromData(t *testing.T) {
	result, err := resultFromData(map[string]any{"key": "value"}, false)
	if err != nil {
		t.Fatalf("resultFromData() error = %v", err)
	}
	if result.IsError {
		t.Fatal("resultFromData() IsError = true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("resultFromData() Content len = %d, want 1", len(result.Content))
	}
}

func TestAdapterSchemaHelpers(t *testing.T) {
	if selectorSchema()["type"] != "object" {
		t.Fatal("selectorSchema() missing object type")
	}
	if stringSchema("desc")["type"] != "string" {
		t.Fatal("stringSchema() missing string type")
	}
	if integerSchema("desc")["type"] != "integer" {
		t.Fatal("integerSchema() missing integer type")
	}
	if booleanSchema("desc")["type"] != "boolean" {
		t.Fatal("booleanSchema() missing boolean type")
	}
}

func TestServeHandlesToolsList(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	var output bytes.Buffer
	if err := Serve(context.Background(), input, &output, NewAdapter(nil)); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	var response map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v, output = %s", err, output.String())
	}
	if response["error"] != nil {
		t.Fatalf("response error = %#v", response["error"])
	}
	result := response["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("tools/list returned no tools")
	}
}

func newMCPTestService(t *testing.T, ctx context.Context) *agent.Service {
	t.Helper()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "omakiten.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ImportBundle(ctx, mcpTestBundle(), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	project, err := store.UpsertProject(ctx, "Project", "project", root)
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", "", "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return agent.NewService(store, agent.ProjectSelector{CWD: root})
}

func mcpTestBundle() config.Bundle {
	return config.Bundle{
		Version: 1,
		Kit:     config.Kit{ID: 1, Key: "default", Name: "Default"},
		Config: config.Settings{
			Output:   config.OutputSettings{JSONMinified: true, OmitEmpty: true},
			Context:  config.ContextSettings{DefaultLevel: 2, MaxTokens: 12000},
			Workflow: config.WorkflowSettings{Active: "default"},
			Theme:    config.ThemeSettings{Active: "catppuccin"},
		},
		Skills:   []config.Skill{{Slug: "go", Name: "Go"}},
		Personas: []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}},
		Laws:     []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}},
		Workflows: []config.Workflow{{
			ID:   1,
			Key:  "default",
			Name: "Default",
			Buckets: []config.Bucket{
				{ID: 1, Key: "backlog", Name: "Backlog", Position: 1},
				{ID: 2, Key: "dev", Name: "Development", Position: 2},
			},
			Transitions: []config.Transition{{From: 1, To: 2}},
		}},
	}
}
