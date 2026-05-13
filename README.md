# Omakiten

[![Release](https://img.shields.io/github/v/release/This-Is-NPC/omakiten)](https://github.com/This-Is-NPC/omakiten/releases)
[![License](https://img.shields.io/github/license/This-Is-NPC/omakiten)](LICENSE)

**Opinionated checkpoints for AI-driven development.**

AI agents lose context between sessions, take actions outside your workflow, and rediscover the same fixes every month. Omakiten is the local source of truth they read before continuing so they always start from the same picture you have.

It lives in your terminal and keeps your project state tasks, dependencies, decisions, errors and fixes in a local SQLite database. Your agent reads and writes it through the Model Context Protocol (MCP), so the workflow rules you set apply to the agent too.

## Install

**Linux / macOS / WSL:**

```bash
curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.ps1 | iex
```

The installer asks which AI agents you use and wires Omakiten into each via MCP — see the [MCP Guide](.docs/mcp-guide.md#setup) for the harness list and re-running setup later. It also asks which workflow preset to activate (defaults to `omakase`); pick a different one with `OKT_PRESET=<name>` or via the interactive prompt — see [Workflow Presets](#workflow-presets) below. Then register your first project:

```bash
okt init --name MyProject --slug my-project
# or pin a different preset per-project:
okt init --name MyProject --slug my-project --preset shokunin
```

## How you work with it

Once connected, two modes coexist: **canonical slash prompts** for the most common moves, and **natural-language requests** the agent translates into MCP tool calls.

### Canonical prompts

Eight prompts ship as MCP prompts and work in any harness that supports them. The first five form the happy-path cycle (`okt → okt-resume / okt-imagine → okt-create → okt-continue → okt-implement`); the last three are parallel surfaces for execution, drift survey, and config orientation.

| Prompt | When to use it |
|---|---|
| `/okt` | **Start of a session** loads project identity, active workflow, pending count, and the next-step suggestion so the agent stops guessing what's already happening. |
| `/okt-imagine` | **PLAN phase — discovery before any task exists** the product-owner persona interrogates you via 5W2H (What / Why / Who / When / Where / How / How much), frames success in SMART terms, and surfaces gaps before the task is filed. |
| `/okt-create <description>` | **PLAN → DO handoff — formalize the imagined work** the agent runs duplicate detection, asks to confirm, then files the task with an INVEST-checked user story and prioritization rationale (MoSCoW / RICE) when alternatives exist. |
| `/okt-resume` | **Coming back to a project after a pause** surfaces the most relevant work to pick up next, including blocked items and recent handoff context. |
| `/okt-continue <task_id>` | **Resuming a specific task** pulls its dependencies, comments, workflow position, and recent context in one shot. |
| `/okt-implement` | **Executing approved work** runs the engineer's implement loop with bounded self-review, conventional commits, and self-report on retried errors. |
| `/okt-document` | **Surveying project documentation** lists drift items in `.docs/`, `README.md`, `CONTRIBUTING.md` with file references — does not edit in place. |
| `/okt-config` | **Orienting the agent on the active config layout** before edits — path resolution, entity folders, frontmatter shapes, wiring, workflow guard kinds. Read-only. |

### Natural-language scenarios

Beyond the slash prompts, describe the action and the agent picks the right tool:

| What you say to the agent | What the agent does |
|---|---|
| "What's the state of this project?" | Reads `project.overview` and summarizes. |
| "Pick up where we left off." | Reads `project.resume` and proposes next work. |
| "Continue task 42." | Loads task 42 with deps + comments + recent context. |
| "Move task 17 to review." | Calls `tasks.move` (transition rules and guards still apply). |
| "What's blocking task 42?" | Lists dependencies + their bucket state. |
| "Save a handoff note: refactored auth to use middleware." | Stores a context entry for the next session. |
| "Log this error: TLS handshake fails on staging and the workaround I tried was disabling HTTP/2." | Records the error, attaches a candidate solution. |
| "Have we ever hit this error before?" | Searches errors across **all** projects you track. |
| "That fix worked." | Confirms the solution as known-good (increments its like counter). |

## What makes Omakiten different

### Guardrails the agent can't bypass

Define your workflow as buckets and explicit transitions `backlog → dev → review → done` — with rules between them: *can't leave `dev` without a `#review` comment*, *can't move to `done` while blockers are still open*. Your agent is bound by the same rules you are. Forbidden moves come back as coded errors, not silent state changes. → [Guards Guide](.docs/guards-guide.md)

### One tool, every project

Omakiten tracks every project on your machine in a single local database. Switch directories and your agent picks up where you left off **no re-explaining** context, no separate setup for each repo. Project resolution falls back to your current working directory automatically.

### Memory that survives the session

Errors and the fixes that worked become a searchable, **cross-project** knowledge base. *"Have we hit this before?"* returns matches from any repo on your machine. Your agent stops re-discovering the same fix every quarter.

### Compact handoffs that fit the context window

Context dumps are tiered (level 1–3) and capped at a token budget you set. Your agent gets exactly enough state to continue not a wall of unrelated history. *"Pick up where we left off"* finally means something.

### Customize how your agent behaves

Define rules your agent must follow, give it personas with curated skill sets, set up templates for tasks/PRs/comments, declare workflow defaults and per-bucket CRUD policy, and reshape domain enums (priorities ship as a configurable id↔value table) — all in plain YAML and Markdown under your config directory. Edit them, version them, share them with a teammate by copying a folder. → [Configuration Guide](.docs/configuration-guide.md)

**Per-repo overrides**: drop a `.omakiten/` directory at the root of a project and Omakiten layers it over your user-global config — same `skills/`, `laws/`, `personas/`, `templates/`, `notifications/` folders plus an optional `omakiten.yaml` overlay. Walk-up discovery (git-style) finds it from any subdir. The repo-local layer is config-only; SQLite stays in your home directory so the data is yours, the conventions are the team's. → [Configuration Guide › Repo-local override](.docs/configuration-guide.md#repo-local-omakiten-override-4-layer-resolution)

### Workflow presets

Four official presets ship under `defaults/config/`. Each one is a different **process discipline** — they do not prescribe architecture, only how the team works through the development cycle.

| Preset | Methodology | When to use |
|---|---|---|
| **omakase** | Trunk-based + CI + DORA + TDD + Conventional Commits + Boy-Scout cleanup | The balanced default. Mainstream professional software work. |
| **izakaya** | Lean Startup + XP Spike + Tracer Bullet + Walking Skeleton | Spikes, prototypes, side-projects. Minimum ceremony. |
| **kaiseki** | Staged delivery (PMBOK-flavored) with formal sign-offs + decision records + peer review | Planned features in a serious codebase. Architecture-agnostic. |
| **shokunin** | Site Reliability Engineering + Pre-mortem + Multi-reviewer change control + Blameless postmortem | Regulated environments, irreversible changes, audit-trail-mandatory work. |

The installer asks which one to activate at install time (defaults to omakase). Switch later from the TUI Settings › Config picker, with `okt init --preset <name>` on a new project, or by editing `~/.config/omakiten/config/.active`. List the menu via `okt config presets`.

Every preset's `okt-imagine` interrogates you via 5W2H (What / Why / Who / When / Where / How / How much) so you understand what you're building before any code is planned. Success criteria land in SMART form; priorities (when alternatives exist) record as MoSCoW or RICE. The `okt-*` cycle maps to Plan-Do-Check-Act — see the [Workflow Guide § PDCA mapping](.docs/workflow-guide.md#pdca-mapping--the-cycle-behind-every-preset).

Authoring your own preset is a first-class path. The agent can orient itself on the active configuration layout via the `/okt-config` MCP prompt — frontmatter shapes, wiring, guard kinds, and the naming convention all live in the orientation template the agent fetches via `templates.show config-orientation`. The full picker / fork recipe sits in the [Workflow Guide](.docs/workflow-guide.md).

### Local-first, by design

Your tasks, comments, dependencies, errors, and fixes live in a SQLite file in your home directory. No account. No telemetry. No cloud.

## When you want to see it

`okt tui` opens a terminal UI organised into three zones — Tasks (board / table / graph), Stats (model benchmark / activity logs), Settings (runtime info / entity browser) — same data the CLI and MCP layers see, just visual. Run it outside a project and it opens a multi-project home — pick one, work on it, and your shell lands in that project's folder when you exit. → [TUI Guide](.docs/tui-guide.md)

The full MCP surface (36 tools, 2 resources, 8 prompts) is documented in the [MCP Guide](.docs/mcp-guide.md).

## Documentation

**Reference**

- [Architecture & Tech Stack](.docs/architecture.md)
- [Requirements & Behavior Map](.docs/_generated/requirements.md)
- [Why Omakiten?](.docs/why_omakiten.md)

**User guides**

- [CLI Guide](.docs/cli-guide.md)
- [TUI Guide](.docs/tui-guide.md)
- [MCP Guide](.docs/mcp-guide.md)
- [Configuration Guide](.docs/configuration-guide.md)
- [Workflow Guide — presets and authoring your own](.docs/workflow-guide.md)
- [Workflow Guards Guide](.docs/guards-guide.md)
- [Theming Guide](.docs/theming-guide.md)
- [Data Model Guide](.docs/data-model-guide.md)
- [Domain Events Catalog](.docs/domain-events.md)
- [Hooks Engine](.docs/hooks.md)
- [Notifications](.docs/notifications.md)
- [Integration Guide — wiring hooks](.docs/integration-guide.md)

**Project**

- [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [LICENSE](LICENSE)
