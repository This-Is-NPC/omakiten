# Omakiten documentation

Public documentation for the Omakiten CLI/TUI/MCP toolkit. 21 docs total, grouped by audience.

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

## Authoring docs in this tree

Three rules govern edits to `.docs/`:

1. **Every atom of information has one canonical home.** Other docs reference it by anchor link. Copying the text is forbidden.
2. **Each doc declares "Update when".** Look for the "Update when" section at the bottom of every doc — that names the trigger that should bring you back to edit it.
3. **No catalogs Omakiten can derive from code.** Domain events, tag vocabulary, per-preset wiring snapshots, and CLI flag dumps should never be hand-maintained. The source files (`internal/domain/events.go`, `defaults/config/<preset>.yaml`, etc.) are the source of truth — link to them and let agents read directly.

### Atom map — where each fact lives

| Atom | Canonical home |
|---|---|
| Law / skill / persona / template description | `defaults/<kind>/<slug>.md` frontmatter (inspect via `okt <kind> show <slug>`) |
| Theme colors and TUI palette | `defaults/themes/<slug>.yaml` (linked from `configuration-guide/themes.md`) |
| Notification card definitions | `defaults/notifications/<slug>.yaml` (linked from `configuration-guide/notifications.md`) |
| Bundled language packs | `defaults/languages/<code>.yaml` (linked from `configuration-guide/languages.md`) |
| Per-preset wiring | `defaults/config/<preset>.yaml` (compared in `presets.md`) |
| Tag vocabulary | walked from `comments_tagged.tag` in preset yamls — no static doc |
| Domain events | `internal/domain/events.go::KnownEventTypes` (cross-referenced from hooks / events docs) |
| Mental models + citations | `why_omakiten.md` (canonical) |
| Filesystem layout + path resolution | `configuration-guide/path-resolution.md` (canonical: `internal/paths/paths.go`) |

### Token / size budgets (kept for reuse in entity bodies)

| Kind | Body budget | Notes |
|---|---|---|
| Law | ≤120 tokens | Rule + one bad/good example pair max |
| Skill | ≤80 tokens | Steps or rules; no narrative |
| Persona | ≤200 tokens | Voice + loop only; no architecture prescription |
| Template | ≤250 tokens | Placeholder scaffold only |

These limits exist because every law/skill/persona/template a preset wires into a prompt expands inline. The agent's context budget is finite.

### What NOT to prescribe in workflow entities

Process discipline, yes. Architecture, no. Reviewer ergonomics, yes. Specific frameworks (Clean / Hexagonal / DDD / MVC), no. Best practices (TDD, peer review, decision records), yes. Specific tooling (Jest / pytest / Datadog), no.

The kit ships four official presets — `omakase`, `izakaya`, `kaiseki`, `shokunin` — each a distinct **process discipline**. None prescribe architecture.

### Workflow for adding a new entity (canonical: a new law)

1. Create `defaults/laws/<slug>.md` with `name`, `severity`, and a body that explains the rule.
2. Wire the law into the relevant preset under `defaults/config/<preset>.yaml` (`mcp_commands.<command>.laws`).
3. Run `mise run check` — lint, vet, tests must stay green.

Two hand-edited files total: the entity and the preset yaml. Release notes come from the PR / release-please flow.

### Workflow for adding a bundled language pack

See [configuration-guide/languages.md](configuration-guide/languages.md) — `scripts/new-language-pack.sh` scaffolds the file with TODO markers and the parity test catches drift.
