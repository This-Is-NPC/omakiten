package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omakiten/internal/agent"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/sqlite"
	"omakiten/internal/testfixtures"
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
		"metrics.summary":     false,
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

// withModel injects the coercive _agent_model field every CallTool now
// requires. Tests use a stable sentinel so events emitted during the run
// are easy to filter when debugging.
func withModel(extra map[string]any) map[string]any {
	args := map[string]any{"_agent_model": "test-model"}
	for k, v := range extra {
		args[k] = v
	}
	return args
}

// TestToolsDeclareAgentAttributionSchema pins the contract that every
// registered tool exposes _agent_model (required) and _agent_session_id
// (optional) on its InputSchema. Without this declaration, schema-aware
// clients strip the reserved fields before sending the call, which makes
// extractAgentAttribution reject every request with a self-describing
// error the LLM cannot act on (the field is invisible to it).
func TestToolsDeclareAgentAttributionSchema(t *testing.T) {
	for _, tool := range Tools() {
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: InputSchema.properties missing or wrong type: %#v", tool.Name, tool.InputSchema["properties"])
		}
		modelSchema, ok := props["_agent_model"].(map[string]any)
		if !ok {
			t.Fatalf("%s: properties._agent_model missing", tool.Name)
		}
		if modelSchema["type"] != "string" {
			t.Fatalf("%s: _agent_model.type = %v, want string", tool.Name, modelSchema["type"])
		}
		desc, _ := modelSchema["description"].(string)
		if !strings.Contains(desc, "claude-opus-4-7") || !strings.Contains(desc, "Required") {
			t.Fatalf("%s: _agent_model.description missing exemplars or required hint: %q", tool.Name, desc)
		}
		sessionSchema, ok := props["_agent_session_id"].(map[string]any)
		if !ok {
			t.Fatalf("%s: properties._agent_session_id missing", tool.Name)
		}
		if sessionSchema["type"] != "string" {
			t.Fatalf("%s: _agent_session_id.type = %v, want string", tool.Name, sessionSchema["type"])
		}

		required, ok := tool.InputSchema["required"].([]string)
		if !ok {
			t.Fatalf("%s: InputSchema.required missing or wrong type: %#v", tool.Name, tool.InputSchema["required"])
		}
		found := false
		for _, name := range required {
			if name == "_agent_model" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s: required missing _agent_model: %v", tool.Name, required)
		}
		if found2 := false; func() bool {
			for _, name := range required {
				if name == "_agent_session_id" {
					return true
				}
			}
			return found2
		}() {
			t.Fatalf("%s: required must NOT include _agent_session_id (it is optional): %v", tool.Name, required)
		}
	}
}

func TestAdapterCallToolReturnsCompactJSONText(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)

	result, err := NewAdapter(service).CallTool(ctx, "project.overview", withModel(nil))
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

	result, err := NewAdapter(service).CallTool(ctx, "tasks.continue", withModel(map[string]any{"task_id": 9999}))
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
		"metrics.summary",
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
		_, err := adapter.CallTool(ctx, name, withModel(args))
		if err != nil {
			t.Fatalf("CallTool(%s) error = %v", name, err)
		}
	}
}

func TestAdapterCallToolRequiresAgentModel(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// No _agent_model at all.
	_, err := adapter.CallTool(ctx, "project.overview", nil)
	if err == nil {
		t.Fatal("CallTool(missing _agent_model) error = nil, want validation_error")
	}
	if !strings.Contains(err.Error(), "_agent_model is required") {
		t.Fatalf("CallTool error = %v, want '_agent_model is required'", err)
	}

	// Empty _agent_model.
	_, err = adapter.CallTool(ctx, "project.overview", map[string]any{"_agent_model": ""})
	if err == nil {
		t.Fatal("CallTool(empty _agent_model) error = nil, want validation_error")
	}
	if !strings.Contains(err.Error(), "_agent_model must be a non-empty string") {
		t.Fatalf("CallTool error = %v, want non-empty validation", err)
	}
}

func TestAdapterCallToolStripsAgentFieldsBeforeDecoding(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// _agent_session_id is opt-in but must still be removed before the
	// tool-specific decoder sees it (otherwise a strict decoder might fail
	// on unknown fields).
	args := map[string]any{
		"_agent_model":      "test-model",
		"_agent_session_id": "sess-9",
		"description":       "boom",
	}
	result, err := adapter.CallTool(ctx, "errors.record", args)
	if err != nil {
		t.Fatalf("CallTool(errors.record) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(errors.record) failed: %v", result.Content)
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
	if err := store.ImportBundle(ctx, mcpTestBundle(t), "test.yaml", "hash"); err != nil {
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
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return agent.NewService(store, agent.ProjectSelector{CWD: root})
}

// TestAdapterServiceResolverRoutesByProjectArg locks in the Phase 3b
// invariant: when SetServiceResolver is wired, CallTool peeks the
// project / project_id args and dispatches against whichever service
// the resolver hands back. The default service is the fallback for
// calls without a project arg, and for resolver replies of (nil, nil).
func TestAdapterServiceResolverRoutesByProjectArg(t *testing.T) {
	ctx := context.Background()

	storeA, projectA := newMCPProjectFixture(t, ctx, "alpha")
	storeB, projectB := newMCPProjectFixture(t, ctx, "bravo")

	defaultService := agent.NewService(storeA, agent.ProjectSelector{ProjectID: projectA.ID})
	projectBService := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID})

	adapter := NewAdapter(defaultService)
	var observed []string
	adapter.SetServiceResolver(func(_ context.Context, project string, projectID int64) (*agent.Service, error) {
		observed = append(observed, fmt.Sprintf("project=%q id=%d", project, projectID))
		if project == "bravo" || projectID == projectB.ID {
			return projectBService, nil
		}
		return nil, nil
	})

	// Default routing (no project arg): observed call still happens
	// (resolver invoked with zero values) but the service stays the
	// adapter default — projectA.
	result, err := adapter.CallTool(ctx, "project.overview", withModel(map[string]any{}))
	if err != nil {
		t.Fatalf("CallTool default: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool default returned error: %+v", result)
	}

	// Explicit project="bravo": resolver returns projectB's service, so
	// the overview is computed against storeB's tasks (which are
	// distinct from storeA's).
	result, err = adapter.CallTool(ctx, "project.overview", withModel(map[string]any{"project": "bravo"}))
	if err != nil {
		t.Fatalf("CallTool bravo: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool bravo returned error: %+v", result)
	}

	if len(observed) != 2 {
		t.Fatalf("resolver invoked %d times, want 2: %v", len(observed), observed)
	}
	if !strings.Contains(observed[0], `project=""`) {
		t.Fatalf("first resolver call should observe empty project, got %q", observed[0])
	}
	if !strings.Contains(observed[1], `project="bravo"`) {
		t.Fatalf("second resolver call should observe project=bravo, got %q", observed[1])
	}
}

// newMCPProjectFixture builds a self-contained sqlite store + project +
// task triple keyed by slug. Used by per-project routing tests where
// two adapters need to point at distinct underlying state.
func newMCPProjectFixture(t *testing.T, ctx context.Context, slug string) (*sqlite.Store, domain.Project) {
	t.Helper()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "omakiten.db"))
	if err != nil {
		t.Fatalf("Open(%s): %v", slug, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.ImportBundle(ctx, mcpTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle(%s): %v", slug, err)
	}
	root := filepath.Join(t.TempDir(), slug)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", slug, err)
	}
	project, err := store.UpsertProject(ctx, slug, slug, root)
	if err != nil {
		t.Fatalf("UpsertProject(%s): %v", slug, err)
	}
	if _, err := store.CreateTask(ctx, project.ID, "T-"+slug, "", domain.Priority(2), "backlog"); err != nil {
		t.Fatalf("CreateTask(%s): %v", slug, err)
	}
	return store, project
}

// TestPeekProjectArg exercises the typed-vs-string handling that JSON
// decoding can produce for project_id. The MCP protocol uses
// json.Unmarshal which lands integers as float64; the JSON-RPC layer
// may also pass json.Number when configured for arbitrary-precision.
func TestPeekProjectArg(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		project   string
		projectID int64
	}{
		{name: "empty", args: map[string]any{}, project: "", projectID: 0},
		{name: "string project", args: map[string]any{"project": "alpha"}, project: "alpha", projectID: 0},
		{name: "float64 id", args: map[string]any{"project_id": float64(7)}, project: "", projectID: 7},
		{name: "int64 id", args: map[string]any{"project_id": int64(9)}, project: "", projectID: 9},
		{name: "int id", args: map[string]any{"project_id": 11}, project: "", projectID: 11},
		{name: "json.Number id", args: map[string]any{"project_id": json.Number("13")}, project: "", projectID: 13},
		{name: "non-string project ignored", args: map[string]any{"project": 42}, project: "", projectID: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, id := peekProjectArg(tt.args)
			if project != tt.project || id != tt.projectID {
				t.Fatalf("peekProjectArg = (%q, %d), want (%q, %d)", project, id, tt.project, tt.projectID)
			}
		})
	}
}

func mcpTestBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}}
	return bundle
}
