# The Omakiten documentation

[The front door](../README.md) explains what Omakiten is. This directory
explains how to use it, in the order you need it, and where to look when
something needs a closer answer.

## Start here

1. [How to install Omakiten, and take it off again](how-to-install-and-remove.md)
   — a release or a checkout, what setup writes, and what uninstall preserves.
2. [How to put a project on the board](how-to-register-a-project.md)
   — register a root, select the project, and recover its context.
3. [How to take a task from an idea to done](how-to-work-on-a-task.md)
   — create, read, record evidence, and follow the active workflow.
4. [How to move a plan as one file](how-to-import-and-export-work.md)
   — Markdown tasks and complete OKF plans, with previews before importing.
5. [How to give an agent the same context](how-to-use-with-agents.md)
   — install the skill and use the CLI to resume and hand off work.

The shortest useful path is 1, 2, 3. Plans and agents use the same board; you
can add them when the work needs them.

## When you know what you want

| The page | What it answers |
| --- | --- |
| [The command line](cli.md) | Which command family to use, how to read its help, and what stdout and stderr carry. |
| [How to browse project knowledge](how-to-browse-project-knowledge.md) | File-backed Markdown, OpenAPI, CLI resources, and optional related projects. |
| [The terminal, in the order you meet it](screens.md) | Home, Project, the board, Task Detail, plans, search, and Studio. |
| [How to choose and share a workflow](how-to-manage-presets.md) | Catalog, repositories, installed snapshots, activation, and single-file transport. |
| [How to give a project its own workflow](how-to-configure-a-project.md) | `.omakiten/`, discovery, application preferences, and the shared database. |
| [How to change a workflow](how-to-customize-a-workflow.md) | Modules, entities, command bindings, buckets, permissions, guards, and staged edits. |
| [How to serve the board to a graphical client](how-to-run-the-local-api.md) | The local HTTP API, discovery, authentication, and the live event stream. |
| [How to run an action when work changes](how-to-use-hooks.md) | Events, executable actions, notifications, and where to inspect failures. |
| [How to change the appearance and language](how-to-change-appearance-and-language.md) | Color themes, persona themes, and application language preferences. |
| [How to keep and recover the board](how-to-back-up-and-recover.md) | Database snapshots, restore, search integrity, and recovery before deletion. |
| [When a command refuses](troubleshooting.md) | Configuration errors, workflow gates, wrong project selection, and import failures. |

## Working on Omakiten

Contributor guides live in `internal/`. They describe implementation contracts
and maintenance, rather than the steps to use the application.

| The page | What it answers |
| --- | --- |
| [How it is built](internal/design.md) | Ports, adapters, runtime ownership, and publication of configuration. |
| [What the database holds](internal/data-model.md) | Operational tables, relationships, events, and work-document metadata. |
| [Working on Omakiten](internal/development.md) | Mise, development state, focused verification, the pre-push gate, and releases. |
| [The behavior the project promises](internal/requirements.md) | Project isolation, workflow enforcement, atomic imports, and recovery. |
| [Assembling a TUI screen](internal/tui-screen-assembly.md) | The normative presentation and component ownership rules. |
| [Reviewing a change](internal/review-guide.md) | Severity, evidence, and routing a finding. |

## Conventions across the pages

**`example`, `42`, and `delivery` are placeholders.** They stand for a project
slug, a task id, and a plan slug. Use the values your commands return.

**The active workflow decides the vocabulary.** Bucket keys, priority labels,
permissions, and guards come from the selected preset. Inspect them with
`okt workflow show` before copying a transition from an example.

**The CLI owns the board.** Read and write tasks through `okt`, including from
agents. Installed preset files describe policy; SQLite holds the work.

**Help is available at every level.** `okt task --help` lists its verbs;
`okt task create --help` explains its flags. The guides teach a task rather
than reproducing every flag table.

Documentation is hand-written in English. Put usage in the how-to pages,
terminal behavior in `screens.md`, and contributor constraints in `internal/`.
Describe current behavior and link to its owner rather
than maintaining the same explanation in several places.
