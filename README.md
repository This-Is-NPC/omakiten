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

## Connect your AI agent

The installer ends with an interactive multi-select prompt — pick the agents you use (any of `claude-code`, `claude-desktop`, `opencode`, `crush`, `github-copilot`, `codex`) and Enter. Each selection is wired via `okt mcp setup --harness <name> --force`, so the install + MCP-setup flow is one step.

To skip the prompt (or pre-select in CI), set `OKT_HARNESSES`:

```bash
OKT_HARNESSES=claude-code,opencode \
  curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | bash
```

Then register the project so the MCP layer has something to attach to:

```bash
okt init --name MyProject --slug my-project
```

You can also re-run the wiring at any time without reinstalling:

```bash
okt mcp setup --harness claude-code --force
okt mcp setup --harness codex --force
# ... or any harness from the list above
```

The full table of harnesses, default config paths, and server-entry shapes lives in the [MCP Guide](.docs/mcp-guide.md#setup).

## How you work with it

Once connected, two modes coexist: **canonical slash prompts** for the most common moves, and **natural-language requests** the agent translates into MCP tool calls.

### Canonical prompts

Four prompts ship as MCP prompts and work in any harness that supports them:

| Prompt | When to use it |
|---|---|
| `/okt` | **Start of a session** loads project identity, active workflow, pending count, and the next-step suggestion so the agent stops guessing what's already happening. |
| `/okt-resume` | **Coming back to a project after a pause** surfaces the most relevant work to pick up next, including blocked items and recent handoff context. |
| `/okt-continue <task_id>` | **Resuming a specific task** pulls its dependencies, comments, workflow position, and recent context in one shot. |
| `/okt-create <description>` | **Creating a task with duplicate detection** the agent first checks for similar/related work and asks to confirm before creating. |

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

Define rules your agent must follow, give it personas with curated skill sets, and set up templates for tasks, PRs, and comments all in plain YAML and Markdown under your config directory. Edit them, version them, share them with a teammate by copying a folder. → [Configuration Guide](.docs/configuration-guide.md)

### Local-first, by design

Your tasks, comments, dependencies, errors, and fixes live in a SQLite file in your home directory. No account. No telemetry. No cloud.

## When you want to see it

`okt tui` opens a terminal UI organised into three zones — Tasks (board / table / graph), Stats (model benchmark / activity logs), Settings (runtime info / entity browser) — same data the CLI and MCP layers see, just visual. Run it outside a project and it opens a multi-project home — pick one, work on it, and your shell lands in that project's folder when you exit. → [TUI Guide](.docs/tui-guide.md)

The full MCP surface (29 tools, 2 resources, 4 prompts) is documented in the [MCP Guide](.docs/mcp-guide.md).

## Documentation

**Reference**

- [Architecture & Tech Stack](.docs/architecture.md)
- [Requirements & Behavior Map](.docs/requirements.md)
- [Why Omakiten?](.docs/why_omakiten.md)

**User guides**

- [CLI Guide](.docs/cli-guide.md)
- [TUI Guide](.docs/tui-guide.md)
- [MCP Guide](.docs/mcp-guide.md)
- [Configuration Guide](.docs/configuration-guide.md)
- [Workflow Guards Guide](.docs/guards-guide.md)
- [Theming Guide](.docs/theming-guide.md)
- [Data Model Guide](.docs/data-model-guide.md)

**Project**

- [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [LICENSE](LICENSE)
