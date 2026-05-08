---
name: Config Orientation
description: Map of where Omakiten config lives, what each entity kind is for, and how they wire together.
entity: orientation
laws:
  - template-fidelity
---
## Path resolution

`omakiten.yaml` is resolved by `internal/paths/paths.go` in this precedence:

1. `--config <path>` flag (CLI/TUI).
2. `$OMAKITEN_HOME/config/omakiten.yaml`.
3. `$XDG_CONFIG_HOME/omakiten/config/omakiten.yaml`.
4. `~/.config/omakiten/config/omakiten.yaml`.

The yaml lives under `<root>/config/`. Per-entity folders are siblings of `config/`, not nested inside it.

## Layout under `<root>`

| Folder | Purpose |
| --- | --- |
| `config/omakiten.yaml` | Canonical write-model: workflow, personas, mcp_commands, config knobs. |
| `config/.active` | One-line state file recording the active yaml profile. |
| `laws/<slug>.md` | Constraint entities. Frontmatter requires `severity` (`error` or `warning`). |
| `skills/<slug>.md` | Capability bundles bound to personas. |
| `personas/<slug>.md` | Role bodies. Frontmatter can declare `laws:` + `skills:`. |
| `templates/<slug>.md` | Scaffolds. Frontmatter can declare `entity`, `default`, `project`, `laws`. |
| `themes/<slug>.toml` | TUI palettes. |
| `<entity>/custom/` | User-authored overrides; preserved across `okt config defaults refresh`. |

## Entity frontmatter shape

| Kind | Required | Optional |
| --- | --- | --- |
| Law | `severity` | `name`, `description` |
| Skill | — | `name`, `description` |
| Persona | — | `name`, `description`, `skills`, `laws` |
| Template | — | `name`, `description`, `entity`, `default`, `project`, `laws` |

A template with `default:` needs its kind listed in `config.template_defaults`. Templates without `default:` still load and can be bound to MCP commands; they just aren't picker-eligible in the TUI.

## Wiring relationships

- **Persona → skills/laws.** Listed in the persona's frontmatter.
- **`mcp_commands.<cmd>` → persona / laws / templates.** Each prompt resolves one persona, the union of bound laws, and any bound templates.
- **`mcp_commands.global.laws`.** Inherited by every command unless opted out via `mcp_commands.<cmd>.laws_disabled`.
- **Effective laws.** `global ∪ persona.laws ∪ command.laws ∪ templates[].laws`, deduped, minus `laws_disabled`.

## Workflow shape

Lives under `workflows[]` in the yaml.

- `buckets[]` — `id`, `key`, `name`, `position`. Identity is `key`.
- `transitions[]` — `from` / `to` reference bucket `id`s.
- `transitions[].guards[]` — policy on the transition. Three kinds:
  - `comments_tagged` — requires N comments carrying a tag.
  - `comments_min` — requires N total comments.
  - `blockers_in` — requires every dependency to sit in a listed bucket.

Guards run in `app.WorkflowService.MoveTask` after the transition allowance check; the first failing guard short-circuits with `guard_violation`.

## Canonical references

For deeper detail, fetch the matching guide:

- `.docs/configuration-guide.md` — every yaml field, semantics, validation rules.
- `.docs/guards-guide.md` — guard kinds, evaluation order, MCP-prompt guardrails.
- `.docs/mcp-guide.md` — MCP tool surface, prompt anatomy, token costs.
