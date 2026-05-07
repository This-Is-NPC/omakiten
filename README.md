# Omakiten

[![Release](https://img.shields.io/github/v/release/This-Is-NPC/omakiten)](https://github.com/This-Is-NPC/omakiten/releases)
[![License](https://img.shields.io/github/license/This-Is-NPC/omakiten)](LICENSE)

Opinionated checkpoints for AI-driven development.

Omakiten is a local-first task and context manager for AI-assisted workflows. It lives in your terminal, keeps your project state in a local SQLite database, and exposes agent intents through the Model Context Protocol (MCP) so your AI assistant can read and update project state directly — without you re-explaining context every session.

## Getting Started

### Install

**Linux / macOS / WSL:**

```bash
curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | bash
```

**Windows (PowerShell):**

```powershell
irm https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.ps1 | iex
```

### Project Setup

Register your project and launch the TUI:

```bash
okt init --name MyProject --slug my-project
okt tui
```

### MCP Setup

Connect Omakiten to your AI agent via MCP.

**Claude Code:**

```bash
okt mcp setup --harness claude-code --force
```

**Claude Desktop:**

```bash
okt mcp setup --harness claude-desktop --force
```

**OpenCode:**

```bash
okt mcp setup --harness opencode --force
```

## Use Omakiten with your AI agent

Once MCP is set up, your agent can read and mutate Omakiten state directly. Two usage modes coexist: **canonical slash prompts** for the most common moves, and **natural-language requests** that the agent translates into MCP tool calls.

### Canonical prompts

Four prompts ship as MCP prompts and work in any harness that supports them:

| Prompt | When to use it |
|---|---|
| `/okt` | Start of a session — loads project identity, active workflow, pending count, and the next-step suggestion so the agent stops guessing what's already happening. |
| `/okt-resume` | Coming back to a project after a pause — surfaces the most relevant work to pick up next, including blocked items and recent handoff context. |
| `/okt-continue <task_id>` | Resuming a specific task — pulls its dependencies, comments, workflow position, and recent context in one shot. |
| `/okt-create <description>` | Creating a task **with duplicate detection** — the agent first checks for similar/related work and asks to confirm before creating. |

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
| "Log this error: TLS handshake fails on staging — and the workaround I tried was disabling HTTP/2." | Records the error, attaches a candidate solution. |
| "Have we ever hit this error before?" | Searches errors across **all** projects you track. |
| "That fix worked." | Confirms the solution as known-good (increments its like counter). |

### Why this changes how you work

- **No re-onboarding every session.** Your agent always starts with the same picture you do.
- **Workflow rules apply to the agent too.** It cannot move a task through a forbidden transition or skip a workflow guard — your guardrails are real, not aspirational.
- **Errors and fixes become institutional memory.** Every error you record + every confirmed solution is searchable across projects, so the agent stops re-discovering the same fix.
- **Handoffs are just notes.** A short context entry today is what makes "pick up where we left off" actually work tomorrow.

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
