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

| Tool | Purpose |
|---|---|
| `project.overview` | Implements `/okt`: active project identity, workflow, pending count, recent context, next-step prompt. |
| `project.resume` | Implements `/okt-resume`: project distribution, likely next work, blocked/dependent work, recent context. |
| `tasks.continue` | Implements `/okt-continue #<id>`: task details, dependencies, comments, workflow, context. |
| `tasks.list` | Lists active project tasks with optional bucket filtering. |
| `tasks.create_intent` | Implements `/okt-create <description>` with similar-task detection and confirmation. |
| `tasks.create` | Direct task creation equivalent to `okt add`. |
| `tasks.move` | Moves a task through allowed workflow transitions. |
| `comments.add` | Adds a human or agent task comment. |
| `comments.list` | Lists task comments. |
| `dependencies.add` | Adds a project-scoped dependency with cycle checks. |
| `dependencies.remove` | Requires confirmation before removing a dependency. |
| `dependencies.list` | Lists dependencies for one task or all project tasks. |
| `context.add` | Adds a project handoff context entry. |
| `context.dump` | Dumps compact context at level 1, 2, or 3. |
| `workflow.show` | Shows active workflow buckets and transitions. |
| `progress.record` | Records material progress through edits, comments, context, and optional workflow moves. |

Every tool accepts optional project selector fields where useful: `project_id`, `project`, and `cwd`. Resolution follows Omakiten's standard order: explicit id, explicit slug, then current working directory inside a registered project root.

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

## Failure Guidance

Domain errors are mapped to compact coded failures with next-step guidance. Examples include `project_not_found`, `task_not_found`, `workflow_invalid_transition`, `bucket_not_found`, `dependency_invalid`, and `validation_error`.

## Scope Controls

- All reads and writes resolve one active `ProjectContext` at intent entry.
- Tasks, comments, dependencies, and context entries are always read or written through existing project-scoped repositories.
- Workflow movement uses `TaskService.Move` or `TaskService.Edit`, preserving existing transition guardrails.
- The implementation does not depend on porting the `assisted_workflow` skillset.
- The core `internal/agent` layer has no MCP SDK, package manager, or transport dependency.
