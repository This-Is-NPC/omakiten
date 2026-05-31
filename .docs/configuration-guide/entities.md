# Entity assets

Omakiten loads markdown assets from `skills/`, `laws/`, `personas/`, and `templates/`, then wires them from the active profile. This page owns asset frontmatter, autoload rules, custom overrides, and project/persona wiring. It does not document workflow schema, command binding schema, view defaults, or enum tables.

Use the source files when you need the exact shipped catalog:

- Entity assets: `defaults/{skills,laws,personas,templates}/` plus user `custom/` overrides.
- Active profile wiring: `defaults/config/<preset>.yaml` or `.omakiten/config/<profile>.yaml`.
- Command bindings: [command-bindings.md](command-bindings.md).

## Contents

- [Autoload, custom overrides, and slug rules](#autoload-custom-overrides-and-slug-rules)
- [`skills`](#skills)
- [`laws`](#laws)
- [`personas`](#personas)
- [`projects`](#projects)
- [`templates`](#templates)
- [Importing entity wiring](#importing-entity-wiring)
- [Update when](#update-when)

## Autoload, custom overrides, and slug rules

For each entity folder (`skills/`, `laws/`, `personas/`, `templates/`):

- the slug is the filename without `.md`;
- files at the folder root are shipped/default assets;
- files under `<folder>/custom/` override same-slug shipped files;
- when the matching top-level YAML allowlist is omitted, every file in that catalog loads;
- when an allowlist is present, only listed slugs activate;
- custom-scope decode errors degrade to warnings where the loader allows it; default-scope drift is fatal.

Themes, notifications, and language packs follow the same “loaded assets plus optional custom override” mental model, but their schemas live in their own guides.

## `skills`

```yaml
skills:
  - implementation
  - markdown
```

When present, only listed skill slugs activate. When omitted, all `skills/*.md` and `skills/custom/*.md` files load.

Skill frontmatter:

```markdown
---
name: Implementation
description: Implements task-scoped code changes.
---
Markdown body.
```

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Human label. |
| `description` | no | Short catalog summary. |

## `laws`

```yaml
laws:
  - template-fidelity
  - authorize-remote-writes
```

Top-level `laws:` is a strict allowlist when present. Command, persona, project, and template law references must resolve to loaded law assets.

Law frontmatter:

```markdown
---
name: Template fidelity
severity: warning
description: Do not invent fields when filling templates.
---
Markdown body.
```

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Human label. |
| `severity` | yes | Must resolve through `config.severities`; see [enums.md](enums.md#configseverities). |
| `description` | no | Short catalog summary. |

One law slug cannot be declared in multiple scopes (`global`, `persona`, `project`) inside the same resolved profile.

## `personas`

Persona bodies live in `personas/<slug>.md`. The active profile wires which personas load and which skills/laws they can use.

```yaml
personas:
  - slug: builder
    schema_version: 2
    skill_repertoire: [implementation, markdown]
    laws: [project-scope-only]
```

| Field | Required | Notes |
|---|---|---|
| `slug` | yes | Must match `personas/<slug>.md`. |
| `schema_version` | no | Use `2` for `skill_repertoire` wiring. |
| `skill_repertoire` | no | Full skill pool command bindings may draw from. |
| `laws` | no | Persona-scoped laws. |

Persona frontmatter:

```markdown
---
name: Builder
description: Implements approved increments.
laws:
  - project-scope-only
---
Markdown body.
```

Persona frontmatter `laws:` merge with the profile's `personas[].laws` for the same slug.

Command-specific skill selection is documented in [command-bindings.md](command-bindings.md#persona-skill-repertoires).

## `projects`

```yaml
projects:
  - slug: omakiten
    name: Omakiten
    description: Local checkpoint system for AI-assisted development.
    laws: [project-scope-only]
```

This block is declarative wiring, not the runtime project registry. Runtime projects live in SQLite. Project laws must resolve to loaded law assets and must not collide with global or persona law scopes.

## `templates`

```yaml
templates:
  - pull-request
  - user-story
```

Template frontmatter:

```markdown
---
name: Pull Request
description: Standard PR scaffold.
entity: pr
default: pr
project: omakiten
laws:
  - template-fidelity
---
Markdown body.
```

| Field | Required | Notes |
|---|---|---|
| `name` | yes | Human label. |
| `description` | no | Short prompt/catalog summary. |
| `entity` | no | Free-form classifier. |
| `default` | no | Must be listed in `config.template_defaults`; see [enums.md](enums.md#configtemplate_defaults). |
| `project` | no | Project slug for project-scoped defaults. |
| `laws` | no | Laws added when this template is bound to a command. |

Template bodies are fetched just in time through `templates.show`; prompt rendering ships metadata only.

## Importing entity wiring

Top-level entity wiring can be split from the active profile:

```yaml
personas:
  from: ./packs/roles/lean-personas.yaml

skills:
  from: ./packs/skills/core-skills.yaml
```

The imported document replaces the target value wholesale. Entity body/frontmatter files themselves do not honor `from:`; imports apply to active profile YAML values only.

Full import rules live in [`path-resolution.md`](path-resolution.md#modular-imports): relative paths, no escapes, cycle detection, max depth, and hot reload through `Bundle.SourcePaths`.

## Update when

Update this page when:

- a skill/law/persona/template frontmatter field changes;
- autoload or custom override behavior changes;
- `PersonaWiring` or `ProjectWiring` changes;
- entity loader validation changes.

Do not update this page merely because a bundled preset changes which concrete persona, skill, law, or template slug it uses. That belongs in the shipped YAML and can be inspected from the active config.
