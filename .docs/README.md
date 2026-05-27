# Omakiten documentation

Public documentation for the Omakiten CLI/TUI/MCP toolkit. 22 docs total, grouped by audience.

## Root — orientation and user-facing surfaces

| Doc | What it covers | Update when |
|---|---|---|
| [why_omakiten.md](why_omakiten.md) | Positioning, mental models (PDCA / 5W2H / SMART / INVEST / MoSCoW / RICE / OKR), bibliography. | Product framing or referenced mental models change. |
| [presets.md](presets.md) | Side-by-side comparison of the four official presets (izakaya / omakase / kaiseki / shokunin). | A new preset ships, or a preset's discipline summary shifts. |
| [workflow.md](workflow.md) | Conceptual workflow loop — PDCA mapping, per-preset walkthrough, plans / multi-agent, defaults. | Workflow concept evolves; preset behavior changes meaningfully. |
| [cli.md](cli.md) | `okt` command reference — flags, subcommands, output envelope. | A subcommand or global flag lands. |
| [tui.md](tui.md) | Terminal UI surfaces — views, key bindings, markdown rendering, the dev-editorial design language. | A view, panel, or key binding changes. |
| [mcp.md](mcp.md) | MCP surface — tools, resources, prompts, scope controls, per-project routing. | A tool or prompt lands; the dispatch contract changes. |

## configuration-guide/ — how to configure features

Each doc inlines the YAML schema for the feature it teaches. See [configuration-guide/README.md](configuration-guide/README.md) for a topic-by-topic map.

| Doc | What it covers |
|---|---|
| [system.md](configuration-guide/system.md) | `config.{output,context,workflow,mcp,tui,sqlite,activity_log,solutions,backup,events,search,tag_synonyms}` plus top-level shape and validation. |
| [entities.md](configuration-guide/entities.md) | Entity wiring — `workflows[]`, `skills`, `laws`, `personas`, `projects`, `templates`, `mcp_commands`, plus enum tables (`priorities`, `severities`), view defaults, `template_defaults`. |
| [guards.md](configuration-guide/guards.md) | The five transition-and-operation guard types, their YAML payloads, and how to add a new type. |
| [subtask-kit.md](configuration-guide/subtask-kit.md) | Per-level kit cascade — `subtask_kit:` shape, validator rules, migration order, `task.bucket_orphaned` event, hook subject metadata, transparency notice. |
| [hooks.md](configuration-guide/hooks.md) | `config.hooks` schema, the built-in actions (`exec`, `noop`, `notification`), and a step-by-step walkthrough for wiring a hook into your workflow. |
| [notifications.md](configuration-guide/notifications.md) | TUI notification cards loaded from `notifications/<slug>.yaml` and dispatched from hooks. |
| [themes.md](configuration-guide/themes.md) | TUI theme YAML — 8 color tokens, markdown palette, authoring recipe. |
| [languages.md](configuration-guide/languages.md) | Bundled language packs under `defaults/languages/`, parity rule, scaffolding helper. |
| [path-resolution.md](configuration-guide/path-resolution.md) | ConfigRoot precedence, `.active` resolution, `<root>/` and `dev_env/` layouts, `okt config <sub>` inspectors, backups. |
| [project-overrides.md](configuration-guide/project-overrides.md) | Per-project `.omakiten/` discovery, immutable per-project Snapshot, hot-reload semantics, invariants. |

## internal/ — contributor docs

| Doc | What it covers | Update when |
|---|---|---|
| [architecture.md](internal/architecture.md) | High-level architecture map — hexagonal layering, packages, the per-project Snapshot pattern. | A package boundary, dependency direction, or composition root changes. |
| [requirements.md](internal/requirements.md) | Functional / non-functional requirements with file references. | A new feature ships or a requirement's status shifts. |
| [dev-guide.md](internal/dev-guide.md) | Local dev setup — `mise` tasks, test layout, common workflows. | A `mise` task, scaffold script, or local-dev convention changes. |
| [data-model.md](internal/data-model.md) | SQLite schema map and the relationship to the in-memory bundle. | A migration lands or the table list changes shape. |
| [authoring.md](internal/authoring.md) | Rules for editing `.docs/` — atom map, token budgets, what NOT to prescribe, and workflows for adding entities or language packs. | A new entity type, token budget, or authoring rule changes. |
