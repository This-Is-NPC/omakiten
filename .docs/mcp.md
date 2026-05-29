# MCP Agent Surface

Omakiten exposes a protocol-neutral agent intent layer in `internal/agent` and an MCP adapter in `internal/mcp`. The adapter maps MCP tools, resources, and prompts to the same `internal/app` services used by the CLI and TUI; it does not shell out to `okt` and does not duplicate workflow or project-scope rules.

A small set of operations are deliberately CLI/TUI-only — `projects.delete`, `db.backup`, `update`, `uninstall`, `setup`. Destructive or install-affecting ops never land on MCP; everything else does.

## Contents

- [Setup](#setup)
- [Tools](#tools)
- [Resources](#resources)
- [Prompts](#prompts)
- [Anatomy of an MCP command](#anatomy-of-an-mcp-command)
- [Confirmation Behavior](#confirmation-behavior)
- [Failure Guidance](#failure-guidance)
- [Per-project routing](#per-project-routing)
- [Scope Controls](#scope-controls)
- [See also](#see-also)

## Setup

Users can opt in from project initialization:

```sh
okt init --enable-mcp
```

Supported harnesses (CLI value → config target):

| Harness | Default config path | Format | Server entry root |
|---|---|---|---|
| `claude-code` *(default)* | `~/.claude.json` | JSON | `mcpServers.omakiten` |
| `claude-desktop` | `<UserConfigDir>/Claude/claude_desktop_config.json` | JSON | `mcpServers.omakiten` |
| `opencode` | `<UserConfigDir>/opencode/opencode.json` | JSON | `mcp.omakiten` |
| `crush` | `~/.config/crush/crush.json` (Linux/macOS) · `%LOCALAPPDATA%\crush\crush.json` (Windows) | JSON | `mcp.omakiten` |
| `github-copilot` | `<UserConfigDir>/Code/User/mcp.json` (VS Code Copilot Chat, agent mode) | JSON | `servers.omakiten` |
| `codex` | `~/.codex/config.toml` | TOML | `[mcp_servers.omakiten]` |

Setup writes an `omakiten` server entry that runs:

```sh
okt mcp serve
```

Useful setup flags:

- `--mcp-dry-run` previews changes without writing.
- `--mcp-config <path>` writes to a specific harness config path.
- `--mcp-command <path>` overrides the command written into the harness config.
- `--mcp-force` replaces an existing `omakiten` entry.

Existing harness config is preserved (unrelated keys, other MCP servers, and any TOML tables outside `[mcp_servers]` round-trip untouched). Omakiten refuses to replace an existing `omakiten` MCP entry unless `--mcp-force` is passed.

### Bundled installer flow

Both `install.sh` and `install.ps1` end with an interactive multi-select prompt that lists every supported harness and runs `okt mcp setup --harness X --force` for each one chosen — no separate setup step required. Use the env override `OKT_HARNESSES=claude-code,opencode` (comma/space/tab/newline separated) to pre-pick selections and skip the prompt; in non-interactive shells (CI, no `/dev/tty`, non-`UserInteractive` PowerShell) the prompt is silently skipped.

### Inspecting prompts

```sh
okt mcp prompts            # render every okt-* prompt
okt mcp prompts okt-implement   # render one
```

Resolves each `okt-*` prompt through the running agent service (using your active config) and prints the composed markdown to stdout, separated by horizontal rules and annotated with byte/rune counts. Useful for previewing the exact prompt the agent receives without spinning up an MCP client.

`mise run mcp:prompts` is the contributor shortcut: syncs `dev_env/` defaults and runs the same command against them.

## Tools

The full surface is the source of truth in `internal/mcp/adapter.go::Tools` (the public entry; the inner `tools()` returns the literal table). Currently 50 tools, grouped below.

### Required `_agent_model` on every call

Every tool call must carry a top-level `_agent_model` string identifying the AI model invoking the tool (e.g. `"claude-opus-4-7"`, `"claude-sonnet-4-6"`, `"gpt-5"`). Calls without it return `validation_error` with self-describing guidance — the agent can fix its own request without a follow-up.

An optional `_agent_session_id` lets the metrics layer correlate searches to records within a session (used by `metrics.summary`'s `search_before_record_ratio`). Both fields are stripped from the input before tool-specific decoding, so they never leak into per-tool DTOs. They are denormalized on every write (`events`, `errors`, `solutions`) so cross-table benchmarks don't need joins.

System-internal entry points (`ReadResource`) bypass the coercive check and write empty attribution so synthetic samples don't pollute the benchmark.

### Project & workflow

| Tool | Purpose |
|---|---|
| `project.overview` | Implements `/okt`: active project identity, workflow, pending count, recent context, next-step prompt. |
| `project.resume` | Implements `/okt-resume`: project distribution, likely next work, blocked/dependent work, recent context. |
| `workflow.show` | Active workflow buckets and allowed transitions. |
| `orphans.migrate` | Rebind tasks whose bucket was deactivated by a workflow swap. First call without `confirmed=true` returns a preview report + `Confirmation` block listing every affected task; retry with `confirmed=true` to apply the rebind. Empty preview short-circuits to a no-op regardless of the flag. Mirrors the CLI `okt workflow orphans` command. |

### Tasks

| Tool | Purpose |
|---|---|
| `tasks.continue` | Implements `/okt-continue #<id>`: task details, dependencies, comments, workflow, context. |
| `tasks.list` | Lists active project tasks. Optional `bucket_key` scopes by workflow bucket. Optional `parent_id` scopes by sub-task relationship: omit for no filter, pass `null` to return roots only (`parent_id IS NULL`), pass an integer to return direct children of that task id. |
| `tasks.create_intent` | Implements `/okt-create <description>` with similar-task detection and confirmation gate. Accepts optional `parent_id` to attach the new task as a sub-task in the same call; the parent must belong to the active project and be `state=active`. Sub-tasks inherit the parent's current bucket when `bucket_key` is omitted. The `task.created` event payload carries `parent_id` when set so audit consumers can attribute sub-task creation. |
| `tasks.create` | Direct task creation equivalent to `okt add`. Accepts optional `parent_id` to create a sub-task in one call; same parent-must-be-active + bucket-inheritance contract as `tasks.create_intent`. Row + FK land in a single atomic INSERT. |
| `tasks.move` | Moves a task through allowed workflow transitions. Subject to the `subtasks_complete` guard where wired — promotion is rejected while any direct child sits outside the workflow's final bucket. |
| `tasks.edit` | Edits a task's title, description, priority, and/or parent. `parent_id` uses a tri-state: omit to leave the column untouched, pass `null` to clear the FK (the task becomes a root), pass an integer to re-parent under that id. Re-parents that would create a cycle (target parent already descends from this task) are rejected with `validation`. At least one of the four optional fields must be provided. Subject to bucket `permissions.task.edit`; bucket moves still go through `tasks.move`. |
| `tasks.delete` | Hard-deletes a task with cascade (comments, tags, dependencies, events). Subject to bucket `permissions.task.delete` and `operations.delete.guards`. Requires `confirmed=true`. |
| `tasks.archive` | Flips `state=archived` and moves the task to the workflow's final bucket. Bypasses bucket policy and transition guards but respects `operations.archive.guards`. |
| `tasks.unarchive` | Restores `state=active` while leaving the bucket untouched. Respects `operations.unarchive.guards` if declared. |

### Plans (WBS-style multi-agent orchestration)

| Tool | Purpose |
|---|---|
| `plans.create` | Create a plan in the active project (`slug`, `name`, optional `goal_body` markdown). Emits `plan.created`. |
| `plans.list` | List every plan in the active project as a rollup: slug, name, status, done/total task counts, percentage, active wave id/name. |
| `plans.show` | Return one plan with its waves, tasks per wave, percentage, active wave, and the set of currently claimable tasks. Read-only. |
| `plans.add_wave` | Append a wave to a plan. Pass `position=0` (or omit) to auto-assign the next slot, or a positive integer to insert at that position. Emits `plan.wave_added`. |
| `plans.assign_task` | Attach an existing task to `(plan_id, wave_id)`. Idempotent re-assign within the same plan. |
| `plans.continue` | Agent-tailored projection of a plan — overview formatted for an agent picking up work. Overlaps `plans.show` in content; tuned for context-window economy. |
| `plans.claim_next` | Atomically reserve the next claimable task in the active wave: `BEGIN IMMEDIATE` → SELECT next-claimable-in-active-wave (first-bucket + unassigned) → SET `assigned_to` to the caller's `_agent_model` in the same transaction. The bucket is NOT touched; the task stays in the workflow's first bucket. Returns the claimed task or `{claimed: false}` when nothing is available. Two concurrent `claim_next` calls serialise at the SQLite write lock; the loser re-evaluates and either claims the next task or returns empty. |

`plans.claim_next` requires `_agent_model` like every tool, but the value is also written to `tasks.assigned_to` as the claimant identity. The claim is ownership-only — the bucket transition is a separate `tasks.move` call that goes through the workflow guard pipeline (e.g. omakase requires a self-branch comment before `backlog → dev`). Agents claim, then move; the two steps stay separate so preset-defined guards on the bucket transition remain authoritative. Recovery from a crashed agent is human-driven: `okt assign <id> ""` clears the assignment, or `okt move <id> backlog` clears it via the transition-out hook. v1 explicitly does NOT auto-reclaim.

### Comments & activity

| Tool | Purpose |
|---|---|
| `comments.add` | Adds a human or agent task comment, with optional tag attachment. |
| `comments.list` | Lists task comments. |
| `comments.edit` | Rewrites a comment's body and replaces its tags. Subject to bucket `permissions.comment.edit` (inherits from `permissions.task.edit` when no comment block is declared). |
| `comments.delete` | Hard-deletes a comment. Subject to bucket `permissions.comment.delete` (same inheritance rule). Requires `confirmed=true`. |
| `task_activity.list` | Unified chronological feed for a task (comments + system events such as `task.created`, `task.moved`, `task.completed`, `task.archived`, `task.unarchived`, `task.migrated`, `task.removed`, `task.assigned`, `task.unassigned`, `comment.edited`, `comment.removed`); supports `order=asc\|desc`. |

### Logs (generic event inspector)

| Tool | Purpose |
|---|---|
| `logs.list` | Generic Logs inspector over the unified events log — every event_type (task lifecycle, comments, plans, guards, hooks, tool calls, tricks, audits, domain bookkeeping) in one read. Optional `categories` (array of `task`/`comment`/`plan`/`tag-dep`/`guard`/`audit`/`hook`/`tool_call`/`trick`/`domain`) scopes the read; omit for all. Optional `since` accepts Go duration (`24h`, `30m`, `1h30m`) or N-day shorthand (`7d`, `30d`); omit to use the project's configured window (`config.views.logs.window_days`, 30 days by default). Optional `limit` caps the row count; omit for no MCP-side cap. Optional `order` is `desc` (default) or `asc`. Every row carries the full `EventRow` columns plus a `category` (resolved `EventCategory`) and a `summary` string rendered via `domain.SummarizeEvent` so agents see human-readable detail without parsing the payload JSON. `categories=["tool_call"]` reproduces the legacy activity-log filter. |

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

### Notes (knowledge entries)

| Tool | Purpose |
|---|---|
| `notes.create` | Creates a project-or-global knowledge note. `scope` is `"project"` (default when a project is resolved) or `"global"` (forces `project_id=NULL` so the row is visible cross-project). `kind` is a free string — convention: `"handoff"`, `"decision"`, `"architecture"`, `"requirements"`, `"runbook"`, `"gotcha"`, `"retrospective"`, `"glossary"`, `"free"`. Tags reuse the global tags table. Body is soft-limited to 64 KiB. |
| `notes.edit` | Patches an existing note. Omit a field to leave it untouched; an empty `title`/`body`/`kind` rejects with `validation_error`. The `tags` pointer is a full replacement — pass an empty array to clear every tag. |
| `notes.show` | Returns one note row plus its tags by id. |
| `notes.list` | Lists notes filtered by `scope` (`""`, `"project"`, `"global"`), `kind`, `tags` (intersection), `pinned`, and `limit`/`offset`. Default ordering: pinned DESC, `updated_at` DESC, `id` DESC. Default scope when a project resolves is "any" — both project-scoped and global notes flow back. |
| `notes.delete` | Hard-deletes a note. Requires `confirm=true`; the first call without confirmation returns a Confirmation block. Tags and FTS rows cascade via triggers. |

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
| `solutions.add` | Attaches a candidate solution to an error. |
| `solutions.confirm` | Marks a solution success/failure; `success=true` increments its like counter. |
| `solutions.list_top` | Lists the top-N most-liked solutions globally. |

> **Breaking change (0.16):** `errors.search` was removed in favour of the unified `search` tool below. The equivalent call is `search(query, entity_types=["error"])`. The unified call still emits the legacy `error.searched` domain event (with `"unified": true` in the payload) so `metrics.summary` keeps producing the search-before-record ratio per model.

### Search (unified FTS5 across content entities)

| Tool | Purpose |
|---|---|
| `search` | Full-text search across `task`, `comment`, `error`, `solution`, and `context` entities using SQLite FTS5. Required `query` is an FTS5 MATCH expression (phrase, prefix*, NEAR, AND/OR/NOT — see [sqlite.org/fts5.html](https://sqlite.org/fts5.html)). Optional `entity_types` restricts the kinds returned (omit or pass an empty list to cover all five). Optional `project` / `project_id` scopes the index to one project; omit both for the cross-project view. Archived tasks (`state='archived'`) are filtered out automatically. Each hit ships `entity_type`, `id`, `score` (negated BM25 so higher is better), `snippet` (matching content with `<mark>…</mark>` highlights), and `project_id`. Capped at 200 hits per call. |

### Templates (read-only)

| Tool | Purpose |
|---|---|
| `templates.list` | Lists every loaded template (slug, name, default kind, project scope, custom flag); optional `kind`/`project`/`include_body` filters. |
| `templates.show` | Returns one template by slug, including its full body. **Strict shadow validation:** when an active project resolves (via `project_id` / `project` / `cwd`) and the requested slug refers to a global template that the project shadows with an override of the same `default` kind, the call hard-rejects with `validation_error`. The rejection's `details` name `active_slug` so the agent can re-call directly. Outside any registered project, current slug-only lookup is preserved. An explicit `project` / `project_id` that does not resolve propagates `project_not_found` rather than falling back. |

### Metrics (cross-agent benchmarking)

| Tool | Purpose |
|---|---|
| `metrics.summary` | Aggregates per-AI-model behaviour over a period: errors recorded, errors searched, solutions added, like rate, and search-before-record ratio. Period defaults to `30d` (also accepts `7d` and `all`); invalid values fall back to `30d`. Optional `project_id` narrows the view to one project; omit for the cross-project benchmark. Models that never pass `_agent_session_id` report `0.0` for `search_before_record_ratio` even if they search heavily — correlating searches to records requires session continuity, by design. Rows with empty `agent_model` (TUI human, system internals) are excluded. |

### Progress

| Tool | Purpose |
|---|---|
| `progress.record` | Records material progress through edits, comments, context, and optional workflow moves in a single call. |

Every tool accepts optional project selector fields where useful: `project_id`, `project`, and `cwd`. Resolution follows Omakiten's standard order: explicit id, explicit slug, then current working directory inside a registered project root. `tags.list_all`, `errors.*`, `solutions.*`, `templates.list`, and `metrics.summary` are intentionally cross-project and ignore the selector (`metrics.summary` accepts a separate `project_id` filter argument when scoped is desired).

## Resources

| URI | Purpose |
|---|---|
| `omakiten://project/overview` | Reads the active project overview. |
| `omakiten://workflow/active` | Reads the active workflow. |

## Prompts

`prompts/list` is built from `agent.CommandNames()`; bindings come from `mcp_commands` in the active profile yaml. Each prompt resolves a persona, the union of bound laws, and any bound templates into a single user message — see the worked example below.

| Prompt | Intent |
|---|---|
| `okt` | Load project overview before continuing work. |
| `okt-imagine` | PLAN phase — product-owner persona interrogates the user via 5W2H and frames success in SMART terms before any task exists. |
| `okt-create` | PLAN → DO handoff — author the task with INVEST-checked user story; record prioritization (MoSCoW / RICE) when alternatives exist. |
| `okt-resume` | Scan likely-next work across the active project. |
| `okt-continue` | Read a task's checkpoint as an engineer before resuming work. |
| `okt-implement` | Execute approved engineering work with strict rigor and commit discipline. |
| `okt-document` | Survey project documentation for drift and propose updates. |
| `okt-config` | Orient the agent on the active Omakiten config layout before edits. |
| `okt-commit` | Draft Conventional Commits for the working tree without pushing. |
| `okt-review` | Walk a diff through Fowler/Beck/Martin/Feathers lens; emit findings + refactor opportunities by file:line, severity-tagged. Read-only. |
| `okt-check` | Discover the project's check targets via `mise tasks` and report pass/fail in a tabular comment. |
| `okt-handoff` | Close a session with a synthesised handoff note covering delta since the previous handoff, active work, decisions, blockers, and next steps. Writes a `notes.create` row with `kind="handoff"` against the resolved scope. |
| `okt-note` | Capture a free-form knowledge note (project or global) without ceremony. Pairs with the `note-free` template. |
| `okt-standup` | Cross-project standup digest — for each project with activity in the window, surfaces the latest handoff plus the delta since. Read-side aggregation; no writes. |
| `okt-recap` | Temporal recap over a window — notes grouped by kind plus tasks moved to `done`. Useful for retrospectives or release notes. Read-side aggregation; no writes. |

The default kit follows a REST-style handoff pattern: each prompt's action text ends by suggesting the next prompt in the cycle. `okt → okt-resume / okt-imagine → okt-create → okt-continue → okt-implement` is the happy path; `okt-document`, `okt-config`, `okt-commit`, `okt-review`, and `okt-check` are parallel surfaces. `okt-handoff`, `okt-note`, `okt-standup`, and `okt-recap` sit alongside the loop: `okt-handoff` closes a session by writing a `handoff` note; `okt-note` captures ad-hoc knowledge; `okt-standup` and `okt-recap` read across projects / windows to orient the next session.

## Anatomy of an MCP command

Every `okt-*` prompt follows the same shape — only the bound tools and templates change. The flow is:

1. **Prompt resolution** — the MCP client sends `prompts/get` and the server returns a single composed `PromptMessage`. This message carries the bound persona, the persona's skills, every effective law (global ∪ persona ∪ command ∪ template-bound, minus `laws_disabled`), any bound templates, and the action text ending with a REST-style handoff to the next command.
2. **Tool calls (zero or more)** — the agent reads the action and calls the tool(s) named there. Most prompts point at one canonical tool (`okt-continue` → `tasks.continue`, `okt` → `project.overview`, etc.); read-only or open-ended ones (`okt-imagine`, `okt-document`) may call several or none.
3. **REST handoff** — the action text always ends with a pointer at the next prompt in the cycle, so the agent can suggest `/okt-implement` after `/okt-continue`, `/okt-create` after `/okt-imagine`, and so on.

The diagram below traces a concrete invocation — `/okt-continue 42` — but the shape applies to every prompt. Substitute the bound tool name and the DTO returned for the relevant command.

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant Client as MCP client
    participant Server as okt mcp serve
    participant Adapter as internal/mcp.Adapter
    participant Service as internal/agent.Service
    participant Store as SQLite (read model)
    participant Agent as LLM agent

    User->>Client: /okt-<command> [args]
    Client->>Server: prompts/get { name, args }
    Server->>Adapter: GetPrompt
    Adapter->>Service: ResolveCommand(name)

    Note over Service: Reads bundle snapshots seeded<br/>at runtime startup — no DB hit.<br/>Effective laws =<br/>global ∪ persona ∪ command<br/>∪ template-bound,<br/>− laws_disabled, deduped.

    Service->>Service: renderCommandMarkdown<br/>(persona description + body inline,<br/>skill names inline,<br/>law severity + name + body inline,<br/>template metadata only — body JIT,<br/>action text)
    Service-->>Adapter: ResolveCommandResponse{ Markdown, … }
    Adapter->>Adapter: cache_prompts? attach<br/>cache_control: ephemeral hint
    Adapter-->>Server: PromptResult
    Server-->>Client: { messages:[ user/text ] }
    Client->>Agent: inject user message (fixed size, see table)

    Note over Client,Agent: Subsequent prompts/get within<br/>the cache window reuse the cached<br/>body — ~90% cheaper.

    Note over Agent: Reads "## Action" block,<br/>decides which tool(s) to call.

    Agent->>Client: tools/call <bound tool> { args }
    Client->>Server: tools/call
    Server->>Adapter: CallTool
    Adapter->>Service: <command method>(input)

    Service->>Store: project-scoped reads<br/>(tasks / workflow / deps /<br/>comments / context / …)
    Store-->>Service: aggregated payload
    Service-->>Adapter: <command>Response{ … }
    Adapter-->>Server: ToolResult (json text)
    Server-->>Client: result
    Client->>Agent: inject tool result<br/>(variable size, see table)

    Note over Agent,User: Agent may call additional<br/>tools (progress.record,<br/>comments.add, tasks.move)<br/>before responding.

    Agent->>User: synthesized response +<br/>REST handoff to next prompt
```

### Two-message floor

Every prompt invocation injects **at least one message** (the composed prompt) and typically a second (the tool result):

1. **The composed prompt** — one `PromptMessage` (`role: user`, `content.text: <rendered markdown>`). Size is **fixed per prompt** because no per-task data is embedded.
2. **Tool result(s)** — zero or more, depending on what the action text directs. For prompts tied to a single tool, this is one `ToolResult` carrying a JSON payload whose size **varies** with the underlying data.

For action-heavy prompts (`okt-implement`, `okt-document`) the agent will fan out and call several tools — `progress.record`, `comments.add`, `tasks.move` — each one adding another tool result message to the window. Those are the agent's choice, not the prompt's structure.

### What ships inline vs JIT

Not every entity body trafega in the prompt. The renderer pre-loads what the agent needs at resolution time and defers heavy bodies to dedicated tools:

| Entity | What ships in the prompt | When body is fetched |
|---|---|---|
| Persona | `description` + full `body` (inline under `## Persona`) | Always inline — body carries the role's procedural loop |
| Skills | Names only, inline list under `## Skills — A, B, C` | Never — descriptions are decorative; procedure lives in persona body / action text |
| Laws | `severity` + `name` + full `body` (inline under `## Laws`) | Always inline — laws are constraints that must frame every step |
| Templates | Slug + optional name (when divergent from slug) + default kind + description | On demand via `templates.show <slug>` — template bodies can be long (PR scaffolds, story templates) and shipping them inline would dominate the prompt window |

The fetch hint for templates lives in the action text or the bound persona's body — not as a generic footer — so the prompt only mentions `templates.show` for commands that actually need it. `internal/agentruntime.TestTemplateBoundCommandsCarryFetchHint` enforces that contract: every `okt-*` prompt with bound templates must surface the hint somewhere in its rendered Markdown.

This follows Anthropic's just-in-time context engineering principle: ship lightweight identifiers, let the agent fetch payloads on demand. The `template-fidelity` law remains pre-loaded (it is a constraint that must shape the fetch when it happens), but the body the constraint applies to lives behind a tool call.

Trade-off: one extra MCP round-trip on the materialization step (only when the agent actually drafts the PR or fills the scaffold), in exchange for a meaningful drop in every prompt resolution that does not reach materialization.

### What does NOT ship in the prompt body

- Prompt name and description — both already exposed via `prompts/list` metadata in the MCP protocol; aware clients surface them before calling `prompts/get`. Echoing them in the body would just duplicate bytes the agent already has.
- Skill descriptions — see table above.
- Law count parenthetical — `## Laws` carries no `(N)` suffix; the agent does not branch on the number.

### Per-prompt size budgets

Workflow presets are user-tunable — bound persona / skills / laws / templates change as authors fork the kit, so absolute prompt sizes are an unstable target. A regression test (`internal/agentruntime/prompt_budget_test.go`) holds each prompt to its current footprint plus ~30% headroom against the canonical omakase kit; once a future change pushes a prompt past its budget the test fails and forces a deliberate tradeoff (trim entity bodies, add a JIT optimization, or raise the budget with justification).

The `mise run mcp:prompts` task renders every resolved prompt to stdout against the dev kit — use it locally to inspect the current shape of any prompt without committing a snapshot table to docs.

### Prompt engineering principles applied

The default kit follows Anthropic's [context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents) guidance. Customizers who add their own commands, laws, or templates should match these conventions so the resolved prompts stay coherent:

- **Right altitude in action texts.** Action text describes the command's contract — name the canonical tool, end with a REST handoff. **Role lives in the persona body** (rendered in `## Persona`); **constraints live in bound laws** (rendered in `## Laws`); **scaffold lives in the templates section**. Action text never restates any of those — `mcp_commands.<cmd>.persona` is configurable, so persona-coupled prose in the action would leak engineer-specific instructions into prompts that bind a different persona. The persona body is the single source of truth for "how this role works"; the laws are the single source for "what is forbidden / required"; the action is just the command-specific bootstrap and handoff.
- **Just-in-time over pre-loading.** Template bodies are fetched via `templates.show` rather than embedded inline. The same logic applies to any heavy artifact (long context entries, large dumps): expose a tool, ship the slug, let the agent pull the body when actually needed.
- **Few-shot examples in load-bearing laws.** Laws that govern judgment calls (`template-fidelity`, `conventional-commits`, `no-assumptions`, `self-report`) carry a `Bad:` / `Good:` micro-example after the directive paragraph. Anthropic's principle: examples teach generalization better than abstract rules. Plain text labels (no emoji) keep the prompt readable across terminals and clients.
- **No conditional logic in prompts.** Anti-pattern: `if returns requires_confirmation, ask the user…`. Instead, the server's response carries an actionable `Reason` field that names the next-step tools — the agent acts on the response shape, not on prompt-side branching. See `agent.Confirmation.Reason` in `internal/agent/dto.go`.
- **Failure-driven additions.** Add a law or example only after observing a real failure mode. `template-fidelity` was added because the agent fabricated `Closes #40`; `authorize-remote-writes` was added because `git push` is destructive. Don't speculate.
- **Markdown sections, not XML tags.** The renderer uses `## Persona`, `## Skills`, `## Laws`, `## Templates`, `## Action`. Same load-bearing structure Anthropic recommends, but markdown reads cleanly in both the agent prompt and a developer's terminal when debugging.

### Variable tool-result cost

The composed prompt is only half the picture. For prompts that fetch task state, the tool result is the dominant variable. `tasks.continue`, for example, ships the task body (description can be long on rich tasks), the active workflow shape, the dependency list, the most recent comments with full body and tags, the most recent project context entries, and a `next_step_prompt` string. The total grows linearly with comment volume and description length — a fresh task is compact, a long-running one with many `#resume` notes dominates the window.

### Tuning context cost

The biggest variable is the tool result, not the prompt. Seven knobs in `config.mcp` (see `.docs/configuration-guide/system.md#configmcp`) shape it without changing the protocol:

| Setting | Affects | Impact |
|---|---|---|
| `recent_comment_limit` (int, default `5`) | Caps the comment-tail length in every checkpoint endpoint. Drop to `3` on tasks with many `#resume` notes. | Proportional to the truncated rows × their average body length. |
| `max_comment_chars` (int, default `0`) | Truncates each comment body past N runes with `…`. Set to ~`500` for a hard floor while keeping the latest exchange readable. | High on comment-heavy tasks; nil on terse ones. |
| `include_workflow_in_continue` (`*bool`, default `true`) | Skips the `workflow` block in `tasks.continue`. The agent already has the workflow from the first `/okt` of the session — set `false` to stop re-shipping. | Fixed per-call; matters on multi-task sessions. |
| `cache_prompts` (`*bool`, default `true`) | Emits an Anthropic `cache_control` hint on `prompts/get` content. Aware clients reuse the cached prompt across calls. | Bulk of the prompt body on subsequent calls within the cache window. |
| `recent_context_limit` (int, default `3`) | Caps recent handoff context entries included in checkpoint endpoints. | High when context entries are long prose. |
| `next_work_limit` (int, default `5`) | Caps likely-next-work suggestions in `project.resume`. | Keeps resume payloads bounded on large projects. |
| `similar_task_limit` (int, default `5`) | Caps similar-task hints returned by `tasks.create_intent`. | Bounds duplicate-detection chatter. |

The same accounting applies to every prompt — substitute the bound tool's DTO for the variable row above. `comments.list` is intentionally exempt from `max_comment_chars` because it's the explicit "read the full thread" endpoint; truncation would make the call useless.

Per-call overrides are available where they make sense: `tasks.continue` accepts an `include_workflow` boolean argument that wins over the config default for that single call.

## Confirmation Behavior

Ambiguous or destructive operations return `requires_confirmation` instead of mutating state.

- `tasks.create_intent` returns similar tasks and asks whether to continue existing work or retry with `confirmed=true`.
- `orphans.migrate` previews affected tasks first and applies only when retried with `confirmed=true`.
- `tasks.delete` asks for `confirmed=true` before hard-deleting a task and its cascaded rows.
- `comments.delete` asks for `confirmed=true` before hard-deleting a comment.
- `dependencies.remove` asks for `confirmed=true` before deleting the dependency.
- `tags.remove` asks for `confirmed=true` before detaching the tag from a task or project.

## Failure Guidance

Domain errors are mapped to compact coded failures with next-step guidance (`internal/agent/errors.go:guidanceForCode`). Codes currently defined in `internal/domain/errors.go`:

`config_invalid`, `config_too_large`, `project_not_found`, `project_ambiguous`, `task_not_found`, `workflow_invalid_transition`, `bucket_not_found`, `dependency_invalid`, `validation_error`, `law_not_found`, `skill_not_found`, `persona_not_found`, `skill_referenced`, `editor_failed`, `editor_not_found`, `tag_not_found`, `tag_conflict`, `guard_violation`, `error_not_found`, `solution_not_found`, `plan_not_found`, `plan_slug_conflict`, `plan_wave_not_found`, `uninstall_failed`, `update_failed`.

## Per-project routing

Every tool input may carry `project` (slug) or `project_id` (integer) — both are declared on the `selectorProperties` schema and accepted on every tool that exposes a selector. The MCP adapter `peekProjectArg`s these before dispatch, asks the runtime's `ServiceResolver` to look up the matching `*ProjectRuntime` from the `BundleCache`, and dispatches the call against that project's `agent.Service`. Calls without either field fall back to the adapter's default service (the boot-resolved project).

Implications:

- N agents may target N different projects through the same `okt mcp serve` process. Each call resolves an isolated `agent.Service`, hooks engine, action registry, notification snapshot, theme, tag synonyms, and stopwords — bundles do not cross-talk.
- A project without a `.omakiten/` install falls through to the default service (single-bundle behaviour). To upgrade a project to its own bundle, run `okt config init --scope local` inside its repo root.
- Cache rebuilds (mtime change on the on-disk YAML, explicit `cache.Reload` from the TUI) rotate the underlying `agent.Service`. The adapter's `DefaultServiceProvider` consults the runtime on every call so dispatch never lands on a stale pointer.
- Hooks fire only when the engine's `projectID` matches the event's `ProjectID`, with zero on either side opting out (system events reach every engine; engines built before a project resolves catch all). Two projects' hook entries never cross-fire.

## Scope Controls

- All reads and writes resolve one active `ProjectContext` at intent entry, except for the explicitly cross-project tools listed above (`tags.list_all`, `errors.*`, `solutions.*`, `templates.list`).
- Tasks, comments, dependencies, context entries, and tags are read or written through project-scoped repositories.
- Workflow movement goes through `app.WorkflowService.MoveTask` (transition allowance + guards + `task.completed` emission); task edits go through `app.TaskService.Edit`.
- The core `internal/agent` package has no MCP SDK, package-manager, or transport dependency. The composition root for the MCP server is `internal/agentruntime`; the protocol translation lives in `internal/mcp`.
- Hexagonal boundaries (no `agent` → `sqlite`/`configstore`/`mcp` imports) are enforced by `internal/arch/arch_test.go` and mirrored as `depguard` rules in `.golangci.yml`.

## See also

- [cli.md](cli.md) — sibling CLI surface for the same operations.
- `internal/domain/events.go::KnownEventTypes` — canonical list of events emitted by MCP tool calls.
