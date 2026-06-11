# Configuration guide

How to configure Omakiten through the active profile yaml plus the sibling entity folders. Each doc below teaches one feature and inlines its YAML schema — pick the one matching what you want to change.

| Doc | Covers |
|---|---|
| [system.md](system.md) | Runtime knobs — `config.{output,context,workflow,mcp,tui,sqlite,solutions,backup,events,search,tag_synonyms}` plus top-level shape and validation. |
| [workflows.md](workflows.md) | `workflows[]` schema — buckets, transitions, operations, task/comment permissions. |
| [command-bindings.md](command-bindings.md) | `mcp_commands` and persona skill-repertoire bindings for MCP prompt composition. |
| [entities.md](entities.md) | Entity asset loaders — skills, laws, personas, projects, templates, frontmatter, autoload/custom rules. |
| [enums.md](enums.md) | `config.template_defaults`, `config.priorities`, and `config.severities`. |
| [views.md](views.md) | `config.views` sort/filter/window defaults for board/table/graph/logs/task activity. |
| [guards.md](guards.md) | The five transition-and-operation guard types, their YAML payloads, failure shapes, and how to add a new type. |
| [subtask-kit.md](subtask-kit.md) | Per-level kit cascade — `subtask_kit:` shape, validator rules, migration order, `task.bucket_orphaned` event schema, hook subject metadata, transparency notice. |
| [hooks.md](hooks.md) | Event subscriptions in `config.hooks`, the built-in action contracts (`exec`, `noop`, `notification`), and the `${{intl:KEY}}` interpolation. |
| [notifications.md](notifications.md) | TUI notification cards loaded from `notifications/<slug>.yaml` and dispatched from hooks. |
| [tricks.md](tricks.md) | Trick palette (`ctrl+k`) — full reference: command catalog, Tricks + Search tab keybindings, open dispatch, `config.tricks.nav` overrides, reserved verbs, hook recipes, troubleshooting. |
| [themes.md](themes.md) | TUI theme YAML (`themes/<key>.yaml`) — 8 color tokens, markdown palette, authoring recipe, `config.theme` wiring. |
| [languages.md](languages.md) | Bundled language packs under `defaults/languages/`, parity rule, scaffolding helper, and how to add a new locale. |
| [path-resolution.md](path-resolution.md) | ConfigRoot precedence, `.active` resolution, `<root>/` layout, `okt config <sub>` inspectors, backup paths. |
| [project-overrides.md](project-overrides.md) | Per-project `.omakiten/` discovery, immutable per-project Snapshot, hot-reload semantics, invariants the runtime depends on. |

## Where to start

- **Picked a preset and want to change one knob.** → [system.md](system.md) for runtime knobs, [workflows.md](workflows.md) for buckets/guards, [command-bindings.md](command-bindings.md) for prompt roles, or [entities.md](entities.md) for asset wiring.
- **Adding a project-local override.** → [project-overrides.md](project-overrides.md) for the layering model, [path-resolution.md](path-resolution.md) for the `.omakiten/` walk-up.
- **Building a hook or wiring a notification.** → [hooks.md](hooks.md) → [notifications.md](notifications.md).
- **Authoring a custom theme or translation.** → [themes.md](themes.md) / [languages.md](languages.md).
- **Stuck on a `guard_violation` error.** → [guards.md](guards.md) for guard payloads/failures or [workflows.md](workflows.md) for CRUD policy.
