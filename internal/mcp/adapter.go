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
//
// Both fields are also declared on every tool's InputSchema by
// withAgentAttribution so schema-aware clients (Claude Code harness,
// JSON-Schema-validating SDKs) include them automatically — without that
// declaration the client strips the unknown keys and the call fails with
// a "_agent_model is required" rejection the LLM cannot recover from.
const (
	agentModelArgKey   = "_agent_model"
	agentSessionArgKey = "_agent_session_id"
)

const agentModelSchemaDescription = "AI model identifier invoking this tool (e.g. \"claude-opus-4-7\", \"claude-sonnet-4-6\", \"gpt-5\"). Required — the server rejects calls without it so /metrics.summary can benchmark per-model behaviour."

const agentSessionSchemaDescription = "Optional session id correlating multiple tool calls from the same agent run. Stripped before tool-specific decoding."

// extractAgentAttribution pulls the reserved keys out of args, rejecting
// the call when _agent_model is absent or empty. Failing closed forces
// every benchmark sample to carry a model id, which is the whole point of
// /metrics.summary. The schema (see withAgentAttribution) carries the
// usage hint; this guard exists for clients that bypass schema validation.
func extractAgentAttribution(args map[string]any) (model, sessionID string, err error) {
	rawModel, ok := args[agentModelArgKey]
	if !ok {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model is required on all MCP tool calls (see tool inputSchema for usage).", nil)
	}
	model, _ = rawModel.(string)
	if model == "" {
		return "", "", domain.NewError(domain.ErrValidation,
			"_agent_model must be a non-empty string (see tool inputSchema for usage).", nil)
	}
	delete(args, agentModelArgKey)

	if raw, present := args[agentSessionArgKey]; present {
		sessionID, _ = raw.(string)
		delete(args, agentSessionArgKey)
	}
	return model, sessionID, nil
}

type Adapter struct {
	// service is the static fallback NewAdapter captured. It is only
	// consulted when defaultProvider is nil (test paths that never
	// touch a BundleCache). Production wires defaultProvider via
	// SetDefaultServiceProvider so cache rebuilds always surface the
	// fresh service — a stale *agent.Service pointer was the Phase
	// 3b regression the provider fixes.
	service         *agent.Service
	defaultProvider DefaultServiceProvider
	repo            activity.ActivityLogRepository
	resolver        ServiceResolver
}

// ServiceResolver returns the per-project agent.Service that should
// handle a tool call. Implementations consult the per-process
// BundleCache (see agentruntime) and return the cached project's
// service, falling back to the default when project is empty or
// unresolvable. Returning (nil, nil) means "use the adapter's default
// service" — the adapter treats it as an idiomatic no-op rather than
// an error so resolvers can keep their decision tree shallow.
type ServiceResolver func(ctx context.Context, project string, projectID int64) (*agent.Service, error)

// DefaultServiceProvider returns the adapter's fallback service on
// every call. Threaded through SetDefaultServiceProvider so the
// adapter never caches a *agent.Service pointer that could go stale
// after a BundleCache rebuild. Returning nil falls back to the static
// service NewAdapter captured.
type DefaultServiceProvider func() *agent.Service

func NewAdapter(service *agent.Service) *Adapter {
	return &Adapter{service: service}
}

// SetDefaultServiceProvider replaces the static default service with
// a function the adapter consults on every CallTool / ReadResource.
// Required for any runtime that mutates the default service after
// adapter construction (mtime-driven BundleCache rebuilds, explicit
// Reload). Pass nil to revert to the captured static service.
func (a *Adapter) SetDefaultServiceProvider(provider DefaultServiceProvider) {
	a.defaultProvider = provider
}

// defaultService resolves the adapter's fallback service through the
// provider when wired, falling back to the static service NewAdapter
// captured. Returns nil when neither source produced a service —
// CallTool surfaces that to the caller as a configuration error.
func (a *Adapter) defaultService() *agent.Service {
	if a.defaultProvider != nil {
		if svc := a.defaultProvider(); svc != nil {
			return svc
		}
	}
	return a.service
}

func (a *Adapter) SetActivityLogRepository(repo activity.ActivityLogRepository) {
	a.repo = repo
}

// SetServiceResolver installs the per-project routing function. When
// present, every CallTool peeks `project` / `project_id` from the
// incoming args and asks the resolver which agent.Service should
// handle the call. The default service is used when the resolver is
// absent, when it returns nil, or when the args do not declare a
// project — that mirrors the pre-3b single-project behaviour for the
// installed tests and for clients that have not yet adopted per-project
// args.
func (a *Adapter) SetServiceResolver(resolver ServiceResolver) {
	a.resolver = resolver
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
	defs := tools()
	for i := range defs {
		defs[i].InputSchema = withAgentAttribution(defs[i].InputSchema)
	}
	return defs
}

// withAgentAttribution mutates an InputSchema in place to declare the
// reserved _agent_model (string, required) and _agent_session_id (string,
// optional) fields. Centralising the injection here keeps every tool
// schema in lockstep with extractAgentAttribution's contract — adding a
// new tool registration cannot forget the attribution fields.
func withAgentAttribution(schema map[string]any) map[string]any {
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	props[agentModelArgKey] = stringSchema(agentModelSchemaDescription)
	props[agentSessionArgKey] = stringSchema(agentSessionSchemaDescription)
	schema["properties"] = props

	required, _ := schema["required"].([]string)
	if !containsString(required, agentModelArgKey) {
		required = append(required, agentModelArgKey)
	}
	schema["required"] = required
	return schema
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func tools() []ToolDefinition {
	return []ToolDefinition{
		{Name: "project.overview", Description: "Return active project identity, workflow awareness, pending count, recent context, and next-step prompt.", InputSchema: selectorSchema()},
		{Name: "project.resume", Description: "Return project distribution, likely next work, blocked work, dependencies, recent context, and workflow state.", InputSchema: selectorSchema()},
		{Name: "tasks.continue", Description: "Load a project-owned task with dependencies, comments, workflow bucket, and recent handoff context. Set include_workflow=false on subsequent calls in a session where the workflow shape was already loaded by /okt to save tokens.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id to continue"), "include_workflow": booleanSchema("Optional override for config.mcp.include_workflow_in_continue. Pass false to skip the workflow block when /okt already loaded it.")}, []string{"task_id"})},
		{Name: "tasks.list", Description: "List active project tasks, optionally filtered by workflow bucket and/or parent. The parent_id filter is tri-state: omit for no filter (every task), pass null for roots only (parent_id IS NULL), or pass a task id for that parent's direct children.", InputSchema: objectSchema(map[string]any{"bucket_key": stringSchema("Optional workflow bucket key"), "parent_id": nullableIntegerSchema("Optional tri-state parent filter: omit for no filter; pass null for roots only (parent_id IS NULL); pass a task id for direct children of that id.")}, nil)},
		{Name: "tasks.create_intent", Description: "Create a task intent after checking for similar or related project tasks and requiring confirmation when needed.", InputSchema: createTaskSchema()},
		{Name: "tasks.create", Description: "Create a task directly through Omakiten's shared task service.", InputSchema: createTaskSchema()},
		{Name: "tasks.move", Description: "Move a task through an allowed workflow transition.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "bucket_key": stringSchema("Target bucket key")}, []string{"task_id", "bucket_key"})},
		{Name: "tasks.edit", Description: "Edit a task's title, description, priority, and/or parent_id. Provide at least one of the optional fields; the service rejects no-op calls. Subject to bucket policy (permissions.task.edit) — the default kit allows edits only in the planning bucket. Bucket moves go through tasks.move so the activity log distinguishes the two intents. The parent_id field is tri-state: omit to leave parent_id untouched, pass null to clear (re-root the task), or pass a task id to re-parent (anti-cycle is enforced — naming a descendant fails with the conflicting ancestor surfaced).", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "title": stringSchema("Optional new title"), "description": stringSchema("Optional new description"), "priority": stringSchema("Optional priority label resolved against config.priorities (e.g. \"low\", \"normal\", \"high\")"), "parent_id": nullableIntegerSchema("Optional tri-state re-parent: omit to leave parent_id alone; pass null to clear (becomes a root); pass a task id to re-parent with anti-cycle enforcement.")}, []string{"task_id"})},
		{Name: "tasks.delete", Description: "Hard-delete a task with cascade (comments, tags, dependencies, events). Subject to bucket policy (permissions.task.delete) and operations.delete.guards.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "confirmed": booleanSchema("Required true to actually delete the task")}, []string{"task_id"})},
		{Name: "tasks.archive", Description: "Archive a task (state=archived) and move it into the workflow's final bucket. Bypasses bucket policy and transition guards but respects operations.archive.guards.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})},
		{Name: "tasks.unarchive", Description: "Restore an archived task to active state, leaving its current bucket intact. Respects operations.unarchive.guards if declared.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})},
		{Name: "comments.add", Description: "Add a human or agent comment to a project-owned task. Optionally tag the comment with one or more tag names (normalized to kebab-case) or pre-fill its body from a loaded template.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "body": stringSchema("Comment body"), "author_type": stringSchema("human or agent"), "tags": arrayStringSchema("Optional tag names to attach to this comment (e.g. [\"resume\", \"deployment-notes\"])"), "template_slug": stringSchema("Optional slug of a loaded template; when set, the template body is merged into the comment (user content first, template appended).")}, []string{"task_id", "body"})},
		{Name: "comments.list", Description: "List comments for a project-owned task.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})},
		{Name: "comments.edit", Description: "Rewrite a comment body and replace its tags. Subject to bucket policy (permissions.comment.edit, inherited from permissions.task when not declared).", InputSchema: objectSchema(map[string]any{"comment_id": integerSchema("Comment id"), "body": stringSchema("New comment body"), "tags": arrayStringSchema("Optional tag names; replaces all existing tags on the comment")}, []string{"comment_id", "body"})},
		{Name: "comments.delete", Description: "Hard-delete a comment. Subject to bucket policy (permissions.comment.delete, inherited from permissions.task when not declared).", InputSchema: objectSchema(map[string]any{"comment_id": integerSchema("Comment id"), "confirmed": booleanSchema("Required true to actually delete the comment")}, []string{"comment_id"})},
		{Name: "task_activity.list", Description: "Return the unified activity feed for a task: comments and system events (task.created, task.moved, task.completed) ordered chronologically.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id"), "order": stringSchema("Sort order: 'asc' (chronological, default) or 'desc' (newest first)")}, []string{"task_id"})},
		{Name: "logs.list", Description: "Generic Logs inspector over the unified events log. Returns every event_type — task lifecycle, comments, plans, guards, hooks, tool calls (CLI/MCP/TUI), tricks, audits, and domain bookkeeping — each row carrying a rendered `summary` string so the agent does not have to parse the payload JSON. Default scope is the active project over the configured window (config.views.logs.window_days, 30 days by default). Pass `categories=[\"tool_call\"]` to reproduce the legacy activity-log filter; pass `since=\"24h\"` to narrow the window. Allowed categories: task, comment, plan, tag-dep, guard, audit, hook, tool_call, trick, domain.", InputSchema: logsListSchema()},
		{Name: "dependencies.add", Description: "Add a project-scoped task dependency with cycle prevention.", InputSchema: dependencySchema(false)},
		{Name: "dependencies.remove", Description: "Remove a task dependency after explicit confirmation.", InputSchema: dependencySchema(true)},
		{Name: "dependencies.list", Description: "List dependencies for one task or all active project tasks.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Optional task id; omit or set 0 for all")}, nil)},
		{Name: "context.add", Description: "Add a project handoff context entry.", InputSchema: objectSchema(map[string]any{"body": stringSchema("Context body")}, []string{"body"})},
		{Name: "context.dump", Description: "Dump compact project context at level 1, 2, or 3.", InputSchema: objectSchema(map[string]any{"level": integerSchema("Context level: 1, 2, or 3")}, nil)},
		{Name: "workflow.show", Description: "Show the active workflow buckets and allowed transitions.", InputSchema: selectorSchema()},
		{Name: "orphans.migrate", Description: "Detect tasks whose bucket was deactivated by a workflow swap and rebind them to the active workflow (matching key when preserved, first bucket otherwise). First call without confirmed=true returns a preview report plus a Confirmation block listing every affected task; retry with confirmed=true to apply the rebind. Empty preview short-circuits to a no-op.", InputSchema: objectSchema(map[string]any{"confirmed": booleanSchema("Required true to apply the rebind; first call returns a preview with affected tasks.")}, nil)},
		{Name: "progress.record", Description: "Record material agent progress through task edits, comments, context entries, and optional workflow movement.", InputSchema: progressSchema()},
		{Name: "tags.add", Description: "Add a reusable tag to a task or project. The tag name is normalized to kebab-case and deduplicated automatically.", InputSchema: tagMutationSchema(false)},
		{Name: "tags.remove", Description: "Remove a tag from a task or project after explicit confirmation.", InputSchema: tagMutationSchema(true)},
		{Name: "tags.list", Description: "List tags for a specific task or project.", InputSchema: tagListSchema()},
		{Name: "tags.list_all", Description: "List all tags across all projects with usage counts.", InputSchema: objectSchema(map[string]any{}, nil)},
		{Name: "tags.merge", Description: "Merge a source tag into a target tag, reassigning all references and deleting the source.", InputSchema: objectSchema(map[string]any{"source_tag_id": integerSchema("Source tag id to merge from (will be deleted)"), "target_tag_id": integerSchema("Target tag id to merge into (canonical)")}, []string{"source_tag_id", "target_tag_id"})},
		{Name: "errors.record", Description: "Record an error encountered during development with optional context and tags. Errors and their solutions are visible cross-project so the agent can reuse prior fixes.", InputSchema: recordErrorSchema()},
		{Name: "search", Description: "Full-text search across tasks, comments, errors, solutions, and context entries using SQLite FTS5. Returns BM25-ranked hits with snippets (<mark>...</mark> highlights). Optional `entity_types` filter restricts the indexed kinds; omit `project`/`project_id` for a cross-project view. Archived tasks are filtered out automatically. Replaces the legacy `errors.search` tool — equivalent call: search(query, entity_types=[\"error\"]).", InputSchema: searchSchema()},
		{Name: "solutions.add", Description: "Attach a candidate solution to an error. Multiple solutions per error are supported.", InputSchema: addSolutionSchema()},
		{Name: "solutions.confirm", Description: "Confirm whether a solution worked. success=true marks it as the recommended fix and increments its like counter; success=false marks it as known-bad so the agent does not retry it without new context.", InputSchema: confirmSolutionSchema()},
		{Name: "solutions.list_top", Description: "List the top N most-liked solutions globally (cross-project). Useful to surface validated fixes and audit recurring patterns. Likes are incremented only by solutions.confirm(success=true).", InputSchema: listTopSolutionsSchema()},
		{Name: "templates.list", Description: "List every loaded template (slug, name, default kind, project scope, custom flag). Read-only; templates are authored by the user — the agent never modifies template bindings.", InputSchema: objectSchema(map[string]any{"kind": stringSchema("Optional default-kind filter (e.g. \"task\")"), "project": stringSchema("Optional project slug to scope project-bound templates"), "include_body": booleanSchema("Set true to include the template body in each entry; default omits it for compact responses")}, nil)},
		{Name: "metrics.summary", Description: "Aggregate per-AI-model behaviour over a period. Each row carries a `buckets` map keyed by metric tag (`error_recorded`, `error_searched`, `solution_added`, `solution_liked`, `solution_failed`, `solution_top_viewed`) plus `like_rate`, `search_before_record_ratio`, and `session_correlated_sample`. Use to benchmark whether different agents research existing context before recording new errors. Requires that callers pass _agent_model on every tool call (now coercive).", InputSchema: objectSchema(map[string]any{"period": stringSchema("Time window: \"7d\", \"30d\" (default), or \"all\""), "project_id": integerSchema("Optional registered project id; omit for cross-project view")}, nil)},
		{Name: "templates.show", Description: "Return one template by slug, including its full body. Read-only. Hard-rejects (validation_error) when the requested slug is a global template that is shadowed by a project-scoped override in the active project — the rejection's details name the active slug so callers can re-call directly.", InputSchema: showTemplateSchema()},
		{Name: "plans.create", Description: "Create a WBS-style plan that groups child tasks in ordered waves. Slug must be unique within the project; goal_body is markdown describing the plan's intent and acceptance criteria. Emits plan.created.", InputSchema: objectSchema(map[string]any{"slug": stringSchema("Plan slug (kebab-case recommended); unique per project"), "name": stringSchema("Human-readable plan name"), "goal_body": stringSchema("Optional markdown body describing the plan goal and acceptance criteria")}, []string{"slug", "name"})},
		{Name: "plans.list", Description: "List every plan in the active project, ordered by creation. Goal bodies are omitted from list entries — call plans.show to fetch one with its full body.", InputSchema: selectorSchema()},
		{Name: "plans.show", Description: "Return one plan with its waves, tasks per wave, per-wave and overall done/total counts, integer percent, and the active wave id (lowest-position wave with pending work). Archived tasks are filtered out of the counts but stay in the wave's tasks list.", InputSchema: objectSchema(map[string]any{"slug": stringSchema("Plan slug")}, []string{"slug"})},
		{Name: "plans.add_wave", Description: "Append a wave to a plan (position=0 auto-assigns after the current highest position; explicit position>0 inserts at that slot and rejects on collision). Identify the plan by slug or plan_id; supply at least one. Emits plan.wave_added.", InputSchema: objectSchema(map[string]any{"slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)"), "name": stringSchema("Wave name (human-readable)"), "position": integerSchema("Optional 1-based wave position; omit or 0 to append after the current highest")}, []string{"name"})},
		{Name: "plans.assign_task", Description: "Attach an existing task to a (plan, wave). Identify the plan by slug or plan_id; supply at least one. Cross-plan / cross-project wave references are rejected.", InputSchema: objectSchema(map[string]any{"task_id": integerSchema("Task id to attach"), "slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)"), "wave_id": integerSchema("Wave id; must belong to the named plan")}, []string{"task_id", "wave_id"})},
		{Name: "plans.claim_next", Description: "Atomically reserve the next claimable task in the plan's active wave (lowest-position wave with pending tasks). Claimable means active, unassigned, and still in the workflow's first bucket. Stamps tasks.assigned_to with the caller's _agent_model and emits task.assigned; the bucket is not moved, so callers must use tasks.move separately once preset guards are satisfied. Returns claimed=false (no task) when every wave is fully done or no unassigned first-bucket task remains in the active wave. Concurrency-safe via BEGIN IMMEDIATE on a pinned connection.", InputSchema: objectSchema(map[string]any{"slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)")}, nil)},
		{Name: "plans.continue", Description: "Agent-tailored projection of a plan: returns the same aggregate plans.show emits (full plan + waves + done/total + active wave) plus a non-mutating preview of the task plans.claim_next would reserve next. Use before plans.claim_next so an agent can inspect goal_body, the wave layout, and the candidate task before committing to a claim.", InputSchema: objectSchema(map[string]any{"slug": stringSchema("Plan slug")}, []string{"slug"})},
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
	service := a.defaultService()
	if service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}

	if args == nil {
		args = map[string]any{}
	}
	agentModel, agentSessionID, err := extractAgentAttribution(args)
	if err != nil {
		return ToolResult{}, err
	}

	if a.resolver != nil {
		project, projectID := peekProjectArg(args)
		if resolved, err := a.resolver(ctx, project, projectID); err == nil && resolved != nil {
			service = resolved
		}
	}

	ctx = activity.WithAgent(ctx, "mcp", name, agentModel, agentSessionID)
	return a.dispatchTool(ctx, service, name, args)
}

// peekProjectArg extracts `project` / `project_id` from an arbitrary
// tool input WITHOUT consuming them — handlers still decode the same
// keys via the input struct's embedded ProjectSelector. Numeric values
// arrive as float64 (json.Unmarshal default) or json.Number; both
// shapes are accepted.
func peekProjectArg(args map[string]any) (project string, projectID int64) {
	if raw, ok := args["project"]; ok {
		if s, ok := raw.(string); ok {
			project = s
		}
	}
	if raw, ok := args["project_id"]; ok {
		switch v := raw.(type) {
		case float64:
			projectID = int64(v)
		case int64:
			projectID = v
		case int:
			projectID = int64(v)
		case json.Number:
			if i, err := v.Int64(); err == nil {
				projectID = i
			}
		}
	}
	return project, projectID
}

// dispatchTool runs the bare tool dispatch with the activity context the
// caller already prepared. Splits out of CallTool so internal entry points
// (ReadResource) can bypass the coercive _agent_model validation — those
// calls are system-internal, not agent-driven, and shouldn't pollute the
// per-model metrics with synthetic samples.
//
// The service parameter is resolved by the caller (CallTool peeks the
// project arg and asks the resolver; ReadResource always uses the
// default). dispatch itself never reads a.service so per-project
// routing works without cross-call interference.
func (a *Adapter) dispatchTool(ctx context.Context, service *agent.Service, name string, args map[string]any) (ToolResult, error) {
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
			data, err = service.Overview(ctx, input)
		}
	case "project.resume":
		var input agent.ResumeProjectInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ResumeProject(ctx, input)
		}
	case "tasks.continue":
		var input agent.ContinueTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ContinueTask(ctx, input)
		}
	case "tasks.list":
		var input agent.ListTasksInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListTasks(ctx, input)
		}
	case "tasks.create_intent":
		var input agent.CreateTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.CreateTaskIntent(ctx, input)
		}
	case "tasks.create":
		var input agent.CreateTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.CreateTask(ctx, input)
		}
	case "tasks.move":
		var input agent.MoveTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.MoveTask(ctx, input)
		}
	case "tasks.edit":
		var input agent.EditTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.EditTask(ctx, input)
		}
	case "tasks.delete":
		var input agent.DeleteTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.DeleteTask(ctx, input)
		}
	case "tasks.archive":
		var input agent.ArchiveTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ArchiveTask(ctx, input)
		}
	case "tasks.unarchive":
		var input agent.ArchiveTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.UnarchiveTask(ctx, input)
		}
	case "comments.add":
		var input agent.AddCommentInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AddComment(ctx, input)
		}
	case "comments.list":
		var input agent.ListCommentsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListComments(ctx, input)
		}
	case "comments.edit":
		var input agent.EditCommentInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.EditComment(ctx, input)
		}
	case "comments.delete":
		var input agent.DeleteCommentInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.DeleteComment(ctx, input)
		}
	case "task_activity.list":
		var input agent.ListTaskActivityInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListTaskActivity(ctx, input)
		}
	case "logs.list":
		var input agent.ListLogsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListLogs(ctx, input)
		}
	case "dependencies.add":
		var input agent.AddDependencyInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AddDependency(ctx, input)
		}
	case "dependencies.remove":
		var input agent.RemoveDependencyInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.RemoveDependency(ctx, input)
		}
	case "dependencies.list":
		var input agent.ListDependenciesInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListDependencies(ctx, input)
		}
	case "context.add":
		var input agent.AddContextInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AddContext(ctx, input)
		}
	case "context.dump":
		var input agent.DumpContextInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.DumpContext(ctx, input)
		}
	case "workflow.show":
		var input agent.WorkflowInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ShowWorkflow(ctx, input)
		}
	case "orphans.migrate":
		var input agent.MigrateOrphansInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.MigrateOrphans(ctx, input)
		}
	case "progress.record":
		var input agent.RecordProgressInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.RecordProgress(ctx, input)
		}
	case "tags.add":
		var input agent.AddTagInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AddTag(ctx, input)
		}
	case "tags.remove":
		var input agent.RemoveTagInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.RemoveTag(ctx, input)
		}
	case "tags.list":
		var input agent.ListTagsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListTags(ctx, input)
		}
	case "tags.list_all":
		data, err = service.ListAllTags(ctx)
	case "tags.merge":
		var input agent.MergeTagsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.MergeTags(ctx, input)
		}
	case "errors.record":
		var input agent.RecordErrorInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.RecordError(ctx, input)
		}
	case "search":
		var input agent.SearchInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.Search(ctx, input)
		}
	case "solutions.add":
		var input agent.AddSolutionInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AddSolution(ctx, input)
		}
	case "solutions.confirm":
		var input agent.ConfirmSolutionInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ConfirmSolution(ctx, input)
		}
	case "solutions.list_top":
		var input agent.ListTopSolutionsInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListTopSolutions(ctx, input)
		}
	case "templates.list":
		var input agent.ListTemplatesInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListTemplates(ctx, input)
		}
	case "templates.show":
		var input agent.ShowTemplateInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ShowTemplate(ctx, input)
		}
	case "metrics.summary":
		var input agent.MetricsSummaryInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.MetricsSummary(ctx, input)
		}
	case "plans.create":
		var input agent.CreatePlanInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.CreatePlan(ctx, input)
		}
	case "plans.list":
		var input agent.ListPlansInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ListPlans(ctx, input)
		}
	case "plans.show":
		var input agent.ShowPlanInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ShowPlan(ctx, input)
		}
	case "plans.add_wave":
		var input agent.AddPlanWaveInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AddPlanWave(ctx, input)
		}
	case "plans.assign_task":
		var input agent.AssignPlanTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.AssignPlanTask(ctx, input)
		}
	case "plans.claim_next":
		var input agent.ClaimNextPlanTaskInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ClaimNextPlanTask(ctx, input)
		}
	case "plans.continue":
		var input agent.ContinuePlanInput
		err = decodeArgs(args, &input)
		if err == nil {
			data, err = service.ContinuePlan(ctx, input)
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
	service := a.defaultService()
	if service == nil {
		return ToolResult{}, fmt.Errorf("mcp adapter requires an agent service")
	}
	// Resource reads are system-internal — no _agent_model validation.
	// Empty agent model marks them as "not benchmarked" so the metrics
	// layer can filter them out without a special sentinel.
	ctx = activity.WithAgent(ctx, "mcp", "resource:"+uri, "", "")
	switch uri {
	case "omakiten://project/overview":
		return a.dispatchTool(ctx, service, "project.overview", map[string]any{})
	case "omakiten://workflow/active":
		return a.dispatchTool(ctx, service, "workflow.show", map[string]any{})
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
	var service *agent.Service
	if a != nil {
		service = a.defaultService()
	}
	if service == nil {
		text := agent.CommandActionFallback(name)
		if text == "" {
			return PromptResult{}, fmt.Errorf("unknown MCP prompt %q", name)
		}
		// No service wired — emit the cache hint anyway since the prompt is
		// still byte-stable (it is just the canonical action text). Clients
		// that cache benefit; clients that don't ignore.
		return promptResult(text, text, true), nil
	}
	resolved, err := service.ResolveCommand(ctx, agent.ResolveCommandInput{Name: name})
	if err != nil {
		return PromptResult{}, err
	}
	description := resolved.Description
	if description == "" {
		description = resolved.Action
	}
	return promptResult(description, resolved.Markdown, service.SettingsCachePrompts()), nil
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

func showTemplateSchema() map[string]any {
	props := selectorProperties()
	props["slug"] = stringSchema("Template slug")
	return objectSchema(props, []string{"slug"})
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
	props["parent_id"] = nullableIntegerSchema("Optional parent task id. Set to an existing task's id to attach the new row as a sub-task via TaskService.AddSub (parent must belong to the same project and be active; cross-bucket parents are rejected). Omit or pass null to create a root task.")
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

// nullableIntegerSchema declares an optional integer that also accepts the JSON
// null literal. The schema-validating MCP client passes integers as-is, keeps
// `null` (instead of stripping it as "unknown"), and drops the property entirely
// when the caller leaves it absent — preserving the tri-state encoding the
// agent layer relies on for fields like tasks.parent_id (`omitted` /
// `null` / `id`).
func nullableIntegerSchema(description string) map[string]any {
	return map[string]any{"type": []string{"integer", "null"}, "description": description}
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

func searchSchema() map[string]any {
	props := selectorProperties()
	props["query"] = stringSchema("FTS5 MATCH expression — phrase, prefix*, NEAR, AND/OR/NOT supported (see sqlite.org/fts5.html). Required.")
	props["entity_types"] = arrayStringSchema("Optional restriction to a subset of entity types. Allowed: task, comment, error, solution, context. Empty or omitted indexes all five.")
	return objectSchema(props, []string{"query"})
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
	props["limit"] = integerSchema("Maximum number of solutions to return (defaults and caps come from config.solutions; omitted/<=0 = default_top_limit, larger values clamped to max_top_limit)")
	return objectSchema(props, nil)
}

// logsListSchema declares the param surface for the `logs.list` tool.
// All four params are optional so the no-arg call returns a useful
// default response (last window_days of every category, descending).
// Categories enumerates the legal EventCategory string values so
// schema-aware clients (and the human reading mcp.md) can drive the
// chip without re-deriving them from the Go enum.
func logsListSchema() map[string]any {
	props := selectorProperties()
	props["categories"] = map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string", "enum": []string{"task", "comment", "plan", "tag-dep", "guard", "audit", "hook", "tool_call", "trick", "domain"}},
		"description": "Optional list of EventCategory values to include. Empty/omitted = every category. Example: [\"tool_call\"] reproduces the legacy activity-log filter; [\"task\", \"comment\"] narrows to task lifecycle and comments.",
	}
	props["since"] = stringSchema("Optional time floor as a Go duration (\"24h\", \"30m\") or N-day shorthand (\"7d\", \"30d\"). Omitted → use the project's configured Logs window (config.views.logs.window_days, 30 days by default).")
	props["limit"] = integerSchema("Optional row cap. 0/omitted/>10000 = capped at 10000 by the SQL layer's safety ceiling.")
	props["order"] = stringSchema("Sort direction: \"desc\" (default, newest first) or \"asc\" (oldest first). Anything else falls back to \"desc\".")
	return objectSchema(props, nil)
}
