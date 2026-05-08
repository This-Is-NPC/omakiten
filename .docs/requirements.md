# Implemented Requirements

## Functional Requirements

| ID | Description | Source files |
|----|-------------|-------------|
| FR-001 | Initialize/register a project by root path in the global SQLite database | `internal/cli/init.go`, `internal/app/project_service.go`, `internal/sqlite/projects.go` |
| FR-002 | Create tasks with title, description, bucket, and priority (default-bucket policy resolves the first bucket of the active workflow) | `internal/cli/add.go`, `internal/app/workflow_service.go:CreateTask`, `internal/sqlite/tasks.go:CreateTask` |
| FR-003 | List tasks with optional bucket filter, sort field, and order | `internal/cli/list.go`, `internal/app/task_service.go`, `internal/sqlite/tasks.go:ListTasks` |
| FR-004 | Move tasks between workflow buckets enforcing transition allowance + guards; emits `task.completed` when moving into the workflow's final bucket | `internal/cli/move.go`, `internal/app/workflow_service.go:MoveTask`, `internal/sqlite/tasks.go:MoveTask`, `internal/sqlite/workflows.go`, `internal/sqlite/guards.go` |
| FR-005 | Edit task title, description, priority, and bucket | `internal/cli/edit.go`, `internal/app/task_service.go`, `internal/sqlite/tasks.go:UpdateTask` |
| FR-006 | Add comments to tasks (with optional tags) | `internal/cli/comment.go`, `internal/app/comment_service.go`, `internal/sqlite/comments.go` |
| FR-007 | List comments for a task | `internal/cli/comment.go`, `internal/app/comment_service.go`, `internal/sqlite/comments.go` |
| FR-008 | Add task dependencies with cycle detection and project-scope check | `internal/cli/depend.go`, `internal/app/dependency_service.go`, `internal/sqlite/dependencies.go`, `internal/graph/dependency.go` |
| FR-009 | Remove task dependencies | `internal/cli/depend.go`, `internal/app/dependency_service.go`, `internal/sqlite/dependencies.go` |
| FR-010 | List task dependencies | `internal/cli/depend.go`, `internal/app/dependency_service.go`, `internal/sqlite/dependencies.go` |
| FR-011 | Add context entries with token estimation | `internal/cli/context.go`, `internal/app/context_service.go`, `internal/sqlite/contexts.go` |
| FR-012 | Dump progressive context (levels 1–3) under a configurable token budget | `internal/cli/context.go`, `internal/app/context_service.go` |
| FR-013 | Show active workflow configuration | `internal/cli/workflow.go`, `internal/sqlite/workflows.go:ActiveWorkflow` |
| FR-014 | Validate `omakiten.yaml` configuration | `internal/cli/config.go`, `internal/config/validator.go`, `internal/config/loader.go` |
| FR-015 | List, show, add, edit, and remove **skills** as file-backed markdown entities | `internal/cli/skill.go`, `internal/app/skill_service.go`, `internal/app/bundle_editor.go`, `internal/configstore/configstore.go` |
| FR-016 | List, show, add, edit, and remove **personas** as file-backed markdown entities (with skill references) | `internal/cli/persona.go`, `internal/app/persona_service.go`, `internal/app/bundle_editor.go`, `internal/sqlite/personas.go` |
| FR-017 | List, show, add, edit, and remove **laws** as file-backed markdown entities (scoped global / persona / project) | `internal/cli/law.go`, `internal/app/law_service.go`, `internal/app/bundle_editor.go`, `internal/config/loader_pick.go` |
| FR-018 | Launch TUI with three top-level zones (Tasks / Stats / Settings) plus a multi-project Home sentinel; per-zone sub-menus (Tasks: board / table / graph; Stats: general / logs; Settings: general / laws / personas / skills / templates / tags); session back-stack via `ctrl+o` | `internal/cli/tui.go`, `internal/tui/state.go` (`topID` / `subID` / `topOrder` / `subsByTop` / `viewHistory`), `internal/tui/model.go`, `internal/tui/render_board.go`, `internal/tui/render_table.go`, `internal/tui/render_graph.go`, `internal/tui/render_settings_general.go`, `internal/tui/render_config.go`, `internal/tui/render_stats.go`, `internal/tui/render_logs.go`, `internal/tui/render_chrome.go` |
| FR-019 | Resolve active project via `--project-id`, `--project`, or current working directory | `internal/cli/root.go:resolveProject`, `internal/project/resolver.go` |
| FR-020 | Import YAML bundle into SQLite as a materialized read model | `internal/app/config_service.go`, `internal/sqlite/bundles.go` |
| FR-021 | Materialize embedded default kit assets on first run (laws, skills, personas, templates, themes, omakiten.yaml) | `internal/configstore/configstore.go:EnsureDefaultFiles`, `defaults/defaults.go` |
| FR-022 | Migrate legacy config layouts forward when older directory shapes are detected | `internal/configstore/configstore.go:MigrateLayout`, `internal/config/migration.go` |
| FR-023 | Expose MCP tool `project.overview`: identity, workflow awareness, pending count, recent context, next-step prompt | `internal/agent/service_project.go:Overview`, `internal/mcp/adapter.go` |
| FR-024 | Expose MCP tool `project.resume`: distribution, likely next work, blocked work, dependencies, recent context | `internal/agent/service_project.go:ResumeProject`, `internal/mcp/adapter.go` |
| FR-025 | Expose MCP tool `tasks.continue`: task + dependencies + comments + workflow + context | `internal/agent/service_tasks.go:ContinueTask`, `internal/mcp/adapter.go` |
| FR-026 | Expose MCP tool `tasks.list`: list active project tasks with optional bucket filter | `internal/agent/service_tasks.go:ListTasks`, `internal/mcp/adapter.go` |
| FR-027 | Expose MCP tool `tasks.create_intent`: create with similar/related-task detection and confirmation gate | `internal/agent/service_tasks.go:CreateTaskIntent`, `internal/mcp/adapter.go` |
| FR-028 | Expose MCP tool `tasks.create`: direct task creation equivalent to `okt add` | `internal/agent/service_tasks.go:CreateTask`, `internal/mcp/adapter.go` |
| FR-029 | Expose MCP tool `tasks.move`: move via allowed workflow transition | `internal/agent/service_tasks.go:MoveTask`, `internal/mcp/adapter.go` |
| FR-030 | Expose MCP tool `comments.add` with optional tag attachment | `internal/agent/service_comments.go:AddComment`, `internal/mcp/adapter.go` |
| FR-031 | Expose MCP tool `comments.list` | `internal/agent/service_comments.go:ListComments`, `internal/mcp/adapter.go` |
| FR-032 | Expose MCP tool `task_activity.list`: unified chronological feed (comments + system events) for a task | `internal/agent/service_comments.go:ListTaskActivity`, `internal/sqlite/events.go:ListTaskActivity`, `internal/mcp/adapter.go` |
| FR-033 | Expose MCP tool `dependencies.add`: project-scoped, cycle-prevented | `internal/agent/service_dependencies.go:AddDependency`, `internal/mcp/adapter.go` |
| FR-034 | Expose MCP tool `dependencies.remove`: requires explicit `confirmed=true` | `internal/agent/service_dependencies.go:RemoveDependency`, `internal/mcp/adapter.go` |
| FR-035 | Expose MCP tool `dependencies.list`: per-task or all-tasks | `internal/agent/service_dependencies.go:ListDependencies`, `internal/mcp/adapter.go` |
| FR-036 | Expose MCP tool `context.add`: project handoff entry | `internal/agent/service_context.go:AddContext`, `internal/mcp/adapter.go` |
| FR-037 | Expose MCP tool `context.dump`: levels 1–3 with token budget | `internal/agent/service_context.go:DumpContext`, `internal/mcp/adapter.go` |
| FR-038 | Expose MCP tool `workflow.show`: buckets + allowed transitions | `internal/agent/service_workflow.go:ShowWorkflow`, `internal/mcp/adapter.go` |
| FR-039 | Expose MCP tool `progress.record`: edits + comments + context + optional move in one call | `internal/agent/service_progress.go:RecordProgress`, `internal/mcp/adapter.go` |
| FR-040 | Expose MCP tools `tags.add`, `tags.remove`, `tags.list`, `tags.list_all`, `tags.merge` for task/project tag management | `internal/agent/service_tags.go`, `internal/sqlite/tags.go`, `internal/mcp/adapter.go` |
| FR-041 | Expose MCP tools `errors.record` and `errors.search`: cross-project error log with tag/text search | `internal/agent/service_errors.go`, `internal/sqlite/errors.go`, `internal/mcp/adapter.go` |
| FR-042 | Expose MCP tools `solutions.add`, `solutions.confirm`, `solutions.list_top`: candidate fixes per error with success likes | `internal/agent/service_errors.go`, `internal/sqlite/errors.go`, `internal/mcp/adapter.go` |
| FR-043 | Expose MCP tools `templates.list` and `templates.show`: read-only template catalog. `templates.show` performs strict shadow validation — when an active project resolves and the requested slug is a global template that the project shadows with a same-kind override, the call hard-rejects with `validation_error` naming the active slug; an explicit unresolvable `project`/`project_id` propagates `project_not_found` instead of falling back. | `internal/agent/service_templates.go`, `internal/mcp/adapter.go` |
| FR-044 | Expose MCP resources `omakiten://project/overview` and `omakiten://workflow/active` | `internal/mcp/adapter.go:ReadResource` |
| FR-045 | Expose MCP prompts `okt`, `okt-imagine`, `okt-create`, `okt-resume`, `okt-continue`, `okt-implement`, `okt-document`, `okt-config` | `internal/agent/service_command.go:CommandNames`, `internal/mcp/adapter.go:ListPrompts`, `internal/mcp/adapter.go:GetPrompt` |
| FR-046 | Serve MCP stdio server handling JSON-RPC 2.0 requests | `internal/mcp/server.go:Serve`, `internal/cli/mcp.go` |
| FR-047 | Call MCP tools from CLI (`okt mcp call TOOL_NAME --input JSON`) | `internal/cli/mcp.go` |
| FR-048 | List MCP tool/resource/prompt definitions from CLI (`okt mcp tools`) | `internal/cli/mcp.go` |
| FR-049 | Set up MCP harness config (`claude-code`, `claude-desktop`, `opencode`) with dry-run, force, and custom config-path support | `internal/cli/init.go`, `internal/cli/mcp.go`, `internal/agentsetup/setup.go` |
| FR-050 | View recent activity logs in TUI with source, operation, duration, and status, scoped to the active project; topped by Status / Sources summary tables that aggregate the full project history | `internal/tui/render_logs.go`, `internal/sqlite/activity_logs.go:ListActivityLogs`, `internal/sqlite/activity_logs.go:ActivityLogStats` |
| FR-051 | View per-task unified activity feed in TUI (comments + workflow events) | `internal/tui/render_activity.go`, `internal/sqlite/events.go:ListTaskActivity` |
| FR-052 | Expose MCP tool `metrics.summary`: per-AI-model benchmark over a period (errors recorded/searched, solutions added, like rate, search-before-record ratio); supports `7d`/`30d`/`all` and an optional `project_id` filter | `internal/agent/service_metrics.go`, `internal/app/metrics_service.go`, `internal/sqlite/metrics.go`, `internal/mcp/adapter.go` |
| FR-053 | TUI Stats › General sub renders the per-AI-model benchmark inline with a period cycler (`7d`/`30d`/`all`), beneath the project's `Totals` (tasks / comments / context entries / tags) and `Tokens` (estimated / max + budget badge) bordered tables | `internal/tui/render_stats.go`, `internal/tui/model.go:refreshStats`, `internal/tui/state.go` (`subStatsGeneral`) |
| FR-054 | Emit domain events from `app.ErrorService` (`error.recorded`, `error.searched`, `solution.added`, `solution.liked`, `solution.failed`, `solution.viewed_top`) into the unified `events` table for benchmark reconstruction | `internal/app/error_service.go`, `internal/sqlite/events.go:RecordEntityEvent`, `internal/domain/event.go` |

## Non-Functional Requirements

| ID | Description | Source files |
|----|-------------|-------------|
| NFR-001 | All CLI commands emit a single line of minified JSON output through a shared envelope | `internal/output/json.go`, `internal/cli/root.go:writeSuccess`, `internal/cli/root.go:writeError` |
| NFR-002 | Agent-recoverable errors carry stable coded strings | `internal/domain/errors.go`, `internal/cli/root.go:writeError`, `internal/agent/errors.go` |
| NFR-003 | SQLite foreign keys and busy timeouts are enabled on every connection | `internal/sqlite/store.go:Open` |
| NFR-004 | Config file mutations are atomic (temp file + rename) | `internal/config/atomic.go`, `internal/configstore/configstore.go:WriteAtomic` |
| NFR-005 | Bundle editor uses snapshot journals for rollback on partial failure | `internal/app/bundle_editor.go` |
| NFR-006 | Context dump respects a configurable max token budget and truncates gracefully | `internal/app/context_service.go`, `internal/domain/context.go` |
| NFR-007 | Operational data is strictly project-scoped at the repository layer | `internal/sqlite/tasks.go`, `internal/sqlite/comments.go`, `internal/sqlite/dependencies.go`, `internal/sqlite/contexts.go`, `internal/sqlite/tags.go` (every query filters `project_id`) |
| NFR-008 | YAML loading uses strict field validation (`KnownFields(true)`) | `internal/config/loader.go`, `internal/config/entity_loader.go` |
| NFR-009 | Pure-Go build without CGo via `modernc.org/sqlite` | `go.mod`, `internal/sqlite/store.go` |
| NFR-010 | XDG Base Directory specification compliance for config and data paths | `internal/paths/paths.go` |
| NFR-011 | Embedded default kit is materialized on first run | `defaults/defaults.go`, `internal/configstore/configstore.go:EnsureDefaultFiles` |
| NFR-012 | Schema migrations applied transactionally with version tracking | `internal/sqlite/store.go`, `migrations/migrations.go`, `migrations/001_initial.sql`–`migrations/009_events.sql` |
| NFR-013 | Agent intent layer is protocol-neutral: no MCP SDK, package manager, or transport dependency (verified by `internal/arch/arch_test.go` and `depguard`) | `internal/agent/service.go` (imports only `internal/app`, `internal/domain`, `internal/project`, `internal/token`) |
| NFR-014 | Agent error mapping turns domain coded errors into compact failures with next-step guidance | `internal/agent/errors.go` |
| NFR-015 | Ambiguous or destructive agent intents return `requires_confirmation` instead of mutating state | `internal/agent/service_tasks.go:CreateTaskIntent`, `internal/agent/service_dependencies.go:RemoveDependency`, `internal/agent/service_tags.go` |
| NFR-016 | MCP harness setup preserves existing config entries and refuses silent overwrites | `internal/agentsetup/setup.go` |
| NFR-017 | App service calls are tracked as activity logs with synchronous pruning (max 500 rows, 7 days) | `internal/activity/track.go`, `internal/sqlite/activity_logs.go` (`activityLogMaxRows = 500`, `activityLogMaxAgeDays = 7`) |
| NFR-018 | Activity log failures must not break business logic | `internal/activity/track.go` |
| NFR-019 | Hexagonal boundaries are enforced by an architecture test and mirrored as `depguard` lint rules | `internal/arch/arch_test.go`, `.golangci.yml`, `internal/app/doc.go` |
| NFR-020 | Agent attribution (`source`, `entrypoint`, `agent_model`, `agent_session_id`) flows through the request context (`internal/activity`) and is denormalized on every write into `events`, `errors`, and `solutions` so per-model benchmarks need no joins | `internal/activity/context.go`, `internal/sqlite/errors.go:agentAttribution`, `internal/sqlite/events.go`, `internal/sqlite/activity_logs.go`, `migrations/010_agent_attribution.sql` |

## Business Rules

| ID | Rule | Source files |
|----|------|-------------|
| BR-001 | Workflow transitions must be explicitly defined; arbitrary bucket moves are blocked | `internal/app/workflow_service.go:MoveTask`, `internal/sqlite/workflows.go:TransitionAllowed` |
| BR-002 | Workflow guards (e.g., comment-tag presence, blocker buckets) must be satisfied before a move | `internal/app/workflow_service.go`, `internal/sqlite/guards.go`, `internal/sqlite/comments.go` |
| BR-003 | Task dependencies cannot form cycles | `internal/graph/dependency.go:HasCycle`, `internal/app/dependency_service.go` |
| BR-004 | Task dependencies cannot cross projects | `internal/sqlite/dependencies.go:AddTaskDependency`, `internal/app/dependency_service.go` |
| BR-005 | Entity slugs are immutable — renaming requires delete and re-add | `internal/app/bundle_editor.go`, `internal/config/saver.go` |
| BR-006 | Law scope (`global` / `persona` / `project`) is determined by where the slug is referenced in the wiring file | `internal/config/loader_pick.go`, `internal/config/loader_refs.go` |
| BR-007 | Context dump level 1 includes context entries (under the token budget) | `internal/app/context_service.go` |
| BR-008 | Level 2 additionally includes the active workflow, tasks, and dependencies | `internal/app/context_service.go` |
| BR-009 | Level 3 additionally includes comments and active laws | `internal/app/context_service.go` |
| BR-010 | Context content is added newest-first and stops as soon as the budget is exhausted (`Truncated` flag set) | `internal/app/context_service.go` (`contextBudget`) |
| BR-011 | Persona skills are resolved by slug reference and linked via a junction table | `internal/sqlite/personas.go`, `internal/sqlite/bundles.go` |
| BR-012 | The active workflow is determined by the `workflow.active` setting matching a workflow key | `internal/sqlite/workflows.go:ActiveWorkflow` |
| BR-013 | The default bucket for new tasks is the first bucket of the active workflow when no bucket is specified (no hard-coded `backlog`) | `internal/app/workflow_service.go:ResolveDefaultBucket` |
| BR-014 | Task priority is restricted to `low`, `normal`, or `high` | `internal/domain/task.go`, `internal/config/validator.go` (`allowedPriorities`) |
| BR-015 | Task creation via `tasks.create_intent` requires confirmation when similar or related work already exists | `internal/agent/service_tasks.go:CreateTaskIntent` |
| BR-016 | Dependency removal via agent intent requires explicit `confirmed=true` | `internal/agent/service_dependencies.go:RemoveDependency` |
| BR-017 | Tag removal via agent intent requires explicit `confirmed=true` | `internal/agent/service_tags.go` |
| BR-018 | Agent intents resolve project via standard precedence: explicit `project_id`, explicit `project` slug, then current working directory | `internal/agent/service.go` (`ProjectSelector`), `internal/project/resolver.go` |
| BR-019 | Agent intents must never mix data from different projects | `internal/agent/service_test.go` (e.g., `TestOverviewUsesResolvedProjectAndCompactState`, `TestResumeProjectDoesNotMixProjectState`) |
| BR-020 | Activity logs are pruned synchronously after each insert to cap storage (max 500 rows, 7 days) | `internal/sqlite/activity_logs.go:BeginActivityLog`, `internal/sqlite/activity_logs.go:PruneActivityLogs` |
| BR-021 | Tag names are normalized to kebab-case and deduplicated on creation | `internal/app/tag_normalization.go`, `internal/app/tag_service.go`, `internal/sqlite/tags.go:FindOrCreateTag` |
| BR-022 | Errors and solutions are cross-project (visible globally) so the agent can reuse prior fixes; only `solutions.confirm(success=true)` increments a solution's like counter | `internal/agent/service_errors.go`, `internal/sqlite/errors.go` |
| BR-023 | The MCP-supported harnesses are exactly `claude-code`, `claude-desktop`, and `opencode`; unsupported values are rejected | `internal/agentsetup/setup.go` (`SupportedHarnesses`) |
| BR-024 | Every MCP tool call must carry a non-empty top-level `_agent_model`; the adapter rejects calls missing it with `validation_error` (system-internal `ReadResource` is exempt and writes empty attribution) | `internal/mcp/adapter.go:extractAgentAttribution`, `internal/mcp/adapter.go:CallTool` |
| BR-025 | `metrics.summary` excludes rows with empty `agent_model` (TUI human, system internals); `search_before_record_ratio` is computed only over records carrying a non-empty `agent_session_id` (correlating searches across parallel agents would inflate the ratio) | `internal/sqlite/metrics.go:AgentMetricsSummary`, `internal/sqlite/metrics.go:fillSearchBeforeRecord` |
| BR-026 | `templates.show` rejects with `validation_error` when the requested slug is a global template shadowed by a project-scoped override of the same `default` kind in the active project; the rejection's `details` carry `requested_slug`, `active_slug`, and `project` so callers re-call directly. Calls outside any registered project preserve the legacy slug-only lookup; an explicit `project`/`project_id` that does not resolve propagates `project_not_found`. | `internal/agent/service_templates.go:ShowTemplate`, `internal/agent/template_test.go` |
