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

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testfixtures"
	"omakiten/internal/testfixtures/snapstore"
)

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
		assertAgentSchemaFields(t, tool)
		assertAgentRequiredFields(t, tool)
	}
}

func assertAgentSchemaFields(t *testing.T, tool ToolDefinition) {
	t.Helper()
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: InputSchema.properties missing or wrong type: %#v", tool.Name, tool.InputSchema["properties"])
	}
	model, ok := props["_agent_model"].(map[string]any)
	if !ok || model["type"] != "string" {
		t.Fatalf("%s: _agent_model schema invalid: %#v", tool.Name, props["_agent_model"])
	}
	desc, _ := model["description"].(string)
	if !strings.Contains(desc, "claude-opus-4-7") || !strings.Contains(desc, "Required") {
		t.Fatalf("%s: _agent_model.description missing exemplars or required hint: %q", tool.Name, desc)
	}
	session, ok := props["_agent_session_id"].(map[string]any)
	if !ok || session["type"] != "string" {
		t.Fatalf("%s: _agent_session_id schema invalid: %#v", tool.Name, props["_agent_session_id"])
	}
}

func assertAgentRequiredFields(t *testing.T, tool ToolDefinition) {
	t.Helper()
	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		t.Fatalf("%s: InputSchema.required missing or wrong type: %#v", tool.Name, tool.InputSchema["required"])
	}
	modelRequired := false
	for _, name := range required {
		if name == "_agent_model" {
			modelRequired = true
		}
		if name == "_agent_session_id" {
			t.Fatalf("%s: required must NOT include _agent_session_id: %v", tool.Name, required)
		}
	}
	if !modelRequired {
		t.Fatalf("%s: required missing _agent_model: %v", tool.Name, required)
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

	assertReadOnlyTools(t, ctx, adapter, []string{"skills.create", "skills.edit", "skills.delete"}, "skills")
	assertSkillsList(t, ctx, adapter)
	assertSkillGet(t, ctx, adapter)
	assertUnknownSkill(t, ctx, adapter)
}

func assertSkillsList(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "skills.list", withModel(nil))
	if err != nil {
		t.Fatalf("CallTool(skills.list) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("skills.list error: %s", result.Content[0].Text)
	}
	var payload struct {
		Skills []struct {
			Slug        string `json:"slug"`
			Description string `json:"description"`
			Body        string `json:"body"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("skills.list payload not JSON: %v", err)
	}
	if len(payload.Skills) == 0 {
		t.Fatal("skills.list returned no skills")
	}
	found := false
	for _, skill := range payload.Skills {
		if skill.Body != "" {
			t.Fatalf("skills.list leaked a body for %q", skill.Slug)
		}
		if skill.Slug == "go" {
			found = true
			if skill.Description == "" {
				t.Fatalf("skills.list dropped the description for %q", skill.Slug)
			}
		}
	}
	if !found {
		t.Fatal("skills.list missing known skill \"go\"")
	}
}

func assertSkillGet(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "skills.get", withModel(map[string]any{"slug": "go"}))
	if err != nil {
		t.Fatalf("CallTool(skills.get) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("skills.get error: %s", result.Content[0].Text)
	}
	var payload struct {
		Skill struct {
			Slug string `json:"slug"`
			Body string `json:"body"`
		} `json:"skill"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("skills.get payload not JSON: %v", err)
	}
	if payload.Skill.Slug != "go" || payload.Skill.Body == "" {
		t.Fatalf("skills.get returned %#v, want slug=go with a non-empty body", payload.Skill)
	}
}

func assertUnknownSkill(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "skills.get", withModel(map[string]any{"slug": "does-not-exist"}))
	if err != nil {
		t.Fatalf("CallTool(skills.get unknown) transport error = %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].Text, "does-not-exist") {
		t.Fatalf("skills.get unknown-slug response = %s, want named tool error", result.Content[0].Text)
	}
}

func assertReadOnlyTools(t *testing.T, ctx context.Context, adapter *Adapter, names []string, surface string) {
	t.Helper()
	for _, name := range names {
		if _, err := adapter.CallTool(ctx, name, withModel(nil)); err == nil {
			t.Fatalf("%s unexpectedly dispatched — %s must be read-only", name, surface)
		}
	}
}

// TestAdapterInsightsSummaryFrozenContract pins the consultivo insights.summary
// surface: it dispatches read-only and returns the frozen, versioned output
// contract. The response MUST carry schema_version (currently 2 — bumped by
// task 1353's per-model partial-state gate), an explicit has_data flag on every
// sub-report, and per-model rows that expose sample_size plus the partial-state
// fields (partial / first_stamped_at). There is NO insights.* mutation tool —
// the surface only reads.
func TestAdapterInsightsSummaryFrozenContract(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// No write path exists on the insights surface.
	for _, name := range []string{"insights.record", "insights.refresh", "insights.move"} {
		if _, err := adapter.CallTool(ctx, name, withModel(nil)); err == nil {
			t.Fatalf("%s unexpectedly dispatched — insights must be read-only", name)
		}
	}

	res, err := adapter.CallTool(ctx, "insights.summary", withModel(map[string]any{"stuck_days": 3}))
	if err != nil {
		t.Fatalf("CallTool(insights.summary) error = %v", err)
	}
	if res.IsError {
		t.Fatalf("insights.summary error: %s", res.Content[0].Text)
	}
	assertInsightsSummaryPayload(t, res)
}

func assertInsightsSummaryPayload(t *testing.T, res ToolResult) {
	t.Helper()
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		Insights      struct {
			StuckDays int `json:"stuck_days"`
			Stuck     struct {
				HasData bool `json:"has_data"`
			} `json:"stuck"`
			CycleTime struct {
				HasData bool `json:"has_data"`
			} `json:"cycle_time"`
			WIP struct {
				HasData bool `json:"has_data"`
			} `json:"wip"`
			Guards struct {
				HasData bool `json:"has_data"`
			} `json:"guards"`
			ErrorLoop struct {
				HasData bool `json:"has_data"`
			} `json:"error_loop"`
			PerModel struct {
				HasData bool `json:"has_data"`
				Models  []struct {
					AgentModel     string  `json:"agent_model"`
					SampleSize     *int    `json:"sample_size"`
					Partial        *bool   `json:"partial"`
					FirstStampedAt *string `json:"first_stamped_at"`
				} `json:"models"`
			} `json:"per_model"`
		} `json:"insights"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &payload); err != nil {
		t.Fatalf("insights.summary payload not JSON: %v / %s", err, res.Content[0].Text)
	}
	if payload.SchemaVersion != 2 {
		t.Fatalf("insights.summary schema_version = %d, want 2 (frozen contract, v2 per-model gate)", payload.SchemaVersion)
	}
	if payload.Insights.StuckDays != 3 {
		t.Fatalf("insights.summary echoed stuck_days = %d, want 3", payload.Insights.StuckDays)
	}
	// Per-model rows must carry sample_size and the partial-state fields — their
	// presence (not value) is the frozen-contract guarantee, so they must decode
	// even when zero/false.
	for _, m := range payload.Insights.PerModel.Models {
		if m.SampleSize == nil {
			t.Fatalf("insights.summary per-model row %q is missing sample_size", m.AgentModel)
		}
		if m.Partial == nil {
			t.Fatalf("insights.summary per-model row %q is missing partial", m.AgentModel)
		}
		if m.FirstStampedAt == nil {
			t.Fatalf("insights.summary per-model row %q is missing first_stamped_at", m.AgentModel)
		}
	}
}

// TestAdapterInsightsSummaryScopesToResolvedProject pins the contextual-first
// scope contract of insights.summary: with project_id omitted, the reading
// scopes to the project the service's selector resolves (cwd) — never the
// cross-project global view — and an explicit project_id still overrides the
// resolved context. Regression for the leak where InsightsSummary resolved
// the project but scoped by the raw input.ProjectID (default 0), so every
// self-consult from inside a project root returned the global rollup.
func TestAdapterInsightsSummaryScopesToResolvedProject(t *testing.T) {
	ctx := context.Background()
	store := snapstore.Open(t, filepath.Join(t.TempDir(), "omakiten.db"))
	if err := store.ImportBundle(ctx, mcpTestBundle(t), "test.yaml", "hash"); err != nil {
		t.Fatalf("ImportBundle() error = %v", err)
	}
	projectA := createInsightsProject(t, ctx, store, "alpha", 1)
	projectB := createInsightsProject(t, ctx, store, "bravo", 2)

	svc := operation.NewService(store, contract.ProjectSelector{CWD: projectA.RootPath})
	svc.SetSnapshot(store.Snapshot())
	adapter := NewAdapter(svc)

	if got := insightsWIPTotal(t, ctx, adapter, nil); got != 1 {
		t.Fatalf("insights.summary contextual WIP total = %d, want 1 (alpha only — global rollup leaked)", got)
	}
	if got := insightsWIPTotal(t, ctx, adapter, map[string]any{"project_id": projectB.ID}); got != 2 {
		t.Fatalf("insights.summary explicit project_id WIP total = %d, want 2 (bravo)", got)
	}
}

func createInsightsProject(t *testing.T, ctx context.Context, store *snapstore.Store, slug string, taskCount int) domain.Project {
	t.Helper()
	root := filepath.Join(t.TempDir(), slug)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", slug, err)
	}
	project, err := store.UpsertProject(ctx, slug, slug, root)
	if err != nil {
		t.Fatalf("UpsertProject(%s) error = %v", slug, err)
	}
	for i := 0; i < taskCount; i++ {
		if _, err := store.CreateTask(ctx, project.ID, fmt.Sprintf("%s-%d", slug, i), "", domain.Priority(2), "backlog", nil, store.Snapshot()); err != nil {
			t.Fatalf("CreateTask(%s-%d) error = %v", slug, i, err)
		}
	}
	return project
}

func insightsWIPTotal(t *testing.T, ctx context.Context, adapter *Adapter, args map[string]any) int {
	t.Helper()
	result, err := adapter.CallTool(ctx, "insights.summary", withModel(args))
	if err != nil {
		t.Fatalf("CallTool(insights.summary) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("insights.summary error: %s", result.Content[0].Text)
	}
	var payload struct {
		Insights struct {
			WIP struct {
				Buckets []struct {
					Count int `json:"count"`
				} `json:"buckets"`
			} `json:"wip"`
		} `json:"insights"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("insights.summary payload not JSON: %v / %s", err, result.Content[0].Text)
	}
	total := 0
	for _, bucket := range payload.Insights.WIP.Buckets {
		total += bucket.Count
	}
	return total
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

// TestAdapterRejectsMalformedAgentModel pins the _agent_model hygiene gate:
// the value is stamped into events verbatim and later rendered in a terminal
// and grouped in per-model rosters, so an oversized or control-character
// value is rejected at the choke point instead of stored.
func TestAdapterRejectsMalformedAgentModel(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	// Over the 128-byte cap.
	_, err := adapter.CallTool(ctx, "project.overview", map[string]any{"_agent_model": strings.Repeat("x", 129)})
	if err == nil {
		t.Fatal("CallTool(oversized _agent_model) error = nil, want validation_error")
	}
	if !strings.Contains(err.Error(), "exceeds 128 bytes") {
		t.Fatalf("CallTool error = %v, want length-cap validation", err)
	}

	// Terminal escape / control bytes — C0, DEL, and the C1 block
	// (U+009B = 8-bit CSI, U+009D = 8-bit OSC) must all be rejected.
	for _, bad := range []string{"claude\x1b]0;pwn\x07", "model\nnewline", "model\x7f", "model\u009b31m", "model\u009d0;pwn"} {
		_, err := adapter.CallTool(ctx, "project.overview", map[string]any{"_agent_model": bad})
		if err == nil {
			t.Fatalf("CallTool(_agent_model=%q) error = nil, want validation_error", bad)
		}
		if !strings.Contains(err.Error(), "control characters") {
			t.Fatalf("CallTool(_agent_model=%q) error = %v, want control-character validation", bad, err)
		}
	}

	// Boundary: exactly 128 printable bytes passes the gate.
	if _, err := adapter.CallTool(ctx, "project.overview", map[string]any{"_agent_model": strings.Repeat("x", 128)}); err != nil {
		t.Fatalf("CallTool(128-byte _agent_model) error = %v, want nil", err)
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

	service.SetSettings(operation.ServiceSettings{
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

	service.SetSettings(operation.ServiceSettings{
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
		assertParentIDSchema(t, tool)
	}
}

func assertParentIDSchema(t *testing.T, tool ToolDefinition) {
	t.Helper()
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
	if !ok || len(typ) != 2 || typ[0] != "integer" || typ[1] != "null" {
		t.Fatalf("%s: parent_id.type = %#v, want [integer null]", tool.Name, schema["type"])
	}
	if desc, _ := schema["description"].(string); desc == "" {
		t.Fatalf("%s: parent_id missing description", tool.Name)
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

	childID := createChildTask(t, ctx, adapter)
	if got := taskListCount(t, ctx, adapter, nil); got != 2 {
		t.Fatalf("tasks.list (no filter) returned %d, want 2", got)
	}
	if got := taskListCount(t, ctx, adapter, map[string]any{"parent_id": nil}); got != 1 {
		t.Fatalf("tasks.list (roots) returned %d, want 1", got)
	}
	if got := taskListCount(t, ctx, adapter, map[string]any{"parent_id": float64(1)}); got != 1 {
		t.Fatalf("tasks.list (children) returned %d, want 1", got)
	}
	clearTaskParent(t, ctx, adapter, childID)
}

func createChildTask(t *testing.T, ctx context.Context, adapter *Adapter) int64 {
	t.Helper()
	result, err := adapter.CallTool(ctx, "tasks.create", withModel(map[string]any{
		"description": "child of one", "parent_id": float64(1),
	}))
	if err != nil || result.IsError {
		t.Fatalf("tasks.create with parent_id failed: %v / %s", err, snippet(result))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("tasks.create payload not JSON: %v", err)
	}
	task, _ := payload["task"].(map[string]any)
	childID, _ := task["id"].(float64)
	if task == nil || childID == 0 {
		t.Fatalf("tasks.create payload missing task.id: %v", payload)
	}
	parentID, _ := task["parent_id"].(float64)
	if int64(parentID) != 1 {
		t.Fatalf("created task parent_id = %v, want 1", task["parent_id"])
	}
	return int64(childID)
}

func taskListCount(t *testing.T, ctx context.Context, adapter *Adapter, args map[string]any) int {
	t.Helper()
	result, err := adapter.CallTool(ctx, "tasks.list", withModel(args))
	if err != nil || result.IsError {
		t.Fatalf("tasks.list failed: %v / %s", err, snippet(result))
	}
	var payload struct {
		Tasks []any `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("tasks.list payload not JSON: %v", err)
	}
	return len(payload.Tasks)
}

func clearTaskParent(t *testing.T, ctx context.Context, adapter *Adapter, taskID int64) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "tasks.edit", withModel(map[string]any{
		"task_id": taskID, "parent_id": nil,
	}))
	if err != nil || result.IsError {
		t.Fatalf("tasks.edit (clear) failed: %v / %s", err, snippet(result))
	}
	var payload struct {
		Task map[string]any `json:"task"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("tasks.edit payload not JSON: %v", err)
	}
	if _, present := payload.Task["parent_id"]; present {
		t.Fatalf("tasks.edit (clear) left parent_id on payload: %v", payload.Task)
	}
}

func TestServeHandlesToolsList(t *testing.T) {
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	var output bytes.Buffer
	if err := ServeNotify(context.Background(), input, &output, NewAdapter(nil), nil); err != nil {
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

func newMCPTestService(t *testing.T, ctx context.Context) *operation.Service {
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
	svc := operation.NewService(store, contract.ProjectSelector{CWD: root})
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

	defaultService := operation.NewService(storeA, contract.ProjectSelector{ProjectID: projectA.ID})
	defaultService.SetSnapshot(storeA.Snapshot())
	projectBService := operation.NewService(storeB, contract.ProjectSelector{ProjectID: projectB.ID})
	projectBService.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(defaultService)
	var observed []string
	adapter.SetServiceResolver(func(_ context.Context, project string, projectID int64) (*operation.Service, error) {
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

	serviceA := operation.NewService(storeA, contract.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	serviceB := operation.NewService(storeB, contract.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*operation.Service, error) {
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

	serviceA := operation.NewService(storeA, contract.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	includeFalse := false
	serviceA.SetSettings(operation.ServiceSettings{RecentCommentLimit: 1, IncludeWorkflow: false, CachePrompts: false})
	_ = includeFalse

	serviceB := operation.NewService(storeB, contract.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())
	serviceB.SetSettings(operation.ServiceSettings{RecentCommentLimit: 10, IncludeWorkflow: false, CachePrompts: false})

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*operation.Service, error) {
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

	serviceA := operation.NewService(storeA, contract.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	serviceB := operation.NewService(storeB, contract.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*operation.Service, error) {
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
	seedResolverTasks(t, ctx, storeA, projectA, storeB, projectB)

	serviceA := operation.NewService(storeA, contract.ProjectSelector{ProjectID: projectA.ID})
	serviceA.SetSnapshot(storeA.Snapshot())
	serviceB := operation.NewService(storeB, contract.ProjectSelector{ProjectID: projectB.ID})
	serviceB.SetSnapshot(storeB.Snapshot())

	adapter := NewAdapter(serviceA)
	adapter.SetServiceResolver(func(_ context.Context, project string, _ int64) (*operation.Service, error) {
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
		go runResolverWorker(ctx, adapter, i, iters, &wg, errs)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent routing failure: %v", err)
		}
	}
}

func seedResolverTasks(t *testing.T, ctx context.Context, storeA *snapstore.Store, projectA domain.Project, storeB *snapstore.Store, projectB domain.Project) {
	t.Helper()
	if _, err := storeA.CreateTask(ctx, projectA.ID, "T-alpha-extra", "", domain.Priority(2), "backlog", nil, storeA.Snapshot()); err != nil {
		t.Fatalf("CreateTask(alpha extra): %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := storeB.CreateTask(ctx, projectB.ID, fmt.Sprintf("T-bravo-extra-%d", i), "", domain.Priority(2), "backlog", nil, storeB.Snapshot()); err != nil {
			t.Fatalf("CreateTask(bravo extra %d): %v", i, err)
		}
	}
}

func runResolverWorker(ctx context.Context, adapter *Adapter, id, iters int, wg *sync.WaitGroup, errs chan<- error) {
	defer wg.Done()
	project, wantBacklog := "alpha", 2
	if id%2 == 1 {
		project, wantBacklog = "bravo", 3
	}
	for j := 0; j < iters; j++ {
		result, err := adapter.CallTool(ctx, "project.overview", withModel(map[string]any{"project": project}))
		if err != nil {
			errs <- fmt.Errorf("worker %d iter %d call: %w", id, j, err)
			return
		}
		if err := validateResolverResult(result, project, wantBacklog); err != nil {
			errs <- fmt.Errorf("worker %d iter %d: %w", id, j, err)
			return
		}
	}
}

func validateResolverResult(result ToolResult, project string, wantBacklog int) error {
	if result.IsError {
		return fmt.Errorf("tool error: %s", snippet(result))
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
		return fmt.Errorf("decode: %w", err)
	}
	if payload.Project.Slug != project {
		return fmt.Errorf("crosstalk: got %q want %q", payload.Project.Slug, project)
	}
	for _, bucket := range payload.TaskBuckets {
		if bucket.BucketKey == "backlog" && bucket.Count == wantBacklog {
			return nil
		}
	}
	return fmt.Errorf("project %q backlog count does not equal %d", project, wantBacklog)
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

	svcA := operation.NewService(storeA, contract.ProjectSelector{ProjectID: projectA.ID, CWD: filepath.Join(t.TempDir(), "a")})
	svcA.SetSnapshot(storeA.Snapshot())
	svcB := operation.NewService(storeB, contract.ProjectSelector{ProjectID: projectB.ID, CWD: filepath.Join(t.TempDir(), "b")})
	svcB.SetSnapshot(storeB.Snapshot())

	active := svcA
	adapter := NewAdapter(svcA)
	adapter.SetDefaultServiceProvider(func() *operation.Service { return active })

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
		Slug:            "agent",
		Name:            "Agent",
		Description:     "Test agent persona.",
		Body:            "You are the test agent.",
		SkillRepertoire: []string{"go"},
		Laws:            []string{"scope"},
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

	assertReadOnlyTools(t, ctx, adapter, []string{"personas.create", "personas.edit", "laws.create"}, "catalog")
	assertPersonaList(t, ctx, adapter)

	assertPersonaGet(t, ctx, adapter)
	assertLawTools(t, ctx, adapter)
}

func assertPersonaList(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "personas.list", withModel(nil))
	if err != nil || result.IsError {
		t.Fatalf("personas.list failed: %v / %s", err, snippet(result))
	}
	var payload struct {
		Personas []struct {
			Slug string `json:"slug"`
			Body string `json:"body"`
		} `json:"personas"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("personas.list payload not JSON: %v", err)
	}
	if len(payload.Personas) == 0 {
		t.Fatal("personas.list returned no personas")
	}
	for _, persona := range payload.Personas {
		if persona.Body != "" {
			t.Fatalf("personas.list leaked body for %q", persona.Slug)
		}
	}
}

func assertPersonaGet(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "personas.get", withModel(map[string]any{"slug": "agent"}))
	if err != nil || result.IsError {
		t.Fatalf("personas.get failed: %v / %s", err, snippet(result))
	}
	var payload struct {
		Persona struct {
			Body string `json:"body"`
			Laws []struct {
				Body string `json:"body"`
			} `json:"laws"`
			SkillRepertoire []struct {
				Body string `json:"body"`
			} `json:"skill_repertoire"`
		} `json:"persona"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("personas.get payload not JSON: %v", err)
	}
	if payload.Persona.Body == "" || len(payload.Persona.Laws) == 0 || payload.Persona.Laws[0].Body == "" || len(payload.Persona.SkillRepertoire) == 0 || payload.Persona.SkillRepertoire[0].Body == "" {
		t.Fatalf("personas.get missing expanded payload: %#v", payload.Persona)
	}
}

func assertLawTools(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{name: "laws.list"},
		{name: "laws.get", args: map[string]any{"slug": "scope"}},
	} {
		result, err := adapter.CallTool(ctx, call.name, withModel(call.args))
		if err != nil || result.IsError {
			t.Fatalf("%s failed: %v / %s", call.name, err, snippet(result))
		}
	}
}

// TestAdapterCommentsScopeDispatch drives the reworked comments.* surface
// end-to-end through CallTool: add at task, project, and universal scope, then
// list the project-scoped handoff log filtered by kind.
func TestAdapterCommentsScopeDispatch(t *testing.T) {
	ctx := context.Background()
	service := newMCPTestService(t, ctx)
	adapter := NewAdapter(service)

	taskC := addScopedComment(t, ctx, adapter, map[string]any{"task_id": 1, "body": "task note", "author_type": "agent"})
	if taskC["scope"] != "task" {
		t.Fatalf("task comment scope = %v, want task", taskC["scope"])
	}

	projC := addScopedComment(t, ctx, adapter, map[string]any{"scope": "project", "body": "project recap", "author_type": "agent", "kind": "recap"})
	if projC["scope"] != "project" || projC["kind"] != "recap" {
		t.Fatalf("project comment = %#v, want scope=project kind=recap", projC)
	}

	uniC := addScopedComment(t, ctx, adapter, map[string]any{"scope": "universal", "body": "global note", "author_type": "agent"})
	if uniC["scope"] != "universal" {
		t.Fatalf("universal comment scope = %v, want universal", uniC["scope"])
	}

	assertInvalidScopedComment(t, ctx, adapter)
	assertScopedCommentList(t, ctx, adapter)
}

func addScopedComment(t *testing.T, ctx context.Context, adapter *Adapter, args map[string]any) map[string]any {
	t.Helper()
	result, err := adapter.CallTool(ctx, "comments.add", withModel(args))
	if err != nil || result.IsError {
		t.Fatalf("comments.add failed: %v / %s", err, snippet(result))
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

func assertInvalidScopedComment(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "comments.add", withModel(map[string]any{"scope": "project", "task_id": 1, "body": "x"}))
	if err != nil {
		t.Fatalf("CallTool(comments.add bad) error = %v", err)
	}
	if !result.IsError {
		t.Fatal("comments.add(project+task_id) should return validation failure")
	}
}

func assertScopedCommentList(t *testing.T, ctx context.Context, adapter *Adapter) {
	t.Helper()
	result, err := adapter.CallTool(ctx, "comments.list", withModel(map[string]any{"kind": "recap"}))
	if err != nil || result.IsError {
		t.Fatalf("comments.list failed: %v / %s", err, snippet(result))
	}
	var payload struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("comments.list content not JSON: %v", err)
	}
	if len(payload.Comments) != 1 || payload.Comments[0]["kind"] != "recap" {
		t.Fatalf("comments.list(kind=recap) = %#v, want one recap row", payload.Comments)
	}
}
