package mcp

import (
	"context"
	"fmt"

	"omakiten/internal/agent"
)

type toolHandler func(*Adapter, context.Context, *agent.Service, map[string]any) (ToolResult, error)

type toolRegistration struct {
	name        string
	description string
	schema      func() map[string]any
	handler     toolHandler
}

type toolRegistry struct {
	ordered  []toolRegistration
	handlers map[string]toolHandler
}

func newToolRegistry(registrations []toolRegistration) (toolRegistry, error) {
	registry := toolRegistry{
		ordered:  append([]toolRegistration(nil), registrations...),
		handlers: make(map[string]toolHandler, len(registrations)),
	}
	for i, registration := range registry.ordered {
		if registration.name == "" || registration.description == "" || registration.schema == nil || registration.handler == nil {
			return toolRegistry{}, fmt.Errorf("incomplete MCP tool registration at index %d", i)
		}
		if registration.schema() == nil {
			return toolRegistry{}, fmt.Errorf("MCP tool %q schema factory returned nil", registration.name)
		}
		if _, exists := registry.handlers[registration.name]; exists {
			return toolRegistry{}, fmt.Errorf("duplicate MCP tool registration %q", registration.name)
		}
		registry.handlers[registration.name] = registration.handler
	}
	return registry, nil
}

func mustToolRegistry(registrations []toolRegistration) toolRegistry {
	registry, err := newToolRegistry(registrations)
	if err != nil {
		panic(err)
	}
	return registry
}

func serviceTool[Input, Output any](call func(*agent.Service, context.Context, Input) (Output, error)) toolHandler {
	return func(_ *Adapter, ctx context.Context, service *agent.Service, args map[string]any) (ToolResult, error) {
		var input Input
		if err := decodeArgs(args, &input); err != nil {
			return resultFromData(agent.FailureFromError(err), true)
		}
		data, err := call(service, ctx, input)
		if err != nil {
			return resultFromData(agent.FailureFromError(err), true)
		}
		return resultFromData(data, false)
	}
}

func tagsListAllTool(_ *Adapter, ctx context.Context, service *agent.Service, _ map[string]any) (ToolResult, error) {
	data, err := service.ListAllTags(ctx)
	if err != nil {
		return resultFromData(agent.FailureFromError(err), true)
	}
	return resultFromData(data, false)
}

func commandsListTool(adapter *Adapter, _ context.Context, _ *agent.Service, _ map[string]any) (ToolResult, error) {
	return resultFromData(adapter.Prompts(), false)
}

func commandsResolveTool(_ *Adapter, ctx context.Context, service *agent.Service, args map[string]any) (ToolResult, error) {
	return resolveCommandTool(ctx, service, args)
}

var registeredTools = mustToolRegistry([]toolRegistration{
	{name: "project.overview", description: "Return active project identity, workflow awareness, pending count, and next-step prompt.", schema: selectorSchema, handler: serviceTool((*agent.Service).Overview)},
	{name: "project.resume", description: "Return project distribution, likely next work, blocked work, dependencies, and workflow state.", schema: selectorSchema, handler: serviceTool((*agent.Service).ResumeProject)},
	{name: "project.edit", description: "Update the active project's description, persisting it to the projects.description column and emitting project.updated when the value changes. Returns the refreshed project DTO with the new description.", schema: editProjectSchema, handler: serviceTool((*agent.Service).EditProject)},
	{name: "tasks.continue", description: "Load a project-owned task with dependencies, comments, and workflow bucket. Set include_workflow=false on subsequent calls in a session where the workflow shape was already loaded by /okt to save tokens.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id to continue"), "include_workflow": booleanSchema("Optional override for config.mcp.include_workflow_in_continue. Pass false to skip the workflow block when /okt already loaded it.")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).ContinueTask)},
	{name: "tasks.list", description: "List active project tasks, optionally filtered by workflow bucket and/or parent. The parent_id filter is tri-state: omit for no filter (every task), pass null for roots only (parent_id IS NULL), or pass a task id for that parent's direct children.", schema: func() map[string]any {
		return objectSchema(map[string]any{"bucket_key": stringSchema("Optional workflow bucket key"), "parent_id": nullableIntegerSchema("Optional tri-state parent filter: omit for no filter; pass null for roots only (parent_id IS NULL); pass a task id for direct children of that id.")}, nil)
	}, handler: serviceTool((*agent.Service).ListTasks)},
	{name: "tasks.create_intent", description: "Create a task intent after checking for similar or related project tasks and requiring confirmation when needed.", schema: createTaskSchema, handler: serviceTool((*agent.Service).CreateTaskIntent)},
	{name: "tasks.create", description: "Create a task directly through Omakiten's shared task service.", schema: createTaskSchema, handler: serviceTool((*agent.Service).CreateTask)},
	{name: "tasks.move", description: "Move a task through an allowed workflow transition.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id"), "bucket_key": stringSchema("Target bucket key")}, []string{"task_id", "bucket_key"})
	}, handler: serviceTool((*agent.Service).MoveTask)},
	{name: "tasks.edit", description: "Edit a task's title, description, priority, and/or parent_id. Provide at least one of the optional fields; the service rejects no-op calls. Subject to bucket policy (permissions.task.edit) — the default kit allows edits only in the planning bucket. Bucket moves go through tasks.move so the activity log distinguishes the two intents. The parent_id field is tri-state: omit to leave parent_id untouched, pass null to clear (re-root the task), or pass a task id to re-parent (anti-cycle is enforced — naming a descendant fails with the conflicting ancestor surfaced).", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id"), "title": stringSchema("Optional new title"), "description": stringSchema("Optional new description"), "priority": stringSchema("Optional priority label resolved against config.priorities (e.g. \"low\", \"normal\", \"high\")"), "parent_id": nullableIntegerSchema("Optional tri-state re-parent: omit to leave parent_id alone; pass null to clear (becomes a root); pass a task id to re-parent with anti-cycle enforcement.")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).EditTask)},
	{name: "tasks.delete", description: "Hard-delete a task with cascade (comments, tags, dependencies, events). Subject to bucket policy (permissions.task.delete) and operations.delete.guards.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id"), "confirmed": booleanSchema("Required true to actually delete the task")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).DeleteTask)},
	{name: "tasks.archive", description: "Archive a task (state=archived) and move it into the workflow's final bucket. Bypasses bucket policy and transition guards but respects operations.archive.guards.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).ArchiveTask)},
	{name: "tasks.unarchive", description: "Restore an archived task to active state, leaving its current bucket intact. Respects operations.unarchive.guards if declared.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).UnarchiveTask)},
	{name: "comments.add", description: "Add a scope-aware human or agent comment. scope selects where it hangs: task (default; requires task_id), project (requires no task_id), or universal (cross-project; no task_id). Optional note-like fields kind/title/pinned and tag names (normalized to kebab-case); body may be pre-filled from a loaded template.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id (required when scope=task; must be omitted for project/universal)"), "scope": stringSchema("Comment scope: task (default), project, or universal"), "body": stringSchema("Comment body"), "title": stringSchema("Optional title for the comment"), "kind": stringSchema("Optional comment kind (e.g. \"handoff\", \"recap\", \"standup\")"), "pinned": booleanSchema("Optional; pin the comment to the cover sheet"), "author_type": stringSchema("human or agent"), "tags": arrayStringSchema("Optional tag names to attach to this comment (e.g. [\"resume\", \"deployment-notes\"])"), "template_slug": stringSchema("Optional slug of a loaded template; when set, the template body is merged into the comment (user content first, template appended).")}, []string{"body"})
	}, handler: serviceTool((*agent.Service).AddComment)},
	{name: "comments.list", description: "List comments. With no extra filters this lists the named task's comments (task-scoped, default). Pass comment_id to fetch exactly one comment by id (get-by-id, any scope). Add scope/kind/tag/pinned/query/since to query the filterable handoff log: scope (task|project|universal), kind, tag, pinned (pinned-only), query (FTS5 over body+title), and since (time-window floor, e.g. \"24h\", \"7d\").", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id; narrows task-scoped rows"), "comment_id": integerSchema("Comment id; returns exactly that comment when it belongs to the resolved project, while universal comments remain cross-project"), "scope": stringSchema("Optional scope filter: task, project, or universal"), "kind": stringSchema("Optional comment kind filter"), "tag": stringSchema("Optional tag-name filter"), "pinned": booleanSchema("Optional; return only pinned comments"), "query": ftsQuerySchema(), "since": stringSchema("Optional time-window floor (Go duration \"24h\"/\"30m\" or N-day shorthand \"7d\")")}, nil)
	}, handler: serviceTool((*agent.Service).ListComments)},
	{name: "comments.edit", description: "Edit a comment's body and/or its title/kind/pinned flag, and replace its tags. Every field is a partial update (PATCH semantics): provide at least one of body/title/kind/pinned/tags; the service rejects no-op calls. Omit body to leave it unchanged (metadata-only edit) — a non-null body must be non-empty (you can rewrite but not blank it). Subject to bucket policy (permissions.comment.edit, inherited from permissions.task when not declared).", schema: func() map[string]any {
		return objectSchema(map[string]any{"comment_id": integerSchema("Comment id"), "body": stringSchema("Optional new comment body; omit to leave the body unchanged. A non-null value must be non-empty."), "title": stringSchema("Optional new title"), "kind": stringSchema("Optional new comment kind"), "pinned": booleanSchema("Optional; set the pinned flag"), "tags": arrayStringSchema("Optional tag names; replaces all existing tags on the comment")}, []string{"comment_id"})
	}, handler: serviceTool((*agent.Service).EditComment)},
	{name: "comments.delete", description: "Hard-delete a comment. Subject to bucket policy (permissions.comment.delete, inherited from permissions.task when not declared).", schema: func() map[string]any {
		return objectSchema(map[string]any{"comment_id": integerSchema("Comment id"), "confirmed": booleanSchema("Required true to actually delete the comment")}, []string{"comment_id"})
	}, handler: serviceTool((*agent.Service).DeleteComment)},
	{name: "task_activity.list", description: "Return the unified activity feed for a task: comments and system events (task.created, task.moved, task.completed) ordered chronologically.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id"), "order": stringSchema("Sort order: 'asc' (chronological, default) or 'desc' (newest first)")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).ListTaskActivity)},
	{name: "logs.list", description: "Generic Logs inspector over the unified events log. Returns every event_type — task lifecycle, comments, plans, guards, hooks, tool calls (CLI/MCP/TUI), tricks, audits, and domain bookkeeping — each row carrying a rendered `summary` string so the agent does not have to parse the payload JSON. Default scope is the active project over the configured window (config.views.logs.window_days, 30 days by default). Pass `categories=[\"tool_call\"]` to reproduce the legacy activity-log filter; pass `since=\"24h\"` to narrow the window. Allowed categories: task, comment, plan, tag-dep, guard, audit, hook, tool_call, trick, domain.", schema: logsListSchema, handler: serviceTool((*agent.Service).ListLogs)},
	{name: "dependencies.add", description: "Add a project-scoped task dependency with cycle prevention.", schema: func() map[string]any { return dependencySchema(false) }, handler: serviceTool((*agent.Service).AddDependency)},
	{name: "dependencies.remove", description: "Remove a task dependency after explicit confirmation.", schema: func() map[string]any { return dependencySchema(true) }, handler: serviceTool((*agent.Service).RemoveDependency)},
	{name: "dependencies.list", description: "List dependencies for one task or all active project tasks.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Optional task id; omit or set 0 for all")}, nil)
	}, handler: serviceTool((*agent.Service).ListDependencies)},
	{name: "workflow.show", description: "Show the active workflow buckets and allowed transitions.", schema: selectorSchema, handler: serviceTool((*agent.Service).ShowWorkflow)},
	{name: "orphans.migrate", description: "Detect tasks whose bucket was deactivated by a workflow swap and rebind them to the active workflow (matching key when preserved, first bucket otherwise). First call without confirmed=true returns a preview report plus a Confirmation block listing every affected task; retry with confirmed=true to apply the rebind. Empty preview short-circuits to a no-op.", schema: func() map[string]any {
		return objectSchema(map[string]any{"confirmed": booleanSchema("Required true to apply the rebind; first call returns a preview with affected tasks.")}, nil)
	}, handler: serviceTool((*agent.Service).MigrateOrphans)},
	{name: "progress.record", description: "Record material agent progress through task edits, a progress comment, and optional workflow movement.", schema: progressSchema, handler: serviceTool((*agent.Service).RecordProgress)},
	{name: "tags.add", description: "Add a reusable tag to a task or project. The tag name is normalized to kebab-case and deduplicated automatically.", schema: func() map[string]any { return tagMutationSchema(false) }, handler: serviceTool((*agent.Service).AddTag)},
	{name: "tags.remove", description: "Remove a tag from a task or project after explicit confirmation.", schema: func() map[string]any { return tagMutationSchema(true) }, handler: serviceTool((*agent.Service).RemoveTag)},
	{name: "tags.list", description: "List tags for a specific task or project.", schema: tagListSchema, handler: serviceTool((*agent.Service).ListTags)},
	{name: "tags.list_all", description: "List all tags across all projects with usage counts.", schema: func() map[string]any { return objectSchema(map[string]any{}, nil) }, handler: tagsListAllTool},
	{name: "tags.merge", description: "Merge a source tag into a target tag, reassigning all references and deleting the source.", schema: func() map[string]any {
		return objectSchema(map[string]any{"source_tag_id": integerSchema("Source tag id to merge from (will be deleted)"), "target_tag_id": integerSchema("Target tag id to merge into (canonical)")}, []string{"source_tag_id", "target_tag_id"})
	}, handler: serviceTool((*agent.Service).MergeTags)},
	{name: "errors.record", description: "Record an error encountered during development with optional context and tags. Errors and their solutions are visible cross-project so the agent can reuse prior fixes.", schema: recordErrorSchema, handler: serviceTool((*agent.Service).RecordError)},
	{name: "search", description: "Full-text search across tasks, comments, errors, solutions, and plans using SQLite FTS5. Queries are capped at 4096 UTF-8 bytes and 256 lexical terms/operators before telemetry or SQLite execution. Returns BM25-ranked hits with snippets (<mark>...</mark> highlights). Optional `entity_types` filter restricts the indexed kinds; omit `project`/`project_id` for a cross-project view. Project- and universal-scoped note-like content is returned as `comment`. Archived tasks are filtered out automatically. Replaces the legacy `errors.search` tool — equivalent call: search(query, entity_types=[\"error\"]).", schema: searchSchema, handler: serviceTool((*agent.Service).Search)},
	{name: "solutions.add", description: "Attach a candidate solution to an error. Multiple solutions per error are supported.", schema: addSolutionSchema, handler: serviceTool((*agent.Service).AddSolution)},
	{name: "solutions.confirm", description: "Confirm whether a solution worked. success=true marks it as the recommended fix and increments its like counter; success=false marks it as known-bad so the agent does not retry it without new context.", schema: confirmSolutionSchema, handler: serviceTool((*agent.Service).ConfirmSolution)},
	{name: "solutions.list_top", description: "List the top N most-liked solutions globally (cross-project). Useful to surface validated fixes and audit recurring patterns. Likes are incremented only by solutions.confirm(success=true).", schema: listTopSolutionsSchema, handler: serviceTool((*agent.Service).ListTopSolutions)},
	{name: "templates.list", description: "List every loaded template (slug, name, default kind, project scope, custom flag). Read-only; templates are authored by the user — the agent never modifies template bindings.", schema: func() map[string]any {
		return objectSchema(map[string]any{"kind": stringSchema("Optional default-kind filter (e.g. \"task\")"), "project": stringSchema("Optional project slug to scope project-bound templates"), "include_body": booleanSchema("Set true to include the template body in each entry; default omits it for compact responses")}, nil)
	}, handler: serviceTool((*agent.Service).ListTemplates)},
	{name: "metrics.summary", description: "Aggregate per-AI-model behaviour over a period. Each row carries a `buckets` map keyed by metric tag (`error_recorded`, `error_searched`, `solution_added`, `solution_liked`, `solution_failed`, `solution_top_viewed`) plus `like_rate`, `search_before_record_ratio`, and `session_correlated_sample`. Use to benchmark whether different agents research existing context before recording new errors. Requires that callers pass _agent_model on every tool call (now coercive).", schema: func() map[string]any {
		return objectSchema(map[string]any{"period": stringSchema("Time window: \"7d\", \"30d\" (default), or \"all\""), "project_id": integerSchema("Optional registered project id; omit for cross-project view")}, nil)
	}, handler: serviceTool((*agent.Service).MetricsSummary)},
	{name: "insights.summary", description: "Read-only, consultivo: return the six today-insights for a project (stuck tasks, cycle-time bottleneck, WIP per bucket, guard-violation hotspots, the error loop, and a per-model contrast). The agent self-consults this surface to self-correct (reactive → proactive) — it NEVER moves a task, relaxes a guard, or gates a workflow transition. The output schema is frozen and versioned (`schema_version`, currently 2): a consumer can pin the shape and reject an unknown one. Every sub-report carries an explicit `has_data` flag (no silent zero — empty history is distinguished from a genuine zero reading), and per-model rows carry `sample_size` (the stamped-event count behind the row — the partial-state gate input; the dwell-interval count lives in `dwell_samples`). PRE-COMMITTED FALSIFIABLE HYPOTHESIS: exposing insights.summary to the agent makes guard-violations-per-task drop ≥30% over 2 weeks vs baseline (measured via the guard-hotspot insight).", schema: func() map[string]any {
		return objectSchema(map[string]any{"stuck_days": integerSchema("Optional staleness threshold (days) for the stuck-task scan; omit or 0 takes the service default (7)"), "project_id": integerSchema("Optional registered project id to pin explicitly; omit to scope to the project resolved from cwd (contextual default — global view only when no project resolves)")}, nil)
	}, handler: serviceTool((*agent.Service).InsightsSummary)},
	{name: "templates.show", description: "Return one template by slug, including its full body. Read-only. Hard-rejects (validation_error) when the requested slug is a global template that is shadowed by a project-scoped override in the active project — the rejection's details name the active slug so callers can re-call directly.", schema: showTemplateSchema, handler: serviceTool((*agent.Service).ShowTemplate)},
	{name: "skills.list", description: "List every loaded skill (slug + name + description), ordered by slug. Bodies are omitted — call skills.get for one body. Read-only; skills are authored by the user and the agent never creates, edits, or deletes them through MCP.", schema: func() map[string]any { return objectSchema(map[string]any{}, nil) }, handler: serviceTool((*agent.Service).ListSkills)},
	{name: "skills.get", description: "Return one skill by slug, including its full body. Read-only; there is no mutation counterpart. Rejects (validation_error) when the slug is unknown, naming the missing slug.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Skill slug")}, []string{"slug"})
	}, handler: serviceTool((*agent.Service).ShowSkill)},
	{name: "personas.list", description: "List every persona wired in the active config personas: block (slug + name + description), ordered by slug. Bodies and expanded references are omitted — call personas.get for one persona with laws and skills expanded inline. Read-only.", schema: func() map[string]any { return objectSchema(map[string]any{}, nil) }, handler: serviceTool((*agent.Service).ListPersonas)},
	{name: "personas.get", description: "Return one active-config persona by slug, including its body and every explicitly referenced law and skill expanded inline with full bodies. Read-only. Rejects (validation_error) when the slug is unknown or a referenced law/skill is missing.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Persona slug")}, []string{"slug"})
	}, handler: serviceTool((*agent.Service).ShowPersona)},
	{name: "laws.list", description: "List every loaded law (slug + name + severity + scope), ordered by slug. Bodies are omitted — call laws.get for one body. Read-only.", schema: func() map[string]any { return objectSchema(map[string]any{}, nil) }, handler: serviceTool((*agent.Service).ListLaws)},
	{name: "laws.get", description: "Return one law by slug, including its full body. Read-only. Rejects (validation_error) when the slug is unknown, naming the missing slug.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Law slug")}, []string{"slug"})
	}, handler: serviceTool((*agent.Service).ShowLaw)},
	{name: "plans.create", description: "Create a WBS-style plan that groups child tasks in ordered waves. Slug must be unique within the project; goal_body is markdown describing the plan's intent and acceptance criteria. Emits plan.created.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug (kebab-case recommended); unique per project"), "name": stringSchema("Human-readable plan name"), "goal_body": stringSchema("Optional markdown body describing the plan goal and acceptance criteria")}, []string{"slug", "name"})
	}, handler: serviceTool((*agent.Service).CreatePlan)},
	{name: "plans.list", description: "List every plan in the active project, ordered by creation. Goal bodies are omitted from list entries — call plans.show to fetch one with its full body.", schema: selectorSchema, handler: serviceTool((*agent.Service).ListPlans)},
	{name: "plans.show", description: "Return one plan with its waves, tasks per wave, per-wave and overall done/total counts, integer percent, and the active wave id (lowest-position wave with pending work). Archived tasks are filtered out of the counts but stay in the wave's tasks list.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug")}, []string{"slug"})
	}, handler: serviceTool((*agent.Service).ShowPlan)},
	{name: "plans.add_wave", description: "Append a wave to a plan (position=0 auto-assigns after the current highest position; explicit position>0 inserts at that slot and rejects on collision). Identify the plan by slug or plan_id; supply at least one. Emits plan.wave_added.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)"), "name": stringSchema("Wave name (human-readable)"), "position": integerSchema("Optional 1-based wave position; omit or 0 to append after the current highest")}, []string{"name"})
	}, handler: serviceTool((*agent.Service).AddPlanWave)},
	{name: "plans.assign_task", description: "Attach an existing task to a (plan, wave). Identify the plan by slug or plan_id; supply at least one. Cross-plan / cross-project wave references are rejected.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id to attach"), "slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)"), "wave_id": integerSchema("Wave id; must belong to the named plan")}, []string{"task_id", "wave_id"})
	}, handler: serviceTool((*agent.Service).AssignPlanTask)},
	{name: "plans.claim_next", description: "Atomically reserve the next claimable task in the plan's active wave (lowest-position wave with pending tasks). Claimable means active, unassigned, and still in the workflow's first bucket. Stamps tasks.assigned_to with the caller's _agent_model and emits task.assigned; the bucket is not moved, so callers must use tasks.move separately once preset guards are satisfied. Returns claimed=false (no task) when every wave is fully done or no unassigned first-bucket task remains in the active wave. Concurrency-safe via BEGIN IMMEDIATE on a pinned connection.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)")}, nil)
	}, handler: serviceTool((*agent.Service).ClaimNextPlanTask)},
	{name: "plans.continue", description: "Agent-tailored projection of a plan: returns the same aggregate plans.show emits (full plan + waves + done/total + active wave) plus a non-mutating preview of the task plans.claim_next would reserve next. Use before plans.claim_next so an agent can inspect goal_body, the wave layout, and the candidate task before committing to a claim.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug")}, []string{"slug"})
	}, handler: serviceTool((*agent.Service).ContinuePlan)},
	{name: "plans.edit", description: "Edit a plan's name, slug, status, and/or goal_body. Identify the plan by slug or plan_id; supply at least one editable field. status accepts active / done / abandoned (abandoned co-emits plan.abandoned); a new_slug collision rejects with plan_slug_conflict. Emits plan.edited with the per-field diff (and plan.goal_edited when goal_body changes).", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug to identify the plan (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)"), "name": stringSchema("Optional new plan name"), "new_slug": stringSchema("Optional new plan slug; must stay unique within the project"), "status": stringSchema("Optional new status: active, done, or abandoned"), "goal_body": stringSchema("Optional new markdown goal body")}, nil)
	}, handler: serviceTool((*agent.Service).EditPlan)},
	{name: "plans.delete", description: "Hard-delete a plan. Its waves cascade-delete and member tasks are detached (plan_id / wave_id cleared) but otherwise survive. Identify the plan by slug or plan_id. First call without confirmed=true returns a Confirmation block; retry with confirmed=true to apply. Emits plan.deleted.", schema: func() map[string]any {
		return objectSchema(map[string]any{"slug": stringSchema("Plan slug (alternative to plan_id)"), "plan_id": integerSchema("Plan id (alternative to slug)"), "confirmed": booleanSchema("Required true to actually delete the plan")}, nil)
	}, handler: serviceTool((*agent.Service).DeletePlan)},
	{name: "plans.remove_wave", description: "Delete a wave from a plan. Its tasks survive with wave_id cleared (plan_id intact, so they stay in the plan but unscheduled). First call without confirmed=true returns a Confirmation block; retry with confirmed=true to apply. Emits plan.wave_removed.", schema: func() map[string]any {
		return objectSchema(map[string]any{"wave_id": integerSchema("Wave id to delete"), "confirmed": booleanSchema("Required true to actually remove the wave")}, []string{"wave_id"})
	}, handler: serviceTool((*agent.Service).RemovePlanWave)},
	{name: "plans.rename_wave", description: "Rename a wave. The new name must be non-blank and differ from the current name. Emits plan.wave_renamed.", schema: func() map[string]any {
		return objectSchema(map[string]any{"wave_id": integerSchema("Wave id to rename"), "name": stringSchema("New wave name")}, []string{"wave_id", "name"})
	}, handler: serviceTool((*agent.Service).RenamePlanWave)},
	{name: "plans.reorder_wave", description: "Move a wave to a new 1-based position within its plan. A collision with an occupied slot swaps the two waves. Emits plan.wave_reordered.", schema: func() map[string]any {
		return objectSchema(map[string]any{"wave_id": integerSchema("Wave id to move"), "position": integerSchema("New 1-based position")}, []string{"wave_id", "position"})
	}, handler: serviceTool((*agent.Service).ReorderPlanWave)},
	{name: "plans.unassign", description: "Detach a task from its plan, clearing both plan_id and wave_id (full detach; the task becomes a standalone work item again). A task already unattached is a no-op. Emits plan.task_unassigned.", schema: func() map[string]any {
		return objectSchema(map[string]any{"task_id": integerSchema("Task id to detach")}, []string{"task_id"})
	}, handler: serviceTool((*agent.Service).UnassignPlanTask)},
	{name: "commands.list", description: "List every agent-callable okt-* command (slug + entity-sourced description + arguments), mirroring the prompts/list surface. Use to discover the playbook catalog from the tool-list when no human is present to type a slash command (loop / subagent / Workflow), then fetch one with commands.resolve.", schema: func() map[string]any { return objectSchema(map[string]any{}, nil) }, handler: commandsListTool},
	{name: "commands.resolve", description: "Resolve an okt-* command to its composed playbook markdown (persona + invocation args + skills + laws + templates) via the same path the /mcp__omakiten__okt-* slash prompt uses — byte-identical output. This is the agent-callable twin of the prompt surface: it lets a loop / subagent / Workflow run a playbook (okt-audit, okt-run, okt-task-*, …) without a human typing the slash. Render-only; mutates nothing — the agent reads the returned markdown and acts.", schema: commandResolveSchema, handler: commandsResolveTool},
})
