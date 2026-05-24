# Omakiten

[![Release](https://img.shields.io/github/v/release/This-Is-NPC/omakiten)](https://github.com/This-Is-NPC/omakiten/releases)
[![License](https://img.shields.io/github/license/This-Is-NPC/omakiten)](LICENSE)

**Opinionated checkpoints for AI-driven development.**

AI agents lose context between sessions, act outside your workflow, and rediscover the same fix every month. Omakiten is the local source of truth they read before continuing — they always start from the picture you have.

It lives in your terminal and keeps your project state — tasks, dependencies, decisions, errors, and fixes — in a local SQLite database. Your agent reads and writes it through the Model Context Protocol (MCP), so the workflow rules you set apply to the agent too.

## Install

**Linux / macOS / WSL:**

```bash
curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.ps1 | iex
```

After downloading the binary the installer hands off to `okt setup`, a bubbletea picker that walks you through **CLI language → TUI language → agent-output language → workflow preset → MCP harnesses**. Re-run any time with `okt setup --update` to revisit your choices; existing rc-file wrapper and `omakiten.yaml` are preserved.

Six MCP harnesses ship wired: `claude-code`, `claude-desktop`, `codex`, `crush`, `github-copilot`, `opencode`. Pick one, many, or none — re-run `okt mcp setup --harness <name>` at any time.

For headless installs (CI, Dockerfile, dotfiles) pre-supply the five inputs and the picker stays silent:

| Env var          | Skips the picker for             |
|------------------|----------------------------------|
| `OKT_CLI_LANG`   | CLI language (e.g. `en`, `pt-br`) |
| `OKT_TUI_LANG`   | TUI language (defaults to CLI)    |
| `OKT_AGENT_LANG` | Agent output language (free-form, e.g. `Português (Brasil)`) |
| `OKT_PRESET`     | Workflow preset (default `omakase`) |
| `OKT_HARNESSES`  | MCP harnesses (CSV; `0` skips harness setup) |

Then register your first project:

```bash
okt init --name MyProject --slug my-project
# or pin a different preset per-project:
okt init --name MyProject --slug my-project --preset shokunin
```

Projects can also keep their config beside the code — drop a `.omakiten/config/<preset>.yaml` at the repo root and Omakiten loads that bundle while you're inside the tree. Snapshot, hot-reload, and orphan-task migration are isolated per-project so two repos never see each other's workflow.

### Update and uninstall

The binary self-services its own lifecycle — no need to re-pipe the installer for refreshes or remember the bundled shell scripts:

```bash
okt update --check               # report current vs. latest, no write
okt update --yes                 # download + atomically swap the binary
okt uninstall --yes              # remove binary + okt() wrapper, keep DB and config
okt uninstall --yes --purge      # nuke everything, including data and config
```

Both commands fall through to an interactive picker when invoked without flags on a TTY (cf. `okt uninstall` checkbox flow with on-disk size hints and a `THIS CANNOT BE UNDONE` line). See [`.docs/cli-guide.md`](./.docs/cli-guide.md#okt-update--fetch-latest-release-and-swap-the-binary) for the full flag tables, JSON envelope codes, and the Windows EXE-in-use caveat.

## How you work with it

Once connected, two modes coexist: **canonical slash prompts** for the most common moves, and **natural-language requests** the agent translates into MCP tool calls.

### Canonical prompts

Eleven prompts ship as MCP prompts. The first five form the happy-path cycle (`okt → okt-resume / okt-imagine → okt-create → okt-continue → okt-implement`); the remaining six are parallel surfaces for execution, drift survey, config orientation, commit drafting, diff review, and check discovery.

| Prompt | When to use it |
|---|---|
| `/okt` | **Start of a session** — loads project identity, active workflow, pending count, and the next-step suggestion. |
| `/okt-imagine` | **PLAN phase — discovery before any task exists** — product-owner persona interrogates you via 5W2H, frames success in SMART terms, surfaces gaps. |
| `/okt-create <description>` | **PLAN → DO handoff** — duplicate detection, INVEST-checked user story, prioritization rationale (MoSCoW / RICE) when alternatives exist. |
| `/okt-resume` | **Coming back to a project after a pause** — surfaces the most relevant work to pick up next, including blocked items and recent handoff context. |
| `/okt-continue <task_id>` | **Resuming a specific task** — pulls dependencies, comments, workflow position, and recent context in one shot. |
| `/okt-implement` | **Executing approved work** — bounded self-review, conventional commits, self-report on retried errors. |
| `/okt-document` | **Surveying project documentation** — lists drift items in `.docs/`, `README.md`, `CONTRIBUTING.md`; does not edit in place. |
| `/okt-config` | **Orienting the agent on the active config layout** — path resolution, entity folders, frontmatter shapes, wiring, guard kinds. Read-only. |
| `/okt-commit` | **Draft Conventional Commits from the working tree** — groups changes by intent, writes the "why", never pushes. |
| `/okt-review` | **Walk a diff through Fowler/Beck/Martin/Feathers lens** — emits findings + refactor opportunities by file:line, severity-tagged. Read-only. |
| `/okt-check` | **Discover and run the project's check targets** — `mise tasks` discovery first; report pass/fail in a tabular comment. |

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
| "Log this error: TLS handshake fails on staging." | Records the error, attaches a candidate solution. |
| "Have we ever hit this before?" | Unified `search` across tasks, comments, errors, solutions, and handoff context — every project on the machine. |
| "That fix worked." | Confirms the solution as known-good (increments its like counter). |

## What makes Omakiten different

### Guardrails the agent can't bypass

Define your workflow as buckets and explicit transitions — `backlog → dev → review → done` — with rules between them: *can't leave `dev` without a `#review` comment*, *can't move to `done` while blockers are still open*. Your agent is bound by the same rules you are. Forbidden moves come back as coded errors, not silent state changes. Per-bucket CRUD policy applies to edits and deletes too: a `done` bucket can freeze deletion entirely. → [Guards Guide](.docs/guards-guide.md)

### Memory that survives the session

A unified `search` MCP tool indexes five entity types — tasks, comments, errors, solutions, and handoff context — in a single SQLite FTS5 index ranked by BM25. *"Have we ever hit this error?"* answers from anywhere on your machine: any project, any session. Errors carry the candidate solutions that worked, so the agent stops re-discovering the same fix every quarter.

### Compact handoffs that fit the context window

Context dumps are tiered (level 1–3) and capped at a token budget you set. Your agent gets exactly enough state to continue — not a wall of unrelated history. *"Pick up where we left off"* finally means something.

### Speaks your language

**21 bundled language packs** — Arabic, Chinese (zh-cn), Danish, Dutch, English, Finnish, French, German, Hindi, Italian, Japanese, Korean, Marathi, Norwegian, Polish, Portuguese (Brazil), Russian, Spanish, Swedish, Turkish, Ukrainian. CLI, TUI, and agent-output language are chosen *independently* at install — read your terminal in English, browse the board in Portuguese, tell the agent to reply in Japanese. Missing your locale? `scripts/new-language-pack.sh <code> <native> <name>` scaffolds a TODO-marked pack the parity test keeps honest. → [Languages Guide](.docs/languages-guide.md)

### WBS-style plans with atomic claim

Group tasks into ordered **waves** under a plan, then let two-to-four agents fan out without racing. `plans.claim_next` is an atomic SQLite write — `BEGIN IMMEDIATE` serialises concurrent claims so the same task can never be picked twice, and the claiming agent's identity lands on `tasks.assigned_to` for free. A `wave_gate` guard keeps wave `N+1` blocked until wave `N` fully closes, so a fan-out cannot accidentally jump ahead. The TUI surfaces it as a column-per-wave network diagram next to the board / table / graph views. → [Workflow Guide § Plans](.docs/workflow-guide.md#plans--multi-agent-fan-out)

### Observable by design

Every meaningful state change emits a typed domain event (`task.created`, `task.moved`, `error.recorded`, `guard.violated`, …). A YAML-driven hooks engine subscribes to those events and runs configurable async actions; **notification cards** turn the same stream into pop-up feedback inside the TUI, with short message, optional tab-detail, and timeout dismissal. The MCP `metrics.summary` tool reduces the event log into a per-AI-model dashboard — errors recorded, errors searched, solution like-rate, search-before-record ratio — over a `7d`, `30d`, or `all` window. → [Domain Events Catalog](.docs/domain-events.md) · [Hooks Engine](.docs/hooks.md) · [Notifications](.docs/notifications.md)

### Customize how your agent behaves

Define laws your agent must follow, give it personas with curated skill sets, set templates for tasks / PRs / comments, declare workflow defaults and per-bucket CRUD policy, and reshape domain enums (priorities and severities ship as configurable id↔value tables) — all in plain YAML and Markdown under your config directory. Edit them, version them, share them with a teammate by copying a folder. → [Configuration Guide](.docs/configuration-guide.md)

### Local-first, every project

Tasks, comments, dependencies, errors, fixes — all in a SQLite file in your home directory or `.omakiten/` at the repo root. One tool tracks every project on the machine; switch directories and your agent picks up where you left off. No account. No telemetry. No cloud.

## Workflow presets

Four official presets ship under `defaults/config/`. Each one is a different **process discipline** — they do not prescribe architecture, only how the team works through the development cycle.

| Preset | Methodology | When to use |
|---|---|---|
| **omakase** | Trunk-based + CI + DORA + TDD + Conventional Commits + Boy-Scout cleanup | The balanced default. Mainstream professional software work. |
| **izakaya** | Lean Startup + XP Spike + Tracer Bullet + Walking Skeleton | Spikes, prototypes, side-projects. Minimum ceremony. |
| **kaiseki** | Staged delivery (PMBOK-flavored) with formal sign-offs + decision records + peer review | Planned features in a serious codebase. Architecture-agnostic. |
| **shokunin** | SRE + Pre-mortem + Multi-reviewer change control + Blameless postmortem | Regulated environments, irreversible changes, audit-trail-mandatory work. |

The installer asks which one to activate at install time (defaults to omakase). Switch later from the TUI Settings › Config picker, with `okt init --preset <name>` on a new project, or by editing `~/.config/omakiten/config/.active`. List the menu via `okt config presets`.

Every preset's `okt-imagine` interrogates you via 5W2H so you understand what you're building before any code is planned. Success criteria land in SMART form; priorities (when alternatives exist) record as MoSCoW or RICE. The `okt-*` cycle maps to Plan-Do-Check-Act — see the [Workflow Guide § PDCA mapping](.docs/workflow-guide.md#pdca-mapping--the-cycle-behind-every-preset).

Authoring your own preset is a first-class path. The agent orients itself on the active config via the `/okt-config` MCP prompt; the full picker / fork recipe sits in the [Workflow Guide](.docs/workflow-guide.md).

## When you want to see it

`okt tui` opens a terminal UI organised into three zones — **Tasks** (board / table / graph / plans), **Stats** (per-model benchmark / activity logs), **Settings** (runtime info / entity browser) — same data the CLI and MCP layers see, just visual. Task descriptions, comment bodies, and entity files render as styled markdown by default; press `M` to toggle raw. Editing config files in another tab hot-reloads the running TUI and prompts you through orphan-task migration if the new workflow's buckets changed.

Run it outside a project and it opens a multi-project home — pick one, work on it, and your shell `cd`s into that project's folder when you exit. → [TUI Guide](.docs/tui-guide.md)

The full MCP surface (44 tools, 2 resources, 11 prompts) is documented in the [MCP Guide](.docs/mcp-guide.md).

## Documentation

**User guides**

- [CLI Guide](.docs/cli-guide.md)
- [TUI Guide](.docs/tui-guide.md)
- [MCP Guide](.docs/mcp-guide.md)
- [Configuration Guide](.docs/configuration-guide.md)
- [Workflow Guide — presets and authoring your own](.docs/workflow-guide.md)
- [Workflow Guards Guide](.docs/guards-guide.md)
- [Theming Guide](.docs/theming-guide.md)
- [Languages Guide — adding a bundled pack](.docs/languages-guide.md)
- [Domain Events Catalog](.docs/domain-events.md)
- [Hooks Engine](.docs/hooks.md)
- [Notifications](.docs/notifications.md)
- [Why Omakiten?](.docs/why_omakiten.md)

**Contributors / internals**

- [Architecture & Tech Stack](.docs/internal/architecture.md)
- [Developer Guide](.docs/internal/dev-guide.md)
- [Data Model Guide](.docs/internal/data-model-guide.md)
- [Integration Guide — wiring hooks](.docs/internal/integration-guide.md)
- [Per-project Snapshot architecture](.docs/internal/per-project-snapshot.md)
- [Requirements & Behavior Map](.docs/internal/requirements.md)

**Project**

- [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [LICENSE](LICENSE)
