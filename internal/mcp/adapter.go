package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"omakiten/internal/activity"
	"omakiten/internal/agent"
	"omakiten/internal/domain"
)

// agentModelArgKey / agentSessionArgKey are reserved top-level fields on
// every MCP tool input. _agent_model is required (coercive validation
// rejects calls missing it); _agent_session_id is optional. They are
// stripped from args before tool-specific decoding so individual input
// structs don't have to care.
const (
	agentModelArgKey   = "_agent_model"
	agentSessionArgKey = "_agent_session_id"
)

// extractAgentAttribution pulls the reserved keys out of args, rejecting
// the call when _agent_model is absent or empty. The error message is
// self-describing so the AI agent can fix its own request without a
// follow-up; failing closed forces every benchmark sample to carry a
// model id, which is the whole point of /metrics.summary.
func extractAgentAttribution(args map[string]any) (model, sessionID string, err error) {
	rawModel, ok := args[agentModelArgKey]
	if !ok {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model is required on all MCP tool calls. "+
				"Identify the AI model invoking this tool (e.g., \"claude-opus-4-7\", \"claude-sonnet-4-6\", \"gpt-5\"). "+
				"Pass it as a top-level field in the tool input args.", nil)
	}
	model, _ = rawModel.(string)
	if model == "" {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model must be a non-empty string. "+
				"Identify the AI model invoking this tool (e.g., \"claude-opus-4-7\").", nil)
	}
	delete(args, agentModelArgKey)

	if raw, present := args[agentSessionArgKey]; present {
		sessionID, _ = raw.(string)
		delete(args, agentSessionArgKey)
	}
	return model, sessionID, nil
}

type Adapter struct {
	service *agent.Service
	repo    activity.ActivityLogRepository
}

func NewAdapter(service *agent.Service) *Adapter {
	return &Adapter{service: service}
}

func (a *Adapter) SetActivityLogRepository(repo activity.ActivityLogRepository) {
	a.repo = repo
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MIMEType    string `json:"mimeType"`
}

type PromptDefinition struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

type PromptMessage struct {
	Role    string      `json:"role"`
	Content ContentItem `json:"content"`
}

type PromptResult struct {
	Description string          `json:"description"`
	Messages    []PromptMessage `json:"messages"`
}

type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentItem struct {
	Type string         `json:"type"`
	Text string         `json:"text"`
	Meta map[string]any `json:"_meta,omitempty"`
}

func Tools() []ToolDefinition {
	return []ToolDefinition{
		{Name: "project.overview", Description: "Return active project identity, workflow awareness, pending count, recent context, and next-step prompt.", InputSchema: selectorSchema()},
		{Name: "project.resume", Description: "Return project distribution, likely next work, blocked work, dependencies, recent context, and workflow state.", InputSchema: selectorSchema()},
		{Name: "tasks.continue", Description: "Load a project-owned task with dependencies, comments, workflow bucket, and recent handoff context. Set include_workflow=false on subsequent calls in a session where the workflow shape was already loaded by /okt to save tokens.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id to continue"), "include_workflow": booleanSchema("Optional override for config.mcp.include_workflow_in_continue. Pass false to skip the workflow block when /okt already loaded it.")}, []string{"task_id"})},
		{Name: "tasks.list", Description: "List active project tasks, optionally filtered by workflow bucket.", InputSchema: objectSchema(map[string]any{"bucket_key": stringSchema("Optional workflow bucket key")}, nil)},
		{Name: "tasks.create_intent", Description: "Create a task intent after checking for similar or related project tasks and requiring confirmation when needed.", InputSchema: createTaskSchema()},
		{Name: "tasks.create", Description: "Create a task directly through Omakiten's shared task service.", InputSchema: createTaskSchema()},
		{Name: "tasks.move", Description: "Move a task through an allowed workflow transition.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "bucket_key": stringSchema("Target bucket key")}, []string{"task_id", "bucket_key"})},
		{Name: "comments.add", Description: "Add a human or agent comment to a project-owned task. Optionally tag the comment with one or more tag names (normalized to kebab-case) or pre-fill its body from a loaded template.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "body": stringSchema("Comment body"), "author_type": stringSchema("human or agent"), "tags": arrayStringSchema("Optional tag names to attach to this comment (e.g. [\"resume\", \"deployment-notes\"])"), "template_slug": stringSchema("Optional slug of a loaded template; when set, the template body is merged into the comment (user content first, template appended).")}, []string{"task_id", "body"})},
		{Name: "comments.list", Description: "List comments for a project-owned task.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})},
		{Name: "task_activity.list", Description: "Return the unified activity feed for a task: comments and system events (task.created, task.moved, task.completed) ordered chronologically.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "order": stringSchema("Sort order: 'asc' (chronological, default) or 'desc' (newest first)")}, []string{"task_id"})},
		{Name: "dependencies.add", Description: "Add a project-scoped task dependency with cycle prevention.", InputSchema: dependencySchema(false)},
		{Name: "dependencies.remove", Description: "Remove a task dependency after explicit confirmation.", InputSchema: dependencySchema(true)},
		{Name: "dependencies.list", Description: "List dependencies for one task or all active project tasks.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Optional task id; omit or set 0 for all")}, nil)},
		{Name: "context.add", Description: "Add a project handoff context entry.", InputSchema: objectSchema(map[string]any{"body": stringSchema("Context body")}, []string{"body"})},
		{Name: "context.dump", Description: "Dump compact project context at level 1, 2, or 3.", InputSchema: objectSchema(map[string]any{"level": integerSchema("Context level: 1, 2, or 3")}, nil)},
		{Name: "workflow.show", Description: "Show the active workflow buckets and allowed transitions.", InputSchema: selectorSchema()},
		{Name: "progress.record", Description: "Record material agent progress through task edits, comments, context entries, and optional workflow movement.", InputSchema: progressSchema()},
		{Name: "tags.add", Description: "Add a reusable tag to a task or project. The tag name is normalized to kebab-case and deduplicated automatically.", InputSchema: tagMutationSchema(false)},
		{Name: "tags.remove", Description: "Remove a tag from a task or project after explicit confirmation.", InputSchema: tagMutationSchema(true)},
		{Name: "tags.list", Description: "List tags for a specific task or project.", InputSchema: tagListSchema()},
		{Name: "tags.list_all", Description: "List all tags across all projects with usage counts.", InputSchema: objectSchema(map[string]any{}, nil)},
		{Name: "tags.merge", Description: "Merge a source tag into a target tag, reassigning all references and deleting the source.", InputSchema: objectSchema(map[string]any{"source_tag_id": integerSchema("Source tag id to merge from (will be deleted)"), "target_tag_id": integerSchema("Target tag id to merge into (canonical)")}, []string{"source_tag_id", "target_tag_id"})},
		{Name: "errors.record", Description: "Record an error encountered during development with optional context and tags. Errors and their solutions are visible cross-project so the agent can reuse prior fixes.", InputSchema: recordErrorSchema()},
		{Name: "errors.search", Description: "Search errors by tag intersection and/or description text. Returns errors with nested solutions ranked by success then recency. Search is cross-project.", InputSchema: searchErrorsSchema()},
		{Name: "solutions.add", Description: "Attach a candidate solution to an error. Multiple solutions per error are supported.", InputSchema: addSolutionSchema()},
		{Name: "solutions.confirm", Description: "Confirm whether a solution worked. success=true marks it as the recommended fix and increments its like counter; success=false marks it as known-bad so the agent does not retry it without new context.", InputSchema: confirmSolutionSchema()},
		{Name: "solutions.list_top", Description: "List the top N most-liked solutions globally (cross-project). Useful to surface validated fixes and audit recurring patterns. Likes are incremented only by solutions.confirm(success=true).", InputSchema: listTopSolutionsSchema()},
		{Name: "templates.list", Description: "List every loaded template (slug, name, default kind, project scope, custom flag). Read-only; templates are authored by the user — the agent never modifies template bindings.", InputSchema: objectSchema(map[string]any{"kind": stringSchema("Optional default-kind filter (e.g. \"task\")"), "project": stringSchema("Optional project slug to scope project-bound templates"), "include_body": booleanSchema("Set true to include the template body in each entry; default omits it for compact responses")}, nil)},
		{Name: "templates.show", Description: "Return one template by slug, including its full body. Read-only.", InputSchema: objectSchema(map[string]any{"slug": stringSchema("Template slug")}, []string{"slug"})},
	}
}

func Resources() []ResourceDefinition {
	return []ResourceDefinition{
		{URI: "omakiten://project/overview", Name: "Active project overview", Description: "Compact overview of the active Omakiten project.", MIMEType: "application/json"},
		{URI: "omakiten://workflow/active", Name: "Active workflow", Description: "Active workflow buckets and transitions for the current Omakiten runtime.", MIMEType: "application/json"},
	}
}

// promptArguments declares the per-command argument list. Keeping it next to
// Prompts() means new commands only need a description in the agent layer
// plus, optionally, an entry here when they take inputs.
var promptArguments = map[string][]PromptArgument{
	"okt-imagine":   {{Name: "topic", Description: "Topic, problem, or feature seed to explore", Required: false}},
	"okt-create":    {{Name: "description", Description: "Task description", Required: true}},
	"okt-continue":  {{Name: "task_id", Description: "Task id", Required: true}},
	"okt-implement": {{Name: "task_id", Description: "Task id to implement", Required: false}},
	"okt-document":  {{Name: "focus", Description: "Optional area to focus the survey (e.g. 'README', 'architecture')", Required: false}},
}

func Prompts() []PromptDefinition {
	names := agent.CommandNames()
	out := make([]PromptDefinition, 0, len(names))
	for _, name := range names {
		out = append(out, PromptDefinition{
			Name:        name,
			Description: agent.CommandDescription(name),
			Arguments:   promptArguments[name],
		})
	}
	return out
}

func (a *Adapter) CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	if a.service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}

	if args == nil {
		args = map[string]any{}
	}
	agentModel, agentSessionID, err := extractAgentAttribution(args)
	if err != nil {
		return ToolResult{}, err
	}

	ctx = activity.WithAgent(ctx, "mcp", name, agentModel, agentSessionID)
	return a.dispatchTool(ctx, name, args)
}

// dispatchTool runs the bare tool dispatch with the activity context the
// caller already prepared. Splits out of CallTool so internal entry points
// (ReadResource) can bypass the coercive _agent_model validation — those
// calls are system-internal, not agent-driven, and shouldn't pollute the
// per-model metrics with synthetic samples.
func (a *Adapter) dispatchTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	if a.repo != nil {
		ctx = activity.WithRepository(ctx, a.repo)
	}

	var data any
	var err error
	switch name {
	case "project.overview":
		var input agent.OverviewInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.Overview(ctx, input)
		}
	case "project.resume":
		var input agent.ResumeProjectInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ResumeProject(ctx, input)
		}
	case "tasks.continue":
		var input agent.ContinueTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ContinueTask(ctx, input)
		}
	case "tasks.list":
		var input agent.ListTasksInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListTasks(ctx, input)
		}
	case "tasks.create_intent":
		var input agent.CreateTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.CreateTaskIntent(ctx, input)
		}
	case "tasks.create":
		var input agent.CreateTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.CreateTask(ctx, input)
		}
	case "tasks.move":
		var input agent.MoveTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.MoveTask(ctx, input)
		}
	case "comments.add":
		var input agent.AddCommentInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.AddComment(ctx, input)
		}
	case "comments.list":
		var input agent.ListCommentsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListComments(ctx, input)
		}
	case "task_activity.list":
		var input agent.ListTaskActivityInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListTaskActivity(ctx, input)
		}
	case "dependencies.add":
		var input agent.AddDependencyInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.AddDependency(ctx, input)
		}
	case "dependencies.remove":
		var input agent.RemoveDependencyInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.RemoveDependency(ctx, input)
		}
	case "dependencies.list":
		var input agent.ListDependenciesInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListDependencies(ctx, input)
		}
	case "context.add":
		var input agent.AddContextInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.AddContext(ctx, input)
		}
	case "context.dump":
		var input agent.DumpContextInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.DumpContext(ctx, input)
		}
	case "workflow.show":
		var input agent.WorkflowInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ShowWorkflow(ctx, input)
		}
	case "progress.record":
		var input agent.RecordProgressInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.RecordProgress(ctx, input)
		}
	case "tags.add":
		var input agent.AddTagInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.AddTag(ctx, input)
		}
	case "tags.remove":
		var input agent.RemoveTagInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.RemoveTag(ctx, input)
		}
	case "tags.list":
		var input agent.ListTagsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListTags(ctx, input)
		}
	case "tags.list_all":
		data, err = a.service.ListAllTags(ctx)
	case "tags.merge":
		var input agent.MergeTagsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.MergeTags(ctx, input)
		}
	case "errors.record":
		var input agent.RecordErrorInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.RecordError(ctx, input)
		}
	case "errors.search":
		var input agent.SearchErrorsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.SearchErrors(ctx, input)
		}
	case "solutions.add":
		var input agent.AddSolutionInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.AddSolution(ctx, input)
		}
	case "solutions.confirm":
		var input agent.ConfirmSolutionInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ConfirmSolution(ctx, input)
		}
	case "solutions.list_top":
		var input agent.ListTopSolutionsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListTopSolutions(ctx, input)
		}
	case "templates.list":
		var input agent.ListTemplatesInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ListTemplates(ctx, input)
		}
	case "templates.show":
		var input agent.ShowTemplateInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = a.service.ShowTemplate(ctx, input)
		}
	default:
		return ToolResult{}, fmt.Errorf("unknown MCP tool %q", name)
	}

	if err != nil {
		return resultFromData(agent.FailureFromError(err), true)
	}
	return resultFromData(data, false)
}

func (a *Adapter) ReadResource(ctx context.Context, uri string) (ToolResult, error) {
	if a.service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}
	// Resource reads are system-internal — no _agent_model validation.
	// Empty agent model marks them as "not benchmarked" so the metrics
	// layer can filter them out without a special sentinel.
	ctx = activity.WithAgent(ctx, "mcp", "resource:"+uri, "", "")
	switch uri {
	case "omakiten://project/overview":
		return a.dispatchTool(ctx, "project.overview", map[string]any{})
	case "omakiten://workflow/active":
		return a.dispatchTool(ctx, "workflow.show", map[string]any{})
	default:
		return ToolResult{}, fmt.Errorf("unknown MCP resource %q", uri)
	}
}

// GetPrompt resolves the bound persona, skills, laws, and templates for the
// requested `okt-*` command and returns a single PromptMessage carrying the
// composed markdown. Callers route through the adapter so the agent service's
// command catalog drives the response — when no service is wired (older
// runtimes / tests), GetPrompt falls back to the action text alone so the
// prompt still functions.
//
// When the bundle's `config.mcp.cache_prompts` is true (the default), the
// returned content carries a `_meta.anthropic.cache_control` hint so
// Anthropic-aware MCP clients can reuse the cached prompt across calls.
// Unaware clients simply ignore the metadata field — there is no protocol
// risk in always emitting it, but the toggle exists for users who want to
// observe pre/post caching behavior or work around a buggy client.
func (a *Adapter) GetPrompt(ctx context.Context, name string, _ map[string]any) (PromptResult, error) {
	if a == nil || a.service == nil {
		text := agent.CommandActionFallback(name)
		if text == "" {
			return PromptResult{}, fmt.Errorf("unknown MCP prompt %q", name)
		}
		// No service wired — emit the cache hint anyway since the prompt is
		// still byte-stable (it is just the canonical action text). Clients
		// that cache benefit; clients that don't ignore.
		return promptResult(text, text, true), nil
	}
	resolved, err := a.service.ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
	if err != nil {
		return PromptResult{}, err
	}
	description := resolved.Description
	if description == "" {
		description = resolved.Action
	}
	return promptResult(description, resolved.Markdown, a.service.SettingsCachePrompts()), nil
}

func promptResult(description, body string, cacheControl bool) PromptResult {
	content := ContentItem{Type: "text", Text: body}
	if cacheControl {
		content.Meta = map[string]any{
			"anthropic.cache_control": map[string]string{"type": "ephemeral"},
		}
	}
	return PromptResult{Description: description, Messages: []PromptMessage{{Role: "user", Content: content}}}
}

func decodeArgs(args map[string]any, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func resultFromData(data any, isError bool) (ToolResult, error) {
	text, err := jsonText(data)
	if err != nil {
		return ToolResult{}, err
	}
	return ToolResult{Content: []ContentItem{{Type: "text", Text: text}}, IsError: isError}, nil
}

func jsonText(data any) (string, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func selectorSchema() map[string]any {
	return objectSchema(selectorProperties(), nil)
}

func selectorProperties() map[string]any {
	return map[string]any{
		"project_id": integerSchema("Optional registered project id"),
		"project":    stringSchema("Optional registered project slug"),
		"cwd":        stringSchema("Optional working directory used for project resolution"),
	}
}

func createTaskSchema() map[string]any {
	props := selectorProperties()
	props["title"] = stringSchema("Optional task title; derived from description when omitted")
	props["description"] = stringSchema("Task description or title text")
	props["priority"] = stringSchema("Optional priority: low, normal, or high")
	props["confirmed"] = booleanSchema("Set true after user confirmation")
	props["template_slug"] = stringSchema("Optional slug of a loaded template; when set, the template body is merged into the description (user content first, template appended). Use templates.list to discover slugs.")
	return objectSchema(props, []string{"description"})
}

func dependencySchema(includeConfirmed bool) map[string]any {
	props := selectorProperties()
	props["task_id"] = integerSchema("Task id")
	props["depends_on_task_id"] = integerSchema("Dependency task id")
	if includeConfirmed {
		props["confirmed"] = booleanSchema("Required true to remove the dependency")
	}
	return objectSchema(props, []string{"task_id", "depends_on_task_id"})
}

func progressSchema() map[string]any {
	props := selectorProperties()
	props["task_id"] = integerSchema("Task id for task edits, comments, or movement")
	props["title"] = stringSchema("Optional updated title")
	props["description"] = stringSchema("Optional updated description")
	props["priority"] = stringSchema("Optional priority: low, normal, or high")
	props["move_to_bucket"] = stringSchema("Optional target workflow bucket key")
	props["comment"] = stringSchema("Optional progress comment")
	props["context"] = stringSchema("Optional project handoff context entry")
	props["author_type"] = stringSchema("human or agent")
	return objectSchema(props, nil)
}

func tagMutationSchema(includeTagID bool) map[string]any {
	props := selectorProperties()
	props["entity_type"] = stringSchema("Entity type: 'task' or 'project'")
	props["entity_id"] = integerSchema("Entity id (task_id for tasks; omit for projects)")
	if includeTagID {
		props["tag_id"] = integerSchema("Tag id to remove")
		props["confirmed"] = booleanSchema("Required true to remove the tag")
		return objectSchema(props, []string{"entity_type", "tag_id"})
	}
	props["tag_name"] = stringSchema("Tag name (normalized to kebab-case automatically)")
	return objectSchema(props, []string{"entity_type", "tag_name"})
}

func tagListSchema() map[string]any {
	props := selectorProperties()
	props["entity_type"] = stringSchema("Entity type: 'task' or 'project'")
	props["entity_id"] = integerSchema("Entity id (task_id for tasks; omit for projects)")
	return objectSchema(props, []string{"entity_type"})
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func arrayStringSchema(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}

func recordErrorSchema() map[string]any {
	props := selectorProperties()
	props["description"] = stringSchema("One-line description of the error")
	props["context"] = stringSchema("Optional surrounding context, stack trace, or symptoms")
	props["tags"] = arrayStringSchema("Tag names attached to this error (normalized to kebab-case). Use specific tags so future searches can match — e.g. [\"sqlite\", \"foreign-key\"].")
	return objectSchema(props, []string{"description"})
}

func searchErrorsSchema() map[string]any {
	props := selectorProperties()
	props["query"] = stringSchema("Optional substring matched against error description and context")
	props["tags"] = arrayStringSchema("Optional tag names; results match errors carrying ANY of these tags")
	return objectSchema(props, nil)
}

func addSolutionSchema() map[string]any {
	props := selectorProperties()
	props["error_id"] = integerSchema("Error id to attach the solution to")
	props["description"] = stringSchema("One-line description of the candidate solution")
	props["steps"] = stringSchema("Optional explicit steps, code snippet, or command to apply the solution")
	props["task_id"] = integerSchema("Optional task id where this solution was discovered/applied")
	return objectSchema(props, []string{"error_id", "description"})
}

func confirmSolutionSchema() map[string]any {
	props := selectorProperties()
	props["solution_id"] = integerSchema("Solution id to confirm")
	props["success"] = booleanSchema("true if the solution worked (also increments the solution's like counter); false to mark it as known-bad")
	return objectSchema(props, []string{"solution_id", "success"})
}

func listTopSolutionsSchema() map[string]any {
	props := selectorProperties()
	props["limit"] = integerSchema("Maximum number of solutions to return (default 10, max 100)")
	return objectSchema(props, nil)
}
