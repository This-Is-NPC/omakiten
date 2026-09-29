# Omakiten

A local checkpoint for AI-assisted development. Tasks, decisions, failures,
fixes, and handoffs stay on the board after a terminal or agent session ends.
The next session reads that state before starting work.

[![Release](https://img.shields.io/github/v/release/This-Is-NPC/omakiten)](https://github.com/This-Is-NPC/omakiten/releases)
[![License](https://img.shields.io/github/license/This-Is-NPC/omakiten)](LICENSE)

## What it does

`okt` is the CLI. `okt tui` opens the same board in a terminal. An embedded
`omakiten` skill teaches agents to use the CLI, so humans and agents read and
write the same project state.

The database is local SQLite. Workflow repositories supply buckets, guards,
permissions, personas, skills, laws, and templates. Installed snapshots work
without the source checkout. Language preferences belong to the application
and apply across projects.

Tasks can start from Markdown. Plans travel as one OKF file containing waves,
tasks, and relationships. Presets also import and export as a single document.

## Install

Linux, macOS, or WSL:

```bash
curl -fsSL https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.sh | bash
```

Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/This-Is-NPC/omakiten/master/install.ps1 | iex
```

Setup asks for language, workflow, and skill destinations. The default
installer checks release checksums; authenticated release verification uses
strict mode with your own Cosign installation. Read
[installation and removal](docs/how-to-install-and-remove.md) for the complete
procedure and what it puts on the machine.

## Put the first project on the board

From its repository:

```bash
okt init --name Example --slug example --root "$PWD"
okt --project example project resume
okt --project example task create --title "Make the next action explicit"
okt --project example tui
```

`example` is a placeholder for your project slug. Keep the returned task id
for continuation and evidence. The selected workflow decides how the task
moves to completion.

## Read what you need next

- [The documentation map](docs/README.md) — every page and the question it answers.
- [Working on a task](docs/how-to-work-on-a-task.md) — evidence, guards, and handoffs.
- [Importing and exporting work](docs/how-to-import-and-export-work.md) — one file for a task or complete plan.
- [Using agents](docs/how-to-use-with-agents.md) — the skill, context recovery, and playbooks.
- [Choosing a workflow](docs/how-to-manage-presets.md) — catalog, repositories, and modified snapshots.
- [The terminal](docs/screens.md) — Home, Project, Task Detail, search, and Studio.
- [When a command refuses](docs/troubleshooting.md) — find the selected scope and the next recovery action.

For exact flags, use `okt --help` and `okt COMMAND --help`.

## Work on Omakiten

```bash
mise install
git config core.hooksPath scripts/hooks
mise run tui
```

Mise owns the pinned tools and project commands. The development terminal uses
isolated state under `dev_env/`; build output and reports belong under `.tmp/`.
The pre-push hook runs the verification gate for clean HEAD.

Read [CONTRIBUTING.md](CONTRIBUTING.md), [the design](docs/internal/design.md), and
[the development guide](docs/internal/development.md) before changing the repository.

## License

[MIT](LICENSE).
