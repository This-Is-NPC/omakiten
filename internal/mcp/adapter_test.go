package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"omakiten/internal/agent"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
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
		"search":              false,
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
		"search",
		"solutions.add",
		"solutions.confirm",
		"metrics.summary",
		"plans.create",
		"plans.list",
		"plans.show",
		"plans.add_wave",
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
		case "search":
			args = map[string]any{"query": "sqlite", "entity_types": []any{"error"}}
		case "solutions.add":
			args = map[string]any{"error_id": 1, "description": "try X"}
		case "solutions.confirm":
			args = map[string]any{"solution_id": 1, "success": true}
		case "plans.create":
			args = map[string]any{"slug": "demo-plan-" + name, "name": "Demo"}
		case "plans.show":
			args = map[string]any{"slug": "demo-plan-plans.create"}
		case "plans.add_wave":
			args = map[string]any{"slug": "demo-plan-plans.create", "name": "wave"}
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
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
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
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", store.Snapshot()); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	svc := agent.NewService(store, agent.ProjectSelector{CWD: root})
	svc.SetSnapshot(store.Snapshot())
	return svc
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
	defaultService.SetSnapshot(storeA.Snapshot())
	projectBService := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID})
	projectBService.SetSnapshot(storeB.Snapshot())

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
func newMCPProjectFixture(t *testing.T, ctx context.Context, slug string) (*snapstore.Store, domain.Project) {
	t.Helper()
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
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
	if _, err := store.CreateTask(ctx, project.ID, "T-"+slug, "", domain.Priority(2), "backlog", store.Snapshot()); err != nil {
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

// TestAdapterServiceResolverIsolatesGuards locks the Phase 3b invariant:
// per-project bundles carry per-project guards. Bundle A puts a
// comments_min(count=2) guard on backlog→dev; bundle B leaves the
// same transition unguarded. The same tasks.move call routed to the
// two services must error in A (no comments yet) and succeed in B —
// proof that the routing does not collapse to a single workflow shape
// behind the adapter.
func TestAdapterServiceResolverIsolatesGuards(t *testing.T) {
	ctx := context.Background()

	bundleA := mcpTestBundle(t)
	bundleA.Workflows[0].Transitions = []config.Transition{
		{From: 1, To: 2, Guards: []config.TransitionGuard{{Type: "comments_min", Count: 2, Hint: "Need 2 comments"}}},
	}
	bundleB := mcpTestBundle(t)
	bundleB.Workflows[0].Transitions = []config.Transition{{From: 1, To: 2}}

	storeA, projectA, taskA := newMCPProjectWithBundle(t, ctx, "alpha", bundleA)
	storeB, projectB, taskB := newMCPProjectWithBundle(t, ctx, "bravo", bundleB)

	serviceA := agent.NewService(storeA, agent.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	serviceB := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*agent.Service, error) {
		switch project {
		case "alpha":
			return serviceA, nil
		case "bravo":
			return serviceB, nil
		}
		return nil, nil
	})

	resultA, err := adapter.CallTool(ctx, "tasks.move", withModel(map[string]any{
		"project":    "alpha",
		"task_id":    taskA.ID,
		"bucket_key": "dev",
	}))
	if err != nil {
		t.Fatalf("CallTool alpha: %v", err)
	}
	if !resultA.IsError {
		t.Fatalf("alpha should hit comments_min guard, got: %s", resultA.Content[0].Text)
	}
	var failureA map[string]any
	if err := json.Unmarshal([]byte(resultA.Content[0].Text), &failureA); err != nil {
		t.Fatalf("alpha payload not JSON: %v", err)
	}
	if failureA["code"] != "guard_violation" {
		t.Fatalf("alpha failure code = %v, want guard_violation; payload=%v", failureA["code"], failureA)
	}

	resultB, err := adapter.CallTool(ctx, "tasks.move", withModel(map[string]any{
		"project":    "bravo",
		"task_id":    taskB.ID,
		"bucket_key": "dev",
	}))
	if err != nil {
		t.Fatalf("CallTool bravo: %v", err)
	}
	if resultB.IsError {
		t.Fatalf("bravo unguarded move should succeed, got error: %s", resultB.Content[0].Text)
	}
}

// TestAdapterServiceResolverIsolatesSettings asserts that each per-project
// service applies its own ServiceSettings. Both services share the same
// underlying bundle; only RecentCommentLimit differs. A task with 3
// comments returns at most 1 comment when routed to service A and all 3
// when routed to service B.
func TestAdapterServiceResolverIsolatesSettings(t *testing.T) {
	ctx := context.Background()

	bundle := mcpTestBundle(t)
	storeA, projectA, taskA := newMCPProjectWithBundle(t, ctx, "alpha", bundle)
	storeB, projectB, taskB := newMCPProjectWithBundle(t, ctx, "bravo", bundle)

	for i := 0; i < 3; i++ {
		if _, err := storeA.AddComment(ctx, projectA.ID, taskA.ID, fmt.Sprintf("c-a-%d", i), "agent", nil); err != nil {
			t.Fatalf("AddComment alpha #%d: %v", i, err)
		}
		if _, err := storeB.AddComment(ctx, projectB.ID, taskB.ID, fmt.Sprintf("c-b-%d", i), "agent", nil); err != nil {
			t.Fatalf("AddComment bravo #%d: %v", i, err)
		}
	}

	serviceA := agent.NewService(storeA, agent.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	includeFalse := false
	serviceA.SetSettings(agent.ServiceSettings{RecentCommentLimit: 1, IncludeWorkflow: false, CachePrompts: false})
	_ = includeFalse

	serviceB := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())
	serviceB.SetSettings(agent.ServiceSettings{RecentCommentLimit: 10, IncludeWorkflow: false, CachePrompts: false})

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*agent.Service, error) {
		switch project {
		case "alpha":
			return serviceA, nil
		case "bravo":
			return serviceB, nil
		}
		return nil, nil
	})

	got := callContinueAndDecodeCommentCount(t, ctx, adapter, "alpha", taskA.ID)
	if got != 1 {
		t.Fatalf("alpha comments returned = %d, want 1 (RecentCommentLimit cap)", got)
	}
	got = callContinueAndDecodeCommentCount(t, ctx, adapter, "bravo", taskB.ID)
	if got != 3 {
		t.Fatalf("bravo comments returned = %d, want 3 (RecentCommentLimit=10 covers all)", got)
	}
}

func callContinueAndDecodeCommentCount(t *testing.T, ctx context.Context, adapter *Adapter, project string, taskID int64) int {
	t.Helper()
	result, err := adapter.CallTool(ctx, "tasks.continue", withModel(map[string]any{
		"project": project,
		"task_id": taskID,
	}))
	if err != nil {
		t.Fatalf("CallTool tasks.continue (%s): %v", project, err)
	}
	if result.IsError {
		t.Fatalf("tasks.continue (%s) error: %s", project, result.Content[0].Text)
	}
	var payload struct {
		Comments []any `json:"comments"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("tasks.continue (%s) payload: %v", project, err)
	}
	return len(payload.Comments)
}

// TestAdapterServiceResolverIsolatesTemplateCatalog asserts that
// templates.list dispatched per-project surfaces only that project's
// catalog. Each service is wired with a distinct SetTemplateCatalog
// closure; the response slugs must not bleed across the resolver
// boundary.
func TestAdapterServiceResolverIsolatesTemplateCatalog(t *testing.T) {
	ctx := context.Background()

	bundleA := mcpTestBundle(t)
	bundleA.Templates = []config.TaskTemplate{
		{Slug: "pr-alpha", Name: "PR-A", Default: "pr", ProjectSlug: "alpha"},
		{Slug: "task-alpha", Name: "Task-A", Default: "task", ProjectSlug: "alpha"},
	}
	bundleB := mcpTestBundle(t)
	bundleB.Templates = []config.TaskTemplate{
		{Slug: "pr-bravo", Name: "PR-B", Default: "pr", ProjectSlug: "bravo"},
	}

	storeA, projectA, _ := newMCPProjectWithBundle(t, ctx, "alpha", bundleA)
	storeB, projectB, _ := newMCPProjectWithBundle(t, ctx, "bravo", bundleB)

	serviceA := agent.NewService(storeA, agent.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	serviceB := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*agent.Service, error) {
		switch project {
		case "alpha":
			return serviceA, nil
		case "bravo":
			return serviceB, nil
		}
		return nil, nil
	})

	slugsA := callListTemplates(t, ctx, adapter, "alpha")
	if !equalUnordered(slugsA, []string{"pr-alpha", "task-alpha"}) {
		t.Fatalf("alpha templates = %v, want {pr-alpha, task-alpha}", slugsA)
	}
	slugsB := callListTemplates(t, ctx, adapter, "bravo")
	if !equalUnordered(slugsB, []string{"pr-bravo"}) {
		t.Fatalf("bravo templates = %v, want {pr-bravo}", slugsB)
	}
}

// equalUnordered checks slice set-equality. ListTemplates builds its
// project-scoped + global result from Go maps, so the response order is
// non-deterministic. Tests assert membership, not order.
func equalUnordered(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, s := range got {
		seen[s]++
	}
	for _, s := range want {
		seen[s]--
	}
	for _, count := range seen {
		if count != 0 {
			return false
		}
	}
	return true
}

func callListTemplates(t *testing.T, ctx context.Context, adapter *Adapter, project string) []string {
	t.Helper()
	result, err := adapter.CallTool(ctx, "templates.list", withModel(map[string]any{"project": project}))
	if err != nil {
		t.Fatalf("CallTool templates.list (%s): %v", project, err)
	}
	if result.IsError {
		t.Fatalf("templates.list (%s) error: %s", project, result.Content[0].Text)
	}
	var payload struct {
		Templates []struct {
			Slug string `json:"slug"`
		} `json:"templates"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("templates.list (%s) payload: %v", project, err)
	}
	out := make([]string, 0, len(payload.Templates))
	for _, s := range payload.Templates {
		out = append(out, s.Slug)
	}
	return out
}

// TestAdapterServiceResolverConcurrentRouting drives N goroutines through
// CallTool with project= alternating between alpha and bravo. The
// project.overview payload echoes the project slug, so each goroutine
// can confirm the routing reached the correct service. -race surfaces
// any cross-call data corruption that a torn dispatch would introduce.
func TestAdapterServiceResolverConcurrentRouting(t *testing.T) {
	ctx := context.Background()

	storeA, projectA, _ := newMCPProjectWithBundle(t, ctx, "alpha", mcpTestBundle(t))
	storeB, projectB, _ := newMCPProjectWithBundle(t, ctx, "bravo", mcpTestBundle(t))

	serviceA := agent.NewService(storeA, agent.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	serviceB := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*agent.Service, error) {
		switch project {
		case "alpha":
			return serviceA, nil
		case "bravo":
			return serviceB, nil
		}
		return nil, nil
	})

	const workers = 16
	const iters = 25
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			project := "alpha"
			if id%2 == 1 {
				project = "bravo"
			}
			for j := 0; j < iters; j++ {
				result, err := adapter.CallTool(ctx, "project.overview", withModel(map[string]any{"project": project}))
				if err != nil {
					errs <- fmt.Errorf("worker %d iter %d call: %w", id, j, err)
					return
				}
				if result.IsError {
					errs <- fmt.Errorf("worker %d iter %d error: %s", id, j, result.Content[0].Text)
					return
				}
				var payload struct {
					Project struct {
						Slug string `json:"slug"`
					} `json:"project"`
				}
				if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
					errs <- fmt.Errorf("worker %d iter %d decode: %w", id, j, err)
					return
				}
				if payload.Project.Slug != project {
					errs <- fmt.Errorf("worker %d iter %d crosstalk: got %q want %q", id, j, payload.Project.Slug, project)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent routing failure: %v", err)
		}
	}
}

// TestAdapterDefaultServiceProviderTracksFreshService asserts the
// fix for the stale-pointer bug: when SetDefaultServiceProvider wires
// a func, CallTool / ReadResource / GetPrompt must consult it on every
// call so a runtime that rotates the default service (BundleCache
// rebuild) does not leave the adapter dispatching against a discarded
// pointer.
func TestAdapterDefaultServiceProviderTracksFreshService(t *testing.T) {
	ctx := context.Background()

	storeA, projectA, _ := newMCPProjectWithBundle(t, ctx, "alpha", mcpTestBundle(t))
	storeB, projectB, _ := newMCPProjectWithBundle(t, ctx, "bravo", mcpTestBundle(t))

	svcA := agent.NewService(storeA, agent.ProjectSelector{ProjectID: projectA.ID, CWD: filepath.Join(t.TempDir(), "a")})
	svcA.SetSnapshot(storeA.Snapshot())
	svcB := agent.NewService(storeB, agent.ProjectSelector{ProjectID: projectB.ID, CWD: filepath.Join(t.TempDir(), "b")})
	svcB.SetSnapshot(storeB.Snapshot())

	active := svcA
	adapter := NewAdapter(svcA)
	adapter.SetDefaultServiceProvider(func() *agent.Service { return active })

	resA, err := adapter.CallTool(ctx, "project.overview", withModel(nil))
	if err != nil || resA.IsError {
		t.Fatalf("CallTool A: err=%v isErr=%v body=%s", err, resA.IsError, snippet(resA))
	}
	var bodyA struct {
		Project struct {
			Slug string `json:"slug"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(resA.Content[0].Text), &bodyA); err != nil {
		t.Fatalf("decode A: %v", err)
	}
	if bodyA.Project.Slug != "alpha" {
		t.Fatalf("default service A overview slug = %q, want alpha", bodyA.Project.Slug)
	}

	// Simulate a BundleCache rebuild rotating the default service to
	// project bravo. The adapter holds a pre-rotation pointer in
	// a.service; the provider func is the only way it sees the
	// rotation.
	active = svcB

	resB, err := adapter.CallTool(ctx, "project.overview", withModel(nil))
	if err != nil || resB.IsError {
		t.Fatalf("CallTool B: err=%v isErr=%v body=%s", err, resB.IsError, snippet(resB))
	}
	var bodyB struct {
		Project struct {
			Slug string `json:"slug"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(resB.Content[0].Text), &bodyB); err != nil {
		t.Fatalf("decode B: %v", err)
	}
	if bodyB.Project.Slug != "bravo" {
		t.Fatalf("rotated default service overview slug = %q, want bravo (stale a.service was used)", bodyB.Project.Slug)
	}
}

func snippet(r ToolResult) string {
	if len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// newMCPProjectWithBundle imports the supplied bundle into a fresh
// store, registers a project under slug, and seeds a single backlog
// task so dispatch tests have something to operate on. Returns the
// triple every per-project test needs.
func newMCPProjectWithBundle(t *testing.T, ctx context.Context, slug string, bundle config.Bundle) (*snapstore.Store, domain.Project, domain.Task) {
	t.Helper()
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, bundle, "test.yaml", "hash"); err != nil {
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
	task, err := store.CreateTask(ctx, project.ID, "T-"+slug, "", domain.Priority(2), "backlog", store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(%s): %v", slug, err)
	}
	return store, project, task
}

func mcpTestBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go"}}
	bundle.Personas = []config.Persona{{Slug: "agent", Name: "Agent", Skills: []string{"go"}}}
	bundle.Laws = []config.Law{{Slug: "scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}}
	return bundle
}
