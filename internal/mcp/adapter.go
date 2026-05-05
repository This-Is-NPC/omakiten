package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"omakiten/internal/activity"
	"omakiten/internal/agent"
)

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
	Type string `json:"type"`
	Text string `json:"text"`
}

func Tools() []ToolDefinition {
	return []ToolDefinition{
		{Name: "project.overview", Description: "Return active project identity, workflow awareness, pending count, recent context, and next-step prompt.", InputSchema: selectorSchema()},
		{Name: "project.resume", Description: "Return project distribution, likely next work, blocked work, dependencies, recent context, and workflow state.", InputSchema: selectorSchema()},
		{Name: "tasks.continue", Description: "Load a project-owned task with dependencies, comments, workflow bucket, and recent handoff context.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id to continue")}, []string{"task_id"})},
		{Name: "tasks.list", Description: "List active project tasks, optionally filtered by workflow bucket.", InputSchema: objectSchema(map[string]any{"bucket_key": stringSchema("Optional workflow bucket key")}, nil)},
		{Name: "tasks.create_intent", Description: "Create a task intent after checking for similar or related project tasks and requiring confirmation when needed.", InputSchema: createTaskSchema()},
		{Name: "tasks.create", Description: "Create a task directly through Omakiten's shared task service.", InputSchema: createTaskSchema()},
		{Name: "tasks.move", Description: "Move a task through an allowed workflow transition.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "bucket_key": stringSchema("Target bucket key")}, []string{"task_id", "bucket_key"})},
		{Name: "comments.add", Description: "Add a human or agent comment to a project-owned task.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "body": stringSchema("Comment body"), "author_type": stringSchema("human or agent")}, []string{"task_id", "body"})},
		{Name: "comments.list", Description: "List comments for a project-owned task.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})},
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
	}
}

func Resources() []ResourceDefinition {
	return []ResourceDefinition{
		{URI: "omakiten://project/overview", Name: "Active project overview", Description: "Compact overview of the active Omakiten project.", MIMEType: "application/json"},
		{URI: "omakiten://workflow/active", Name: "Active workflow", Description: "Active workflow buckets and transitions for the current Omakiten runtime.", MIMEType: "application/json"},
	}
}

func Prompts() []PromptDefinition {
	return []PromptDefinition{
		{Name: "okt", Description: "Contextualize the agent with active Omakiten project state."},
		{Name: "okt-create", Description: "Create a task intent with duplicate/related-work detection.", Arguments: []PromptArgument{{Name: "description", Description: "Task description", Required: true}}},
		{Name: "okt-continue", Description: "Continue a specific Omakiten task by id.", Arguments: []PromptArgument{{Name: "task_id", Description: "Task id", Required: true}}},
		{Name: "okt-resume", Description: "Resume from the most relevant active project checkpoint."},
	}
}

func (a *Adapter) CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	if a.service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}

	ctx = activity.WithSource(ctx, "mcp", name)
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
	default:
		return ToolResult{}, fmt.Errorf("unknown MCP tool %q", name)
	}

	if err != nil {
		return resultFromData(agent.FailureFromError(err), true)
	}
	return resultFromData(data, false)
}

func (a *Adapter) ReadResource(ctx context.Context, uri string) (ToolResult, error) {
	switch uri {
	case "omakiten://project/overview":
		return a.CallTool(ctx, "project.overview", nil)
	case "omakiten://workflow/active":
		return a.CallTool(ctx, "workflow.show", nil)
	default:
		return ToolResult{}, fmt.Errorf("unknown MCP resource %q", uri)
	}
}

func GetPrompt(name string, args map[string]any) (PromptResult, error) {
	switch name {
	case "okt":
		return promptResult("Use the `project.overview` tool to load active Omakiten project identity, pending work, workflow state, recent context, and the next-step prompt."), nil
	case "okt-create":
		return promptResult("Use the `tasks.create_intent` tool with the provided description. If it returns `requires_confirmation`, ask the user whether to continue an existing task or create a separate confirmed task."), nil
	case "okt-continue":
		return promptResult("Use the `tasks.continue` tool for the requested task id. Only continue if the task belongs to the active project; otherwise follow the coded guidance."), nil
	case "okt-resume":
		return promptResult("Use the `project.resume` tool to identify likely continuation points, blocked/dependent work, recent context, and current workflow state."), nil
	default:
		return PromptResult{}, fmt.Errorf("unknown MCP prompt %q", name)
	}
}

func promptResult(text string) PromptResult {
	return PromptResult{Description: text, Messages: []PromptMessage{{Role: "user", Content: ContentItem{Type: "text", Text: text}}}}
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
