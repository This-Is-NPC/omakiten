# Omakiten

[![Release](https://img.shields.io/github/v/release/This-Is-NPC/omakiten)](https://github.com/This-Is-NPC/omakiten/releases)
[![Release workflow](https://img.shields.io/github/actions/workflow/status/This-Is-NPC/omakiten/release.yml?branch=master&label=release)](https://github.com/This-Is-NPC/omakiten/actions/workflows/release.yml)
[![License](https://img.shields.io/github/license/This-Is-NPC/omakiten)](LICENSE)

Opinionated checkpoints for AI-driven development.

Omakiten is a local-first task and context manager for AI-assisted workflows. It lives in your terminal, keeps your project state in a local SQLite database, and exposes agent intents through the Model Context Protocol (MCP).

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

#### Pin a specific version

Both installers honor a `VERSION` environment variable for reproducible installs (CI, locked deployments). When it is unset, the script resolves to the latest GitHub release.

```bash
curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | VERSION=0.1.0 bash
```

```powershell
$env:VERSION = "0.1.0"; irm https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.ps1 | iex
```

Build from source (requires Go 1.25+):

```bash
git clone https://github.com/This-Is-NPC/omakiten.git
cd omakiten
go build -o okt ./cmd/okt
mv okt ~/.local/bin/
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

## Quick Start

```bash
# Add a task
okt add -t "Implement search endpoint"

# List tasks
okt list

# Move a task through workflow
okt move 1 --to review

# Add a handoff context entry
okt context add -b "Refactored auth layer to use middleware"

# Open the TUI
okt tui
```

## Next Steps

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

- [Contributing](CONTRIBUTING.md)
- [LICENSE](LICENSE)

