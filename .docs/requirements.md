# Implemented Requirements

## Functional Requirements

| ID | Description | Source files |
|----|-------------|-------------|
| FR-001 | Initialize/register a project by root path in the global SQLite database | `internal/cli/init.go`, `internal/app/project_service.go`, `internal/sqlite/store.go` |
| FR-002 | Create tasks with title, description, bucket, and priority | `internal/cli/add.go`, `internal/app/task_service.go`, `internal/sqlite/store.go:CreateTask` |
| FR-003 | List tasks with optional bucket filter | `internal/cli/list.go`, `internal/app/task_service.go`, `internal/sqlite/store.go:ListTasks` |
| FR-004 | Move tasks between workflow buckets with transition validation | `internal/cli/move.go`, `internal/app/task_service.go`, `internal/sqlite/store.go:MoveTask` |
| FR-005 | Edit task title, description, priority, and bucket | `internal/cli/edit.go`, `internal/app/task_service.go`, `internal/sqlite/store.go:UpdateTask` |
| FR-006 | Add comments to tasks | `internal/cli/comment.go`, `internal/app/comment_service.go`, `internal/sqlite/store.go:AddComment` |
| FR-007 | List comments for a task or project | `internal/cli/comment.go`, `internal/app/comment_service.go`, `internal/sqlite/store.go:ListComments` |
| FR-008 | Add task dependencies | `internal/cli/depend.go`, `internal/app/dependency_service.go`, `internal/sqlite/store.go:AddTaskDependency` |
| FR-009 | Remove task dependencies | `internal/cli/depend.go`, `internal/app/dependency_service.go`, `internal/sqlite/store.go:RemoveTaskDependency` |
| FR-010 | List task dependencies | `internal/cli/depend.go`, `internal/app/dependency_service.go`, `internal/sqlite/store.go:ListTaskDependencies` |
| FR-011 | Add context entries with token estimation | `internal/cli/context.go`, `internal/app/context_service.go`, `internal/sqlite/store.go:AddContextEntry` |
| FR-012 | Dump progressive context (levels 1–3) with token budgeting | `internal/cli/context.go`, `internal/app/context_service.go` |
| FR-013 | Show active workflow configuration | `internal/cli/workflow.go`, `internal/sqlite/store.go:ActiveWorkflow` |
| FR-014 | Validate `omakiten.yaml` configuration | `internal/cli/config.go`, `internal/config/validator.go`, `internal/config/loader.go` |
| FR-015 | List skills | `internal/cli/skill.go`, `internal/app/skill_service.go`, `internal/sqlite/store.go:ListActiveSkills` |
| FR-016 | Show skill details | `internal/cli/skill.go`, `internal/app/skill_service.go` |
| FR-017 | Add, edit, and remove skills as file-backed markdown entities | `internal/cli/skill.go`, `internal/app/skill_service.go`, `internal/app/bundle_editor.go`, `internal/config/saver.go` |
| FR-018 | List personas | `internal/cli/persona.go`, `internal/app/persona_service.go`, `internal/sqlite/store.go:ListActivePersonas` |
| FR-019 | Show persona details | `internal/cli/persona.go`, `internal/app/persona_service.go` |
| FR-020 | Add, edit, and remove personas as file-backed markdown entities | `internal/cli/persona.go`, `internal/app/persona_service.go`, `internal/app/bundle_editor.go`, `internal/config/saver.go` |
| FR-021 | List laws | `internal/cli/law.go`, `internal/app/law_service.go`, `internal/sqlite/store.go:ListActiveLaws` |
| FR-022 | Show law details | `internal/cli/law.go`, `internal/app/law_service.go` |
| FR-023 | Add, edit, and remove laws as file-backed markdown entities | `internal/cli/law.go`, `internal/app/law_service.go`, `internal/app/bundle_editor.go`, `internal/config/saver.go` |
| FR-024 | Launch TUI with kanban board, table view, dependency graph view, and config entity browser | `internal/cli/tui.go`, `internal/tui/model.go`, `internal/tui/entity.go` |
| FR-025 | Resolve active project via `--project-id`, `--project`, or current working directory | `internal/cli/root.go`, `internal/project/resolver.go` |
| FR-026 | Import YAML bundle into SQLite as a materialized read model | `internal/app/config_service.go`, `internal/sqlite/store.go:ImportBundle` |
| FR-027 | Materialize embedded default config files on first run | `internal/config/loader.go:EnsureDefaultFiles`, `defaults/defaults.go` |
| FR-028 | Expose agent intent `project.overview`: active project identity, workflow, pending count, recent context, next-step prompt | `internal/agent/service.go:Overview`, `internal/mcp/adapter.go` |
| FR-029 | Expose agent intent `project.resume`: project distribution, likely next work, blocked/dependent work, recent context | `internal/agent/service.go:ResumeProject`, `internal/mcp/adapter.go` |
| FR-030 | Expose agent intent `tasks.continue`: task details, dependencies, comments, workflow, context | `internal/agent/service.go:ContinueTask`, `internal/mcp/adapter.go` |
| FR-031 | Expose agent intent `tasks.list`: list active project tasks with optional bucket filtering | `internal/agent/service.go:ListTasks`, `internal/mcp/adapter.go` |
| FR-032 | Expose agent intent `tasks.create_intent`: create task with similar-task detection and confirmation | `internal/agent/service.go:CreateTaskIntent`, `internal/mcp/adapter.go` |
| FR-033 | Expose agent intent `tasks.create`: direct task creation equivalent to `okt add` | `internal/agent/service.go:CreateTask`, `internal/mcp/adapter.go` |
| FR-034 | Expose agent intent `tasks.move`: move task through allowed workflow transitions | `internal/agent/service.go:MoveTask`, `internal/mcp/adapter.go` |
| FR-035 | Expose agent intent `comments.add`: add human or agent comment to task | `internal/agent/service.go:AddComment`, `internal/mcp/adapter.go` |
| FR-036 | Expose agent intent `comments.list`: list task comments | `internal/agent/service.go:ListComments`, `internal/mcp/adapter.go` |
| FR-037 | Expose agent intent `dependencies.add`: add project-scoped dependency with cycle prevention | `internal/agent/service.go:AddDependency`, `internal/mcp/adapter.go` |
| FR-038 | Expose agent intent `dependencies.remove`: remove task dependency with explicit confirmation | `internal/agent/service.go:RemoveDependency`, `internal/mcp/adapter.go` |
| FR-039 | Expose agent intent `dependencies.list`: list dependencies for one or all tasks | `internal/agent/service.go:ListDependencies`, `internal/mcp/adapter.go` |
| FR-040 | Expose agent intent `context.add`: add project handoff context entry | `internal/agent/service.go:AddContext`, `internal/mcp/adapter.go` |
| FR-041 | Expose agent intent `context.dump`: dump compact context at level 1, 2, or 3 | `internal/agent/service.go:DumpContext`, `internal/mcp/adapter.go` |
| FR-042 | Expose agent intent `workflow.show`: show active workflow buckets and transitions | `internal/agent/service.go:ShowWorkflow`, `internal/mcp/adapter.go` |
| FR-043 | Expose agent intent `progress.record`: record material progress through edits, comments, context, and optional workflow moves | `internal/agent/service.go:RecordProgress`, `internal/mcp/adapter.go` |
| FR-044 | Expose MCP resources: `omakiten://project/overview` and `omakiten://workflow/active` | `internal/mcp/adapter.go:ReadResource` |
| FR-045 | Expose MCP prompts: `okt`, `okt-create`, `okt-continue`, `okt-resume` | `internal/mcp/adapter.go:GetPrompt` |
| FR-046 | Serve MCP stdio server handling JSON-RPC 2.0 requests | `internal/mcp/server.go:Serve`, `internal/cli/mcp.go:newMCPServeCommand` |
| FR-047 | Call MCP tools directly from CLI (`okt mcp call TOOL_NAME --input JSON`) | `internal/cli/mcp.go:newMCPCallCommand` |
| FR-048 | List MCP tool/resource/prompt definitions from CLI (`okt mcp tools`) | `internal/cli/mcp.go:newMCPToolsCommand` |
| FR-049 | Set up MCP harness config from `okt init --enable-mcp` with dry-run, force, and custom config path support | `internal/cli/init.go`, `internal/agentsetup/setup.go` |

## Non-Functional Requirements

| ID | Description | Source files |
|----|-------------|-------------|
| NFR-001 | All CLI commands emit a single line of minified JSON output | `internal/output/json.go`, `internal/cli/root.go:writeSuccess`, `internal/cli/root.go:writeError` |
| NFR-002 | Agent-recoverable errors use stable coded error strings | `internal/domain/errors.go`, `internal/cli/root.go:writeError` |
| NFR-003 | SQLite foreign keys and busy timeouts are enabled on every connection | `internal/sqlite/store.go:Open` |
| NFR-004 | Config file mutations are atomic (temp file + rename) | `internal/config/loader.go:WriteAtomic`, `internal/config/saver.go` |
| NFR-005 | Bundle editor uses snapshot journals for rollback on failure | `internal/app/bundle_editor.go` |
| NFR-006 | Context dump respects a configurable max token budget and truncates gracefully | `internal/app/context_service.go`, `internal/domain/context.go` |
| NFR-007 | Operational data is strictly project-scoped at the repository layer | `internal/sqlite/store.go` (all queries filter by `project_id`) |
| NFR-008 | YAML loading uses strict field validation (`KnownFields(true)`) | `internal/config/loader.go:readWiring`, `internal/config/entity_loader.go` |
| NFR-009 | Pure Go build without CGo via `modernc.org/sqlite` | `go.mod`, `internal/sqlite/store.go` |
| NFR-010 | XDG Base Directory specification compliance for config and data paths | `internal/paths/paths.go` |
| NFR-011 | Embedded default kit is materialized on first run | `defaults/defaults.go`, `internal/config/loader.go:EnsureDefaultFiles` |
| NFR-012 | Schema migrations are applied transactionally with version tracking | `internal/sqlite/store.go:applyMigrations`, `migrations/migrations.go` |
| NFR-013 | Agent intent layer is protocol-neutral: no MCP SDK, package manager, or transport dependency | `internal/agent/service.go` (imports only `internal/app`, `internal/domain`, `internal/project`, `internal/token`, `internal/config`) |
| NFR-014 | MCP adapter maps domain errors to compact coded failures with next-step guidance | `internal/agent/errors.go:FailureFromError`, `internal/agent/errors.go:guidanceForCode` |
| NFR-015 | Ambiguous or destructive agent intents return `requires_confirmation` instead of mutating state | `internal/agent/service.go:CreateTaskIntent`, `internal/agent/service.go:RemoveDependency` |
| NFR-016 | MCP harness setup preserves existing config entries and refuses silent overwrites | `internal/agentsetup/setup.go:Setup` |

## Business Rules

| ID | Rule | Source files |
|----|------|-------------|
| BR-001 | Workflow transitions must be explicitly defined; arbitrary bucket moves are blocked | `internal/sqlite/store.go:MoveTask`, `internal/sqlite/store.go:transitionAllowed` |
| BR-002 | Task dependencies cannot form cycles | `internal/graph/dependency.go:HasCycle`, `internal/app/dependency_service.go` |
| BR-003 | Task dependencies cannot cross projects | `internal/sqlite/store.go:AddTaskDependency`, `internal/app/dependency_service.go` |
| BR-004 | Entity slugs are immutable — renaming requires delete and re-add | `internal/app/bundle_editor.go`, `internal/config/saver.go` |
| BR-005 | Law scope (`global`/`persona`/`project`) is determined by where the slug is referenced in the wiring file | `internal/config/loader.go:pickLaws` |
| BR-006 | Context dump level 1 includes project info, token metrics, workflow, and laws | `internal/app/context_service.go` |
| BR-007 | Context dump level 2 adds tasks, dependencies, and comments | `internal/app/context_service.go` |
| BR-008 | Context dump level 3 adds context entries | `internal/app/context_service.go` |
| BR-009 | Context entries are truncated newest-first when the token budget is exceeded | `internal/app/context_service.go` |
| BR-010 | Persona skills are resolved by slug reference and linked via a junction table | `internal/sqlite/store.go:importPersonas`, `internal/sqlite/store.go:personaSkills` |
| BR-011 | The active workflow is determined by the `workflow.active` setting value matching a workflow key | `internal/sqlite/store.go:ActiveWorkflow` |
| BR-012 | The default bucket for new tasks is `backlog` when no bucket is specified | `internal/sqlite/store.go:CreateTask` |
| BR-013 | Task priority is restricted to `low`, `normal`, or `high` | `internal/domain/task.go:Priority`, `internal/config/validator.go` |
| BR-014 | Task creation via agent intent requires confirmation when similar or related work already exists | `internal/agent/service.go:CreateTaskIntent`, `internal/agent/service.go:similarTasks` |
| BR-015 | Dependency removal via agent intent requires explicit `confirmed=true` | `internal/agent/service.go:RemoveDependency` |
| BR-016 | Agent intents resolve project via standard precedence: explicit `project_id`, explicit `project` slug, then current working directory | `internal/agent/service.go:resolveProject` |
| BR-017 | Agent intents must never mix data from different projects | `internal/agent/service_test.go:TestOverviewUsesResolvedProjectAndCompactState`, `internal/agent/service_test.go:TestResumeProjectDoesNotMixProjectState` |
