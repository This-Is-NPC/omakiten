# MCP Agent Surface

Omakiten exposes a protocol-neutral agent intent layer in `internal/agent` and an MCP adapter in `internal/mcp`. The adapter maps MCP tools, resources, and prompts to the same `internal/app` services used by the CLI and TUI; it does not shell out to `okt` and does not duplicate workflow or project-scope rules.

## Setup

Users can opt in from project initialization:

```sh
okt init --enable-mcp
```

Supported harnesses are `claude-code` (default), `claude-desktop`, and `opencode`. Setup writes an `omakiten` MCP server entry that runs:

```sh
okt mcp serve
```

Useful setup flags:

- `--mcp-dry-run` previews changes without writing.
- `--mcp-config <path>` writes to a specific harness config path.
- `--mcp-command <path>` overrides the command written into the harness config.
- `--mcp-force` replaces an existing `mcpServers.omakiten` entry.

Existing harness config is preserved. Omakiten refuses to replace an existing `omakiten` MCP entry unless `--mcp-force` is passed.

## Tools

The full surface is the source of truth in `internal/mcp/adapter.go:ListTools`. Currently 29 tools, grouped below.

### Project & workflow

| Tool | Purpose |
|---|---|
| `project.overview` | Implements `/okt`: active project identity, workflow, pending count, recent context, next-step prompt. |
| `project.resume` | Implements `/okt-resume`: project distribution, likely next work, blocked/dependent work, recent context. |
| `workflow.show` | Active workflow buckets and allowed transitions. |

### Tasks

| Tool | Purpose |
|---|---|
| `tasks.continue` | Implements `/okt-continue #<id>`: task details, dependencies, comments, workflow, context. |
| `tasks.list` | Lists active project tasks with optional bucket filter. |
| `tasks.create_intent` | Implements `/okt-create <description>` with similar-task detection and confirmation gate. |
| `tasks.create` | Direct task creation equivalent to `okt add`. |
| `tasks.move` | Moves a task through allowed workflow transitions. |

### Comments & activity

| Tool | Purpose |
|---|---|
| `comments.add` | Adds a human or agent task comment, with optional tag attachment. |
| `comments.list` | Lists task comments. |
| `task_activity.list` | Unified chronological feed for a task (comments + system events such as `task.created`, `task.moved`, `task.completed`); supports `order=asc\|desc`. |

### Dependencies

| Tool | Purpose |
|---|---|
| `dependencies.add` | Adds a project-scoped dependency with cycle checks. |
| `dependencies.remove` | Requires `confirmed=true` before deleting a dependency. |
| `dependencies.list` | Lists dependencies for one task or all project tasks. |

### Context

| Tool | Purpose |
|---|---|
| `context.add` | Adds a project handoff context entry. |
| `context.dump` | Dumps compact context at level 1, 2, or 3 under a token budget. |

### Tags

| Tool | Purpose |
|---|---|
| `tags.add` | Adds a tag to a task or project. The name is normalized to kebab-case and deduplicated. |
| `tags.remove` | Removes a tag from a task or project; requires `confirmed=true`. |
| `tags.list` | Lists tags for a specific task or project. |
| `tags.list_all` | Lists all tags across all projects with usage counts. |
| `tags.merge` | Merges a source tag into a target tag, reassigning references and deleting the source. |

### Errors & solutions (cross-project)

| Tool | Purpose |
|---|---|
| `errors.record` | Records a development-time error with optional context and tags. |
| `errors.search` | Searches errors by tag intersection and/or description text; returns nested solutions ranked by success then recency. |
| `solutions.add` | Attaches a candidate solution to an error. |
| `solutions.confirm` | Marks a solution success/failure; `success=true` increments its like counter. |
| `solutions.list_top` | Lists the top-N most-liked solutions globally. |

### Templates (read-only)

| Tool | Purpose |
|---|---|
| `templates.list` | Lists every loaded template (slug, name, default kind, project scope, custom flag); optional `kind`/`project`/`include_body` filters. |
| `templates.show` | Returns one template by slug, including its full body. |

### Progress

| Tool | Purpose |
|---|---|
| `progress.record` | Records material progress through edits, comments, context, and optional workflow moves in a single call. |

Every tool accepts optional project selector fields where useful: `project_id`, `project`, and `cwd`. Resolution follows Omakiten's standard order: explicit id, explicit slug, then current working directory inside a registered project root. `tags.list_all`, `errors.*`, `solutions.*`, and `templates.list` are intentionally cross-project and ignore the selector.

## Resources

| URI | Purpose |
|---|---|
| `omakiten://project/overview` | Reads the active project overview. |
| `omakiten://workflow/active` | Reads the active workflow. |

## Prompts

| Prompt | Intent |
|---|---|
| `okt` | Load project overview before continuing work. |
| `okt-create` | Create a task intent with duplicate detection. |
| `okt-continue` | Continue a project-owned task by id. |
| `okt-resume` | Resume from the most relevant checkpoint. |

## Confirmation Behavior

Ambiguous or destructive operations return `requires_confirmation` instead of mutating state.

- `tasks.create_intent` returns similar tasks and asks whether to continue existing work or retry with `confirmed=true`.
- `dependencies.remove` asks for `confirmed=true` before deleting the dependency.
- `tags.remove` asks for `confirmed=true` before detaching the tag from a task or project.

## Failure Guidance

Domain errors are mapped to compact coded failures with next-step guidance (`internal/agent/errors.go:guidanceForCode`). Codes currently defined in `internal/domain/errors.go`:

`config_invalid`, `project_not_found`, `project_ambiguous`, `task_not_found`, `workflow_invalid_transition`, `bucket_not_found`, `dependency_invalid`, `validation_error`, `law_not_found`, `skill_not_found`, `persona_not_found`, `skill_referenced`, `editor_failed`, `tag_not_found`, `tag_conflict`, `guard_violation`, `error_not_found`, `solution_not_found`.

## Scope Controls

- All reads and writes resolve one active `ProjectContext` at intent entry, except for the explicitly cross-project tools listed above (`tags.list_all`, `errors.*`, `solutions.*`, `templates.list`).
- Tasks, comments, dependencies, context entries, and tags are read or written through project-scoped repositories.
- Workflow movement goes through `app.WorkflowService.MoveTask` (transition allowance + guards + `task.completed` emission); task edits go through `app.TaskService.Edit`.
- The core `internal/agent` package has no MCP SDK, package-manager, or transport dependency. The composition root for the MCP server is `internal/agentruntime`; the protocol translation lives in `internal/mcp`.
- Hexagonal boundaries (no `agent` → `sqlite`/`configstore`/`mcp` imports) are enforced by `internal/arch/arch_test.go` and mirrored as `depguard` rules in `.golangci.yml`.
