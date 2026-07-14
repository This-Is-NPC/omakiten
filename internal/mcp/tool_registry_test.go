package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"omakiten/internal/agent"
)

var currentToolNames = []string{
	"project.overview",
	"project.resume",
	"project.edit",
	"tasks.continue",
	"tasks.list",
	"tasks.create_intent",
	"tasks.create",
	"tasks.move",
	"tasks.edit",
	"tasks.delete",
	"tasks.archive",
	"tasks.unarchive",
	"comments.add",
	"comments.list",
	"comments.edit",
	"comments.delete",
	"task_activity.list",
	"logs.list",
	"dependencies.add",
	"dependencies.remove",
	"dependencies.list",
	"workflow.show",
	"orphans.migrate",
	"progress.record",
	"tags.add",
	"tags.remove",
	"tags.list",
	"tags.list_all",
	"tags.merge",
	"errors.record",
	"search",
	"solutions.add",
	"solutions.confirm",
	"solutions.list_top",
	"templates.list",
	"metrics.summary",
	"insights.summary",
	"templates.show",
	"skills.list",
	"skills.get",
	"personas.list",
	"personas.get",
	"laws.list",
	"laws.get",
	"plans.create",
	"plans.list",
	"plans.show",
	"plans.add_wave",
	"plans.assign_task",
	"plans.claim_next",
	"plans.continue",
	"plans.edit",
	"plans.delete",
	"plans.remove_wave",
	"plans.rename_wave",
	"plans.reorder_wave",
	"plans.unassign",
	"commands.list",
	"commands.resolve",
}

func TestToolsCurrentSurface(t *testing.T) {
	definitions := Tools()
	if len(definitions) != 59 {
		t.Fatalf("Tools() count = %d, want 59", len(definitions))
	}

	names := make([]string, 0, len(definitions))
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.Name == "" || definition.Description == "" || definition.InputSchema == nil {
			t.Fatalf("incomplete tool definition: %#v", definition)
		}
		if _, exists := seen[definition.Name]; exists {
			t.Fatalf("duplicate tool definition %q", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		names = append(names, definition.Name)
	}

	if !reflect.DeepEqual(names, currentToolNames) {
		t.Fatalf("Tools() names/order =\n%q\nwant\n%q", names, currentToolNames)
	}
}

func TestTasksMoveBucketKeyDescriptionIsStable(t *testing.T) {
	for _, definition := range Tools() {
		if definition.Name != "tasks.move" {
			continue
		}
		properties, ok := definition.InputSchema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tasks.move properties = %#v", definition.InputSchema["properties"])
		}
		bucketKey, ok := properties["bucket_key"].(map[string]any)
		if !ok {
			t.Fatalf("tasks.move.bucket_key schema = %#v", properties["bucket_key"])
		}
		if got := bucketKey["description"]; got != "Target bucket key" {
			t.Fatalf("tasks.move.bucket_key description = %q, want %q", got, "Target bucket key")
		}
		return
	}
	t.Fatal("tasks.move tool missing")
}

func TestToolsReturnFreshSchemas(t *testing.T) {
	for index, definition := range Tools() {
		t.Run(definition.Name, func(t *testing.T) {
			first := Tools()[index].InputSchema
			second := Tools()[index].InputSchema
			firstBefore := mustMarshalSchema(t, first)
			secondBefore := mustMarshalSchema(t, second)
			if !bytes.Equal(firstBefore, secondBefore) {
				t.Fatal("schema factory returned unequal fresh values")
			}

			mutateSchemaTree(first)
			if bytes.Equal(firstBefore, mustMarshalSchema(t, first)) {
				t.Fatal("recursive probe did not mutate the first schema")
			}
			if !bytes.Equal(secondBefore, mustMarshalSchema(t, second)) {
				t.Fatal("recursive mutation leaked into the second schema")
			}
		})
	}
}

func mutateSchemaTree(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			typed[key] = mutateSchemaTree(child)
		}
		typed["__freshness_probe__"] = true
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = mutateSchemaTree(child)
		}
		if len(typed) == 0 {
			return append(typed, "__freshness_probe__")
		}
		typed[0] = "__freshness_probe__"
		return typed
	case []string:
		if len(typed) == 0 {
			return append(typed, "__freshness_probe__")
		}
		typed[0] = "__freshness_probe__"
		return typed
	default:
		return value
	}
}

func mustMarshalSchema(t *testing.T, schema map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	return encoded
}

func TestSearchToolAdvertisesFivePhysicalEntityTypes(t *testing.T) {
	var search ToolDefinition
	for _, definition := range Tools() {
		if definition.Name == "search" {
			search = definition
			break
		}
	}
	if search.Name == "" {
		t.Fatal("search tool not registered")
	}
	if !strings.Contains(search.Description, "tasks, comments, errors, solutions, and plans") {
		t.Fatalf("search description does not name five physical types: %q", search.Description)
	}
	if !strings.Contains(search.Description, "note-like content is returned as `comment`") {
		t.Fatalf("search description does not explain note-like comments: %q", search.Description)
	}

	properties := search.InputSchema["properties"].(map[string]any)
	entityTypes := properties["entity_types"].(map[string]any)
	want := "Optional restriction to a subset of entity types. Allowed: task, comment, error, solution, plan. Empty or omitted indexes all five."
	if got := entityTypes["description"]; got != want {
		t.Fatalf("search entity_types description = %q, want %q", got, want)
	}
}

func TestFTSQuerySchemasShareLimitsAndCommentIDDescribesProjectScope(t *testing.T) {
	var search, comments ToolDefinition
	for _, definition := range Tools() {
		switch definition.Name {
		case "search":
			search = definition
		case "comments.list":
			comments = definition
		}
	}
	searchQuery := search.InputSchema["properties"].(map[string]any)["query"].(map[string]any)
	commentProperties := comments.InputSchema["properties"].(map[string]any)
	commentQuery := commentProperties["query"].(map[string]any)
	if !reflect.DeepEqual(searchQuery, commentQuery) || searchQuery["maxLength"] != 4096 || !strings.Contains(searchQuery["description"].(string), "256 lexical") {
		t.Fatalf("FTS query schemas differ or omit shared caps: search=%v comments=%v", searchQuery, commentQuery)
	}
	commentID := commentProperties["comment_id"].(map[string]any)["description"].(string)
	if !strings.Contains(commentID, "resolved project") || !strings.Contains(commentID, "universal") {
		t.Fatalf("comments.list comment_id schema misstates project scope: %q", commentID)
	}
}

func TestEveryAdvertisedToolReachesDispatch(t *testing.T) {
	ctx := context.Background()
	adapter := NewAdapter(newMCPTestService(t, ctx))

	for _, definition := range Tools() {
		t.Run(definition.Name, func(t *testing.T) {
			if registeredTools.handlers[definition.Name] == nil {
				t.Fatal("advertised tool has no registered handler")
			}
			_, err := adapter.CallTool(ctx, definition.Name, withModel(nil))
			if err != nil && strings.Contains(err.Error(), "unknown MCP tool") {
				t.Fatalf("advertised tool has no dispatch handler: %v", err)
			}
		})
	}
}

func TestCustomToolHandlersPreserveInputBehavior(t *testing.T) {
	ctx := context.Background()
	adapter := NewAdapter(newMCPTestService(t, ctx))
	unencodable := make(chan int)

	resolved, err := adapter.CallTool(ctx, "commands.resolve", withModel(map[string]any{
		"name":      "okt-audit",
		"arguments": "ignored non-object",
		"extra":     unencodable,
	}))
	if err != nil || resolved.IsError {
		t.Fatalf("commands.resolve manual input handling: err=%v result=%#v", err, resolved)
	}
	wantPrompt, err := adapter.GetPrompt(ctx, "okt-audit", nil)
	if err != nil {
		t.Fatalf("GetPrompt(okt-audit): %v", err)
	}
	if resolved.Content[0].Text != wantPrompt.Messages[0].Content.Text {
		t.Fatal("commands.resolve did not preserve raw prompt markdown for malformed optional arguments")
	}

	listed, err := adapter.CallTool(ctx, "commands.list", withModel(map[string]any{"extra": unencodable}))
	if err != nil || listed.IsError {
		t.Fatalf("commands.list ignored input: err=%v result=%#v", err, listed)
	}
	var gotPrompts []PromptDefinition
	if err := json.Unmarshal([]byte(listed.Content[0].Text), &gotPrompts); err != nil {
		t.Fatalf("decode commands.list: %v", err)
	}
	if !reflect.DeepEqual(gotPrompts, adapter.Prompts()) {
		t.Fatal("commands.list result differs from Adapter.Prompts()")
	}

	allTags, err := adapter.CallTool(ctx, "tags.list_all", withModel(map[string]any{"extra": unencodable}))
	if err != nil || allTags.IsError {
		t.Fatalf("tags.list_all no-input handling: err=%v result=%#v", err, allTags)
	}

	malformed, err := adapter.CallTool(ctx, "project.overview", withModel(map[string]any{"extra": unencodable}))
	if err != nil {
		t.Fatalf("ordinary malformed input returned transport error: %v", err)
	}
	if !malformed.IsError {
		t.Fatal("ordinary malformed input did not return a structured tool error")
	}
}

func TestNewToolRegistryRejectsInvalidRegistrations(t *testing.T) {
	noopHandler := func(*Adapter, context.Context, *agent.Service, map[string]any) (ToolResult, error) {
		return ToolResult{}, nil
	}
	complete := toolRegistration{
		name:        "complete",
		description: "Complete registration.",
		schema:      func() map[string]any { return objectSchema(map[string]any{}, nil) },
		handler:     noopHandler,
	}

	tests := []struct {
		name          string
		registrations []toolRegistration
	}{
		{name: "empty name", registrations: []toolRegistration{{description: complete.description, schema: complete.schema, handler: complete.handler}}},
		{name: "empty description", registrations: []toolRegistration{{name: complete.name, schema: complete.schema, handler: complete.handler}}},
		{name: "nil schema factory", registrations: []toolRegistration{{name: complete.name, description: complete.description, handler: complete.handler}}},
		{name: "nil schema", registrations: []toolRegistration{{name: complete.name, description: complete.description, schema: func() map[string]any { return nil }, handler: complete.handler}}},
		{name: "nil handler", registrations: []toolRegistration{{name: complete.name, description: complete.description, schema: complete.schema}}},
		{name: "duplicate name", registrations: []toolRegistration{complete, complete}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newToolRegistry(tt.registrations); err == nil {
				t.Fatal("newToolRegistry() error = nil")
			}
		})
	}
}
