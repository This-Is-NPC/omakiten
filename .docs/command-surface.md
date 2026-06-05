# Command Surface

Omakiten's MCP prompt surface is a command router for agents. Command names, tiers, roles, scopes, and write behavior are the stable contract. Persona, skills, laws, and templates are configurable bindings documented elsewhere.

Source of truth in code:

- Prompt list (command slugs): `internal/agent/command_table.go` — a bare slug table. The operational playbook and the prompts/list description are entity-sourced from each command's bound `okt-<slug>-playbook` skill, not from Go. See [`mcp.md`](mcp.md#prompts) and [`configuration-guide/command-bindings.md`](configuration-guide/command-bindings.md#playbook-skills).
- Tier routing: `internal/agent/command_registry.go`.

## Tiers

| Tier | Shape | Purpose |
|---|---|---|
| Orchestrator | `okt`, `okt-start`, `okt-shape`, `okt-run`, `okt-audit`, `okt-pause` | Guide a session, decide the next move, and delegate to granular commands when needed. |
| System | `okt-help`, `okt-config`, `okt-skill` | Explain or inspect Omakiten itself. These commands do not operate on one project object. |
| Granular | `okt-<object>-<verb>` | Perform one precise step against a task, plan, project, or note. |

The normal human-facing loop is:

```text
okt-start -> okt-shape -> okt-run -> okt-audit -> okt-pause
```

Granular commands are the escape hatch when the user or orchestrator already knows the exact step.

## Roles

Roles are logical responsibilities. Bundled presets map those roles to themed personas, but documentation should not depend on a particular persona slug.

| Role | Responsibility |
|---|---|
| Concierge | Orient the session, recover handoffs, suggest the next command. |
| Owner | Shape scope, rank work, create tasks and plans, direct execution. |
| Builder | Implement and refactor task-scoped work. |
| Reviewer | Inspect diffs for correctness and design risk. |
| Security | Inspect diffs through a security lens. |
| Tester | Run or assess check targets and quality gates. |
| Scribe | Record handoffs, notes, debriefs, and recap timelines. |
| System | Explain or inspect Omakiten configuration and catalogs. |

## Command Matrix

| Command | Tier | Scope | Role | Writes? | Primary surface |
|---|---|---|---|---|---|
| `okt` | orchestrator | project | Concierge | no | Same action as `okt-start`. |
| `okt-start` | orchestrator | project | Concierge | no | Reads project, tasks, plans, and handoff notes; proposes next commands. |
| `okt-shape` | orchestrator | project/task/plan | Owner | indirect | Directs discovery, requirements, prioritization, task creation, and planning. |
| `okt-run` | orchestrator | plan/task | Owner | indirect | Directs Builder subagents; each subagent uses granular task commands. |
| `okt-audit` | orchestrator | task/plan/diff | Owner | indirect | Directs reviewer/security/quality passes and aggregates findings. |
| `okt-pause` | orchestrator | project | Concierge/Scribe | yes | Writes a project-scoped `kind=handoff` comment. |
| `okt-help` | system | tool | System | no | Explains command tiers and when to use them. |
| `okt-config` | system | config | System | no | Orients on active config and entity layout before edits. |
| `okt-skill` | system | catalog | System | no | Reads skill catalog or one skill body. |
| `okt-task-imagine` | granular | project/task seed | Owner | optional | 5W2H and SMART discovery before a task exists. |
| `okt-task-research` | granular | project/task seed | Owner | no | Maps prior art, unknowns, options, and trade-offs. |
| `okt-task-validate` | granular | project/task seed | Owner | no | Pressure-tests whether the problem is real and worth solving. |
| `okt-task-requirements` | granular | task seed | Owner | optional | Captures functional/non-functional criteria and acceptance. |
| `okt-task-prioritize` | granular | task seed | Owner | optional | Scores work against alternatives. |
| `okt-task-create` | granular | task | Owner | yes | Calls `tasks.create_intent` after feasibility and duplicate checks. |
| `okt-task-decompose` | granular | task | Owner | optional | Breaks coarse work into smaller task candidates. |
| `okt-task-estimate` | granular | task | Owner | optional | Sizes increments and records uncertainty. |
| `okt-task-design` | granular | task | Builder/Owner | optional | Designs approach before implementation. |
| `okt-project-resume` | granular | project | Concierge | no | Cold scan for likely next work. |
| `okt-project-continue` | granular | project | Concierge | no | Warm resume from the latest open thread. |
| `okt-plan-create` | granular | plan | Owner | yes | Creates a WBS-style plan. |
| `okt-plan-show` | granular | plan | Owner | no | Shows one plan's structure. |
| `okt-plan-continue` | granular | plan | Owner | no | Previews the next claimable plan task. |
| `okt-plan-claim` | granular | plan | Owner | yes | Reserves a task by assigning `tasks.assigned_to`; bucket is not moved. |
| `okt-task-resume` | granular | task | Builder | no | Cold-starts a task from persisted context. |
| `okt-task-continue` | granular | task | Builder | no | Reads a task checkpoint before work resumes. |
| `okt-task-implement` | granular | task | Builder | yes | Applies an approved increment and records progress/evidence. |
| `okt-task-self-review` | granular | task/diff | Builder | optional | Author's own pre-handoff diff pass. |
| `okt-task-refactor` | granular | task/diff | Builder | yes | Applies one behavior-preserving structural change. |
| `okt-task-document` | granular | project/docs | Scribe | no | Surveys documentation drift and proposes updates. |
| `okt-task-debrief` | granular | task | Scribe | optional | Captures learnings from completed work. |
| `okt-task-commit` | granular | git diff | Builder/System | yes | Drafts Conventional Commits; never pushes. |
| `okt-task-review` | granular | task/diff | Reviewer | optional | Reviews diff for correctness, design, and refactor findings. |
| `okt-task-secure` | granular | task/diff | Security | optional | Reviews diff for security risk. |
| `okt-task-check` | granular | task/project | Tester | optional | Runs discovered check targets and reports pass/fail. |
| `okt-task-quality` | granular | task/diff | Tester/Reviewer | optional | Performs qualitative quality assessment. |
| `okt-note-free` | granular | project/universal | Scribe | yes | Writes a free-form project or universal note comment. |
| `okt-note-recap` | granular | project/universal | Scribe | no | Renders a recap timeline; wide windows fold in handoff digests. |
| `okt-note-list` | granular | project/universal | Scribe | no | Lists note-like comments by scope/kind/tag/pinned filters. |
| `okt-note-show` | granular | comment id | Scribe | no | Reads one note-like comment by id. |

## See also

- [configuration-guide/command-bindings.md](configuration-guide/command-bindings.md) — `mcp_commands`, persona skill repertoires, and effective laws/templates.
- [mcp.md#anatomy-of-an-mcp-command](mcp.md#anatomy-of-an-mcp-command) — MCP prompt rendering and tool-call flow.
- [configuration-guide/path-resolution.md#modular-imports](configuration-guide/path-resolution.md#modular-imports) — value-level `from:` imports.
