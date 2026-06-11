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
		"project.edit":        false,
		"tasks.list":          false,
		"tasks.create":        false,
		"tasks.move":          false,
		"comments.add":        false,
		"comments.list":       false,
		"dependencies.add":    false,
		"dependencies.remove": false,
		"dependencies.list":   false,
		"workflow.show":       false,
		"progress.record":     false,
		"errors.record":       false,
		"search":              false,
		"solutions.add":       false,
		"solutions.confirm":   false,
		"metrics.summary":     false,
		"logs.list":           false,
		"plans.edit":          false,
		"plans.delete":        false,
		"plans.remove_wave":   false,
		"plans.rename_wave":   false,
		"plans.reorder_wave":  false,
		"plans.unassign":      false,
		"skills.list":         false,
		"skills.get":          false,
		"personas.list":       false,
		"personas.get":        false,
		"laws.list":           false,
		"laws.get":            false,
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
		"project.edit",
		"tasks.continue",
		"tasks.list",
		"tasks.create_intent",
		"tasks.create",
		"tasks.move",
		"comments.add",
		"comments.list",
		"task_activity.list",
		"logs.list",
		"dependencies.add",
		"dependencies.remove",
		"dependencies.list",
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
		"plans.assign_task",
		"plans.claim_next",
		"plans.continue",
		"plans.edit",
		"plans.rename_wave",
		"plans.reorder_wave",
		"plans.unassign",
		"plans.remove_wave",
		"plans.delete",
		"skills.list",
		"skills.get",
		"personas.list",
		"personas.get",
		"laws.list",
		"laws.get",
	}

	for _, name := range tools {
		var args map[string]any
		switch name {
		case "skills.get":
			args = map[string]any{"slug": "go"}
		case "personas.get":
			args = map[string]any{"slug": "agent"}
		case "laws.get":
			args = map[string]any{"slug": "scope"}
		case "tasks.continue", "comments.add", "comments.list", "task_activity.list", "dependencies.add", "dependencies.remove", "dependencies.list":
			args = map[string]any{"task_id": 1}
		case "tasks.move":
			args = map[string]any{"task_id": 1, "bucket_key": "dev"}
		case "project.edit":
			args = map[string]any{"description": "edited via mcp"}
		case "tasks.create_intent", "tasks.create":
			args = map[string]any{"description": "test task"}
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
		case "plans.assign_task":
			args = map[string]any{"task_id": 1, "slug": "demo-plan-plans.create", "wave_id": 1}
		case "plans.claim_next":
			args = map[string]any{"slug": "demo-plan-plans.create"}
		case "plans.continue":
			args = map[string]any{"slug": "demo-plan-plans.create"}
		case "plans.edit":
			args = map[string]any{"slug": "demo-plan-plans.create", "name": "Renamed Demo"}
		case "plans.delete":
			args = map[string]any{"slug": "demo-plan-plans.create", "confirmed": true}
		case "plans.rename_wave":
			args = map[string]any{"wave_id": 1, "name": "renamed wave"}
		case "plans.reorder_wave":
			args = map[string]any{"wave_id": 1, "position": 5}
		case "plans.unassign":
			args = map[string]any{"task_id": 1}
		case "plans.remove_wave":
			args = map[string]any{"wave_id": 1, "confirmed": true}
		}
		_, err := adapter.CallTool(ctx, name, withModel(args))
		if err != nil {
			t.Fatalf("CallTool(%s) error = %v", name, err)
		}
	}
}

// TestAdapterProjectEditDispatchUpdatesDescription pins the project.edit
// dispatch path: calling the tool routes to Service.EditProject, which
// persists the new description and returns the refreshed project DTO. The
// payload echoes the edited description (and the unchanged project
// identity), proving the dispatch reached EditProject rather than a read
// tool.
func TestAdapterProjectEditDispatchUpdatesDescription(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	const want = "restored description via project.edit"
	result, err := adapter.CallTool(ctx, "project.edit", withModel(map[string]any{"description": want}))
	if err != nil {
		t.Fatalf("CallTool(project.edit) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(project.edit) returned error: %+v", result)
	}

	var payload struct {
		Project struct {
			Slug string `json:"slug"`
		} `json:"project"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("unmarshal project.edit payload: %v / %s", err, result.Content[0].Text)
	}
	if payload.Description != want {
		t.Fatalf("project.edit description = %q, want %q", payload.Description, want)
	}
	if payload.Project.Slug != "project" {
		t.Fatalf("project.edit returned project slug %q, want %q", payload.Project.Slug, "project")
	}
}

// TestAdapterSkillsToolsReadOnly pins the CW6 read-only skills surface:
// skills.list returns slugs + descriptions with NO body, skills.get returns
// one known skill's body, and skills.get on an unknown slug rejects cleanly
// with the missing slug surfaced. There is no skills.create / skills.edit /
// skills.delete tool — the catalog is user-authored and MCP never mutates it.
func TestAdapterSkillsToolsReadOnly(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// No write path exists.
	for _, name := range []string{"skills.create", "skills.edit", "skills.delete"} {
		if _, err := adapter.CallTool(ctx, name, withModel(nil)); err == nil {
			t.Fatalf("%s unexpectedly dispatched — skills must be read-only", name)
		}
	}

	// list: slugs + descriptions, no bodies.
	listRes, err := adapter.CallTool(ctx, "skills.list", withModel(nil))
	if err != nil {
		t.Fatalf("CallTool(skills.list) error = %v", err)
	}
	if listRes.IsError {
		t.Fatalf("skills.list error: %s", listRes.Content[0].Text)
	}
	var listPayload struct {
		Skills []struct {
			Slug        string `json:"slug"`
			Description string `json:"description"`
			Body        string `json:"body"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(listRes.Content[0].Text), &listPayload); err != nil {
		t.Fatalf("skills.list payload not JSON: %v", err)
	}
	if len(listPayload.Skills) == 0 {
		t.Fatalf("skills.list returned no skills")
	}
	found := false
	for _, sk := range listPayload.Skills {
		if sk.Body != "" {
			t.Fatalf("skills.list leaked a body for %q — list must omit bodies", sk.Slug)
		}
		if sk.Slug == "go" {
			found = true
			if sk.Description == "" {
				t.Fatalf("skills.list dropped the description for %q", sk.Slug)
			}
		}
	}
	if !found {
		t.Fatalf("skills.list missing known skill %q", "go")
	}

	// get: returns the body for a known slug.
	getRes, err := adapter.CallTool(ctx, "skills.get", withModel(map[string]any{"slug": "go"}))
	if err != nil {
		t.Fatalf("CallTool(skills.get) error = %v", err)
	}
	if getRes.IsError {
		t.Fatalf("skills.get error: %s", getRes.Content[0].Text)
	}
	var getPayload struct {
		Skill struct {
			Slug string `json:"slug"`
			Body string `json:"body"`
		} `json:"skill"`
	}
	if err := json.Unmarshal([]byte(getRes.Content[0].Text), &getPayload); err != nil {
		t.Fatalf("skills.get payload not JSON: %v", err)
	}
	if getPayload.Skill.Slug != "go" || getPayload.Skill.Body == "" {
		t.Fatalf("skills.get returned %#v, want slug=go with a non-empty body", getPayload.Skill)
	}

	// get: unknown slug rejects cleanly, naming the missing slug.
	missRes, err := adapter.CallTool(ctx, "skills.get", withModel(map[string]any{"slug": "does-not-exist"}))
	if err != nil {
		t.Fatalf("CallTool(skills.get unknown) transport error = %v", err)
	}
	if !missRes.IsError {
		t.Fatalf("skills.get on unknown slug should be a tool error, got: %s", missRes.Content[0].Text)
	}
	if !strings.Contains(missRes.Content[0].Text, "does-not-exist") {
		t.Fatalf("skills.get unknown-slug error should name the slug, got: %s", missRes.Content[0].Text)
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
	prompts := []string{"okt", "okt-task-create", "okt-task-continue", "okt-project-resume", "okt-task-imagine", "okt-task-implement"}
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

func TestAdapterGetPromptRendersInvocationArguments(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	result, err := adapter.GetPrompt(ctx, "okt-task-continue", map[string]any{"task_id": 42})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	body := result.Messages[0].Content.Text
	if !strings.Contains(body, "## Invocation Args\n") {
		t.Fatalf("prompt missing invocation args section:\n%s", body)
	}
	if !strings.Contains(body, "- `task_id`: 42") {
		t.Fatalf("prompt missing task_id argument:\n%s", body)
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
	nullable := nullableIntegerSchema("desc")
	gotType, ok := nullable["type"].([]string)
	if !ok {
		t.Fatalf("nullableIntegerSchema() type = %#v, want []string", nullable["type"])
	}
	if len(gotType) != 2 || gotType[0] != "integer" || gotType[1] != "null" {
		t.Fatalf("nullableIntegerSchema() type = %v, want [integer null]", gotType)
	}
}

// TestToolsSchemaExposesParentID pins the contract that the MCP-facing
// schemas for tasks.create / tasks.create_intent / tasks.edit / tasks.list
// declare `parent_id` with `type: [integer, null]`. Without the declaration,
// schema-aware MCP clients strip the field before dispatch, leaving the
// agent layer's tri-state encoding unreachable and gap #8274 open.
func TestToolsSchemaExposesParentID(t *testing.T) {
	wantTools := map[string]bool{
		"tasks.create":        true,
		"tasks.create_intent": true,
		"tasks.edit":          true,
		"tasks.list":          true,
	}
	for _, tool := range Tools() {
		if !wantTools[tool.Name] {
			continue
		}
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: InputSchema.properties missing or wrong type: %#v", tool.Name, tool.InputSchema["properties"])
		}
		raw, ok := props["parent_id"]
		if !ok {
			t.Fatalf("%s: properties.parent_id missing", tool.Name)
		}
		schema, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("%s: parent_id schema wrong shape: %#v", tool.Name, raw)
		}
		typ, ok := schema["type"].([]string)
		if !ok {
			t.Fatalf("%s: parent_id.type = %#v, want []string{\"integer\",\"null\"}", tool.Name, schema["type"])
		}
		if len(typ) != 2 || typ[0] != "integer" || typ[1] != "null" {
			t.Fatalf("%s: parent_id.type = %v, want [integer null]", tool.Name, typ)
		}
		desc, _ := schema["description"].(string)
		if desc == "" {
			t.Fatalf("%s: parent_id missing description", tool.Name)
		}
	}
}

// TestAdapterTasksParentIDTriStateRoundTrip exercises the full MCP edge
// for the parent_id tri-state across tasks.create, tasks.list, and
// tasks.edit. Each call body uses the raw JSON-RPC arg shape so the schema
// → decode → service path matches what a real client sends.
func TestAdapterTasksParentIDTriStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// 1. Create a child of task #1 by passing parent_id as an integer.
	createResult, err := adapter.CallTool(ctx, "tasks.create", withModel(map[string]any{
		"description": "child of one",
		"parent_id":   float64(1),
	}))
	if err != nil {
		t.Fatalf("tasks.create with parent_id error = %v", err)
	}
	if createResult.IsError {
		t.Fatalf("tasks.create with parent_id failed: %s", createResult.Content[0].Text)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(createResult.Content[0].Text), &created); err != nil {
		t.Fatalf("tasks.create payload not JSON: %v", err)
	}
	task, _ := created["task"].(map[string]any)
	if task == nil {
		t.Fatalf("tasks.create payload missing task: %v", created)
	}
	childIDFloat, _ := task["id"].(float64)
	childID := int64(childIDFloat)
	if childID == 0 {
		t.Fatalf("tasks.create payload missing task.id: %v", task)
	}
	if pidFloat, ok := task["parent_id"].(float64); !ok || int64(pidFloat) != 1 {
		t.Fatalf("created task parent_id = %v, want 1", task["parent_id"])
	}

	// 2. tasks.list with parent_id absent returns every task (the new child
	// and the seeded root).
	listAll, err := adapter.CallTool(ctx, "tasks.list", withModel(map[string]any{}))
	if err != nil || listAll.IsError {
		t.Fatalf("tasks.list (no filter) failed: %v / %s", err, listAll.Content[0].Text)
	}
	var listAllPayload map[string]any
	_ = json.Unmarshal([]byte(listAll.Content[0].Text), &listAllPayload)
	allTasks, _ := listAllPayload["tasks"].([]any)
	if len(allTasks) != 2 {
		t.Fatalf("tasks.list (no filter) returned %d, want 2", len(allTasks))
	}

	// 3. tasks.list with parent_id=null returns roots only (the seeded
	// task #1; the child is filtered out).
	listRoots, err := adapter.CallTool(ctx, "tasks.list", withModel(map[string]any{"parent_id": nil}))
	if err != nil || listRoots.IsError {
		t.Fatalf("tasks.list (roots) failed: %v / %s", err, listRoots.Content[0].Text)
	}
	var listRootsPayload map[string]any
	_ = json.Unmarshal([]byte(listRoots.Content[0].Text), &listRootsPayload)
	rootTasks, _ := listRootsPayload["tasks"].([]any)
	if len(rootTasks) != 1 {
		t.Fatalf("tasks.list (roots) returned %d, want 1 (sub-task should be filtered out)", len(rootTasks))
	}

	// 4. tasks.list with parent_id=<id> returns direct children only.
	listChildren, err := adapter.CallTool(ctx, "tasks.list", withModel(map[string]any{"parent_id": float64(1)}))
	if err != nil || listChildren.IsError {
		t.Fatalf("tasks.list (children) failed: %v / %s", err, listChildren.Content[0].Text)
	}
	var listChildrenPayload map[string]any
	_ = json.Unmarshal([]byte(listChildren.Content[0].Text), &listChildrenPayload)
	childTasks, _ := listChildrenPayload["tasks"].([]any)
	if len(childTasks) != 1 {
		t.Fatalf("tasks.list (children) returned %d, want 1", len(childTasks))
	}

	// 5. tasks.edit with parent_id=null clears the parent (re-roots the
	// child). Bucket policy permits edit in the planning bucket — the
	// fixture parent lives in `backlog` so the inherited bucket is OK.
	editResult, err := adapter.CallTool(ctx, "tasks.edit", withModel(map[string]any{
		"task_id":   childID,
		"parent_id": nil,
	}))
	if err != nil || editResult.IsError {
		t.Fatalf("tasks.edit (clear) failed: %v / %s", err, editResult.Content[0].Text)
	}
	var editedPayload map[string]any
	_ = json.Unmarshal([]byte(editResult.Content[0].Text), &editedPayload)
	editedTask, _ := editedPayload["task"].(map[string]any)
	if _, present := editedTask["parent_id"]; present {
		t.Fatalf("tasks.edit (clear) left parent_id on payload: %v", editedTask)
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
	if _, err := store.CreateTask(ctx, project.ID, "Task", "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
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
//
// slug is fixture-arbitrary: pick any non-empty string the test
// asserts against. Each call provisions its own TempDir-backed store
// so multiple invocations in the same test do not collide.
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
	if _, err := store.CreateTask(ctx, project.ID, "T-"+slug, "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
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
// catalog. Each service is wired with a distinct *config.Snapshot via
// SetSnapshot; the response slugs must not bleed across the resolver
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
// project.overview payload echoes the project slug and surfaces
// per-bucket task counts, so each goroutine can confirm both the
// slug echo AND that the overview was computed against the
// resolver-selected store. -race surfaces any cross-call data
// corruption that a torn dispatch would introduce.
//
// Routing isolation: the helper seeds one backlog task per store, so
// we plant additional backlog tasks here to make the per-project
// counts diverge (alpha=2, bravo=3). A resolver that collapsed both
// requests onto a single store would return the same count for both
// projects, while the slug-echo assertion (which only proves the
// request slug round-trips through the response) would still pass.
func TestAdapterServiceResolverConcurrentRouting(t *testing.T) {
	ctx := context.Background()

	storeA, projectA, _ := newMCPProjectWithBundle(t, ctx, "alpha", mcpTestBundle(t))
	storeB, projectB, _ := newMCPProjectWithBundle(t, ctx, "bravo", mcpTestBundle(t))

	// Plant extra tasks so the backlog count differs per project. A
	// resolver collapse would surface as both projects reporting the
	// same count regardless of slug.
	if _, err := storeA.CreateTask(ctx, projectA.ID, "T-alpha-extra", "", domain.Priority(2), "backlog", nil, storeA.Snapshot()); err != nil {
		t.Fatalf("CreateTask(alpha extra): %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := storeB.CreateTask(ctx, projectB.ID, fmt.Sprintf("T-bravo-extra-%d", i), "", domain.Priority(2), "backlog", nil, storeB.Snapshot()); err != nil {
			t.Fatalf("CreateTask(bravo extra %d): %v", i, err)
		}
	}
	const alphaBacklogCount = 2 // seeded T-alpha + T-alpha-extra
	const bravoBacklogCount = 3 // seeded T-bravo + 2 extras

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
			wantBacklog := alphaBacklogCount
			if id%2 == 1 {
				project = "bravo"
				wantBacklog = bravoBacklogCount
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
					TaskBuckets []struct {
						BucketKey string `json:"bucket_key"`
						Count     int    `json:"count"`
					} `json:"task_buckets"`
				}
				if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
					errs <- fmt.Errorf("worker %d iter %d decode: %w", id, j, err)
					return
				}
				if payload.Project.Slug != project {
					errs <- fmt.Errorf("worker %d iter %d crosstalk: got %q want %q", id, j, payload.Project.Slug, project)
					return
				}
				// Routing isolation: the bucket count comes from the
				// resolver-selected store, NOT the request args, so a
				// silent resolver collapse fails this assertion.
				gotBacklog := -1
				for _, b := range payload.TaskBuckets {
					if b.BucketKey == "backlog" {
						gotBacklog = b.Count
						break
					}
				}
				if gotBacklog != wantBacklog {
					errs <- fmt.Errorf("worker %d iter %d project %q backlog count = %d, want %d (resolver may have collapsed to wrong store)", id, j, project, gotBacklog, wantBacklog)
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
//
// slug is fixture-arbitrary: callers commonly pass "alpha" / "bravo"
// for two-project routing tests, but any non-empty string works. The
// helper is safe to invoke multiple times in the same test with
// distinct slugs — each call provisions its own TempDir-backed store
// so the resulting (store, project, task) triples do not collide.
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
	task, err := store.CreateTask(ctx, project.ID, "T-"+slug, "", domain.Priority(2), "backlog", nil, store.Snapshot())
	if err != nil {
		t.Fatalf("CreateTask(%s): %v", slug, err)
	}
	return store, project, task
}

func mcpTestBundle(t *testing.T) config.Bundle {
	t.Helper()
	bundle, _ := testfixtures.LoadBundle(t, "default.yaml")
	bundle.Skills = []config.Skill{{Slug: "go", Name: "Go", Description: "Idiomatic Go.", Body: "Write idiomatic, well-tested Go."}}
	bundle.Personas = []config.Persona{{
		Slug:        "agent",
		Name:        "Agent",
		Description: "Test agent persona.",
		Body:        "You are the test agent.",
		Skills:      []string{"go"},
		Laws:        []string{"scope"},
	}}
	bundle.Laws = []config.Law{{Slug: "scope", Name: "Scope", Severity: "error", Body: "Stay scoped.", Scope: "global"}}
	return bundle
}

// TestAdapterPersonasAndLawsToolsReadOnly pins the read-only persona/law
// catalog surface: list endpoints omit bodies; get endpoints expand references
// on personas and return law bodies.
func TestAdapterPersonasAndLawsToolsReadOnly(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	for _, name := range []string{"personas.create", "personas.edit", "laws.create"} {
		if _, err := adapter.CallTool(ctx, name, withModel(nil)); err == nil {
			t.Fatalf("%s unexpectedly dispatched — catalog must be read-only", name)
		}
	}

	listRes, err := adapter.CallTool(ctx, "personas.list", withModel(nil))
	if err != nil {
		t.Fatalf("CallTool(personas.list) error = %v", err)
	}
	if listRes.IsError {
		t.Fatalf("personas.list error: %s", listRes.Content[0].Text)
	}
	var listPayload struct {
		Personas []struct {
			Slug string `json:"slug"`
			Body string `json:"body"`
		} `json:"personas"`
	}
	if err := json.Unmarshal([]byte(listRes.Content[0].Text), &listPayload); err != nil {
		t.Fatalf("personas.list payload not JSON: %v", err)
	}
	if len(listPayload.Personas) == 0 {
		t.Fatal("personas.list returned no personas")
	}
	for _, p := range listPayload.Personas {
		if p.Body != "" {
			t.Fatalf("personas.list leaked body for %q", p.Slug)
		}
	}

	getRes, err := adapter.CallTool(ctx, "personas.get", withModel(map[string]any{"slug": "agent"}))
	if err != nil {
		t.Fatalf("CallTool(personas.get) error = %v", err)
	}
	if getRes.IsError {
		t.Fatalf("personas.get error: %s", getRes.Content[0].Text)
	}
	var getPayload struct {
		Persona struct {
			Slug  string `json:"slug"`
			Body  string `json:"body"`
			Laws  []struct{ Body string `json:"body"` } `json:"laws"`
			Skills []struct{ Body string `json:"body"` } `json:"skills"`
		} `json:"persona"`
	}
	if err := json.Unmarshal([]byte(getRes.Content[0].Text), &getPayload); err != nil {
		t.Fatalf("personas.get payload not JSON: %v", err)
	}
	if getPayload.Persona.Body == "" || len(getPayload.Persona.Laws) == 0 || getPayload.Persona.Laws[0].Body == "" {
		t.Fatalf("personas.get missing expanded payload: %#v", getPayload.Persona)
	}
	if len(getPayload.Persona.Skills) == 0 || getPayload.Persona.Skills[0].Body == "" {
		t.Fatalf("personas.get missing expanded skills: %#v", getPayload.Persona.Skills)
	}

	lawList, err := adapter.CallTool(ctx, "laws.list", withModel(nil))
	if err != nil {
		t.Fatalf("CallTool(laws.list) error = %v", err)
	}
	if lawList.IsError {
		t.Fatalf("laws.list error: %s", lawList.Content[0].Text)
	}
	lawGet, err := adapter.CallTool(ctx, "laws.get", withModel(map[string]any{"slug": "scope"}))
	if err != nil {
		t.Fatalf("CallTool(laws.get) error = %v", err)
	}
	if lawGet.IsError {
		t.Fatalf("laws.get error: %s", lawGet.Content[0].Text)
	}
}

// TestAdapterCommentsScopeDispatch drives the reworked comments.* surface
// end-to-end through CallTool: add at task, project, and universal scope, then
// list the project-scoped handoff log filtered by kind.
func TestAdapterCommentsScopeDispatch(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	add := func(args map[string]any) map[string]any {
		t.Helper()
		result, err := adapter.CallTool(ctx, "comments.add", withModel(args))
		if err != nil {
			t.Fatalf("CallTool(comments.add) error = %v", err)
		}
		if result.IsError {
			t.Fatalf("comments.add IsError = true, content = %s", result.Content[0].Text)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
			t.Fatalf("comments.add content not JSON: %v", err)
		}
		comment, _ := payload["comment"].(map[string]any)
		if comment == nil {
			t.Fatalf("comments.add payload missing comment: %#v", payload)
		}
		return comment
	}

	taskC := add(map[string]any{"task_id": 1, "body": "task note", "author_type": "agent"})
	if taskC["scope"] != "task" {
		t.Fatalf("task comment scope = %v, want task", taskC["scope"])
	}

	projC := add(map[string]any{"scope": "project", "body": "project recap", "author_type": "agent", "kind": "recap"})
	if projC["scope"] != "project" || projC["kind"] != "recap" {
		t.Fatalf("project comment = %#v, want scope=project kind=recap", projC)
	}

	uniC := add(map[string]any{"scope": "universal", "body": "global note", "author_type": "agent"})
	if uniC["scope"] != "universal" {
		t.Fatalf("universal comment scope = %v, want universal", uniC["scope"])
	}

	// project scope must not carry task_id.
	bad, err := adapter.CallTool(ctx, "comments.add", withModel(map[string]any{"scope": "project", "task_id": 1, "body": "x"}))
	if err != nil {
		t.Fatalf("CallTool(comments.add bad) error = %v", err)
	}
	if !bad.IsError {
		t.Fatalf("comments.add(project+task_id) IsError = false, want validation failure")
	}

	// Filtered list: kind=recap returns exactly the project recap row.
	listResult, err := adapter.CallTool(ctx, "comments.list", withModel(map[string]any{"kind": "recap"}))
	if err != nil {
		t.Fatalf("CallTool(comments.list) error = %v", err)
	}
	if listResult.IsError {
		t.Fatalf("comments.list IsError = true, content = %s", listResult.Content[0].Text)
	}
	var listPayload struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal([]byte(listResult.Content[0].Text), &listPayload); err != nil {
		t.Fatalf("comments.list content not JSON: %v", err)
	}
	if len(listPayload.Comments) != 1 || listPayload.Comments[0]["kind"] != "recap" {
		t.Fatalf("comments.list(kind=recap) = %#v, want one recap row", listPayload.Comments)
	}
}
