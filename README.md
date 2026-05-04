# Omakiten

Opinionated checkpoints for AI-driven development.

Omakiten is a local-first task and context manager for AI-assisted workflows. It lives in your terminal, keeps your project state in a local SQLite database, and exposes agent intents through the Model Context Protocol (MCP).

## Getting Started

### Install

Build from source with Go 1.25+:

```bash
git clone https://github.com/This-Is-NPC/omakiten.git
cd omakiten
go build -o okt ./cmd/okt
mv okt ~/.local/bin/
```

Or with mise:

```bash
mise run install
```

### Project Setup

Register your project and launch the TUI:

```bash
okt init --name MyProject --slug my-project
okt tui
```

### MCP Setup

Connect Omakiten to your AI agent via MCP.

**Claude Desktop:**

```bash
okt mcp setup --harness claude-desktop --force
```

**OpenCode:**

```bash
okt mcp setup --harness opencode --force
```

## Quick Start

```bash
# Add a task
okt add -t "Implement search endpoint"

# List tasks
okt list

# Move a task through workflow
okt move 1 --bucket review

# Add a handoff context entry
okt context add -b "Refactored auth layer to use middleware"

# Open the TUI
okt tui
```

## Next Steps

- [Architecture & Tech Stack](.docs/architecture.md)
- [Requirements & Behavior Map](.docs/requirements.md)
- [MCP Agent Surface](.docs/mcp-agent-surface.md)
- [Why Omakiten?](.docs/why_omakiten.md)
- [Contributing](CONTRIBUTING.md)
