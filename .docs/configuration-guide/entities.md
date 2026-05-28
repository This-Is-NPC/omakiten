# Entities and wiring — workflows, personas, laws, skills, templates

This guide covers the entity wiring shapes in the active profile yaml: how workflows declare buckets/transitions/guards, how personas bind to skills and laws, how templates claim default slots, how MCP commands compose their persona-laws-templates triple, and the enum tables (priorities, severities) plus view defaults that ride alongside.

For the foundational `config.*` runtime knobs, see [system.md](system.md). For ConfigRoot and the on-disk layout, see [path-resolution.md](path-resolution.md). For per-project layering, see [project-overrides.md](project-overrides.md).

## Contents

- [`config.template_defaults`](#configtemplate_defaults)
- [`config.priorities`](#configpriorities)
- [`config.severities`](#configseverities)
- [`config.views`](#configviews)
- [`workflows`](#workflows)
- [Per-entity wiring](#per-entity-wiring)
  - [`skills`](#skills)
  - [`laws`](#laws)
  - [`personas`](#personas)
  - [`projects`](#projects)
  - [`templates`](#templates)
  - [`mcp_commands`](#mcp_commands)
- [Autoload, custom overrides, and slug rules](#autoload-custom-overrides-and-slug-rules)
- [Default kit reference](#default-kit-reference)
- [Update when](#update-when)

---

## `config.template_defaults`

```yaml
template_defaults: [task, pr, comment-resume, comment-selfbranch, comment-documentation]
```

The list of "kinds" that templates may claim as their `default:` slot. This field is required; the kit YAML is the canonical source for the default set.

A template `.md` whose frontmatter declares `default: <kind>` activates as the scaffold for that kind. The validator enforces:

- Every template's `default:` value must be in `template_defaults` (otherwise `default %q is not in config.template_defaults`).
- At most one template per `(default, project)` pair (`only one may`).

The TUI's template-default picker offers exactly the kinds in this list.

## `config.priorities`

Configurable id↔value table for task priorities. Code references the integer id (opaque); renderers (TUI, CLI, MCP, JSON output) resolve the human label via this table at the boundary, so renaming `"high"` to `"urgent"` here is a single-line YAML edit and existing tasks pick up the new label on the next read. The sqlite layer stores `priority_id` integers (see `migrations/015_priority_id.sql`); the column type guarantees rename safety on persisted rows.

```yaml
config:
  priorities:
    - id: 1
      value: low
      color: success     # theme token: error | warning | success | info
    - id: 2
      value: normal
      default: true       # at most one entry may set this
      color: info
    - id: 3
      value: high
      color: error
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | int >0 | yes | Storage handle and SQL sort weight (`ORDER BY priority` reads `priority_id`). List entries low→high so ascending order matches user intuition. Validator rejects duplicates and non-positive ids. |
| `value` | string | yes | Human label rendered in the TUI badges, CLI output, MCP `priority` field, and `Priority` JSON marshaling. Validator rejects empty strings and duplicates across entries. |
| `default` | bool | no | Marks the priority a task receives when the user creates one without naming a priority. At most one entry may set `default: true`; with none flagged, the runtime falls back to the middle entry by index. |
| `color` | string | no | Theme-token name picked up by the TUI badge renderer. Recognised: `error`, `warning`, `success`, `info`. Anything else falls back to `info`. |

Omitting the block keeps the canonical kit (`{1=low, 2=normal default, 3=high}`); add entries to introduce new priorities (e.g. `{id: 4, value: urgent, color: error}`) without any code change.

### Validation rules (parse-time)

| Rule | Error message shape |
|---|---|
| `id` <= 0 | `config.priorities: id must be positive, got 0 for value "low"` |
| missing `value` | `config.priorities[id=1]: value is required` |
| duplicate `id` | `config.priorities: id 1 declared twice (values "low" and "...")` |
| duplicate `value` | `config.priorities: value "low" declared twice (ids 1 and 4)` |
| more than one `default: true` | `config.priorities: at most one entry may set default: true (got 2)` |

### How the runtime wires it

`app.ConfigService.Import` (called from both composition roots — `internal/cli/root.go` and `internal/agentruntime/runtime.go`) returns a `*domain.EnumRegistry` alongside the bundle, between `LoadBundle` (validate) and `config.BuildSnapshot` (materialise the immutable per-project snapshot). Composition roots inject the registry into every service that resolves labels (`TaskService`, `LawService`, `WorkflowService`, `TUIQueryService`, `ConfigService`, `ContextService`, agent `Service`); the TUI Model builds its own copy from the priorities/severities slices it already receives. There are no process-global enum tables — every lookup goes through the injected registry via `registry.PriorityLabel(id)`, `registry.PriorityFromLabel("high")`, `registry.DefaultPriority()`, etc. JSON wire format of `domain.Priority` / `domain.Severity` is the raw int id (no `MarshalJSON`); label projection happens at DTO boundaries (e.g. `agent.TaskSummary.Priority` is the label string). Tests construct a fresh registry via `testfixtures.CanonicalRegistry()` and thread it through service constructors.

### Worked example — adding an "urgent" priority

```yaml
config:
  priorities:
    - id: 1
      value: low
      color: success
    - id: 2
      value: normal
      default: true
      color: info
    - id: 3
      value: high
      color: warning
    - id: 4
      value: urgent
      color: error
```

After re-importing the bundle (`okt config import` or restarting the TUI):
- The TUI form's priority cycle now offers `low | normal | high | urgent`.
- `okt task add --priority urgent` resolves to `priority_id = 4`.
- `ORDER BY priority DESC` lists urgent tasks first.
- Existing tasks keep their stored `priority_id`; nothing in the database changed.

To rename `"high"` to `"critical"` instead, change only the `value:` on entry id 3 — every existing task with `priority_id = 3` immediately renders as "critical" on the next read.

## `config.severities`

Configurable id↔value table for law severities. Same shape and contract as `config.priorities` — code references the integer id, and renderers (TUI badge, MCP `severity` field, JSON marshaling) resolve the human label via this table at the boundary. Migration 016 briefly persisted `laws.severity_id` in SQL, but migration 020 dropped the `laws` table along with every other config table — severities now live on law frontmatter (`severity: <label>`) and the bundle loader resolves the label to its integer id at parse time. Renaming a label is a single YAML edit; existing in-memory law entries pick up the new label on the next bundle reload.

```yaml
config:
  severities:
    - id: 1
      value: info
      color: info
    - id: 2
      value: warning
      default: true
      color: warning
    - id: 3
      value: error
      color: error
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `id` | int >0 | yes | Storage handle and SQL sort weight; declarations must be in ascending order (validator-enforced). |
| `value` | string | yes | Human label written in law frontmatter (`severity: <label>`), rendered in TUI badges, CLI output, MCP responses. |
| `default` | bool | no | Marks the severity applied when a law is created without naming one. At most one entry. |
| `color` | string | no | Theme token for the badge: `error` / `warning` / `success` / `info`. Defaults to `info`. |

Validator rules: positive unique ids, non-empty unique values, ≤1 default, ascending id order. Errors mirror the priority validator's shape (`config.severities: id 2 declared twice...`).

Adding a `{id: 4, value: blocker, color: error}` entry makes `severity: blocker` a valid frontmatter value; no code change needed. Renaming `"error"` to `"critical"` updates the badge for every law on the next bundle reload — the frontmatter `severity:` field is re-resolved against the active registry on every load.

## `config.views`

Per-view defaults seeded into the TUI on startup. Sort fields/orders are required where shown below; filter lists may be empty to mean "all". The kit YAML is the canonical source, so omitted sort knobs fail validation rather than falling back to code defaults.

```yaml
views:
  board:
    sort:   { field: created_at, order: desc }
    filter: { priority: [] }                          # subset of [low, normal, high]; [] = all
  table:
    sort:   { field: created_at, order: desc }
    filter:
      priority: []                                    # same enum
      bucket:   []                                    # subset of bucket keys in the active workflow; [] = all
  graph:
    sort:   { field: id, order: asc }                 # field in [id, title]
  logs:
    sort:        { order: desc }                      # field is rejected — only direction is configurable
    limit:       50                                   # int, > 0
    window_days: 30                                   # int, > 0; default time window for the Logs event inspector (CLI/MCP/TUI). CLI --since and MCP since override.
    filter:      { source: [] }                       # subset of [cli, tui, mcp] — legacy schema; not consumed by the new event inspector
  task_activity:
    sort:   { order: asc }                            # asc = chronological, desc = newest first
```

Allowed values come from `internal/config/validator.go`:

| Setting | Allowed values | Default |
|---|---|---|
| `board.sort.field`, `table.sort.field` | `id`, `title`, `priority`, `created_at` | `created_at` |
| `board.sort.order`, `table.sort.order` | `asc`, `desc` | `desc` |
| `graph.sort.field` | `id`, `title` | `id` |
| `graph.sort.order` | `asc`, `desc` | `asc` |
| `logs.sort.order` | `asc`, `desc` (no `field`) | `desc` |
| `logs.limit` | int `> 0` | `50` |
| `logs.window_days` | int `> 0` | `30` |
| `logs.filter.source` | subset of `cli`, `tui`, `mcp` | `[]` (no filter) |
| `task_activity.sort.order` | `asc`, `desc` (no `field`) | `asc` |
| `*.filter.priority` | subset of `low`, `normal`, `high` | `[]` |
| `table.filter.bucket` | subset of bucket keys in the active workflow | `[]` |

Typos surface with errors like `config.views.board.sort.field "creatd_at" is not one of [id title priority created_at]` rather than being silently ignored.

---

## `workflows`

```yaml
workflows:
  - id: 1
    key: omakase
    name: Omakase Workflow
    defaults: { … }    # optional — see workflow defaults
    buckets: [ … ]
    transitions: [ … ]
    operations: { … } # optional — see workflows[].operations
```

| Field | Type | Notes |
|---|---|---|
| `id` | int, > 0, unique | Stable identifier referenced by `transitions[].from` / `to`. |
| `key` | string, required, unique | Used by `config.workflow.active` and human-facing CLI/TUI flows. |
| `name` | string, required | Display name. |
| `defaults` | object, optional | Workflow-level fallback for task / comment edit / delete. See [Workflow defaults](#workflow-defaults). |
| `buckets` | list, non-empty | See [buckets](#workflowsbuckets). |
| `transitions` | list, optional | See [transitions](#workflowstransitions). Empty list = no moves allowed. |
| `operations` | object, optional | Guards for archive / delete / unarchive. See [operations](#workflowsoperations). |

For preset-level conceptual workflows (PDCA mapping, izakaya/omakase/kaiseki/shokunin walk-throughs, multi-agent plans), see the root [workflow.md](../workflow.md) — this section is the YAML schema only.

### Workflow defaults

```yaml
defaults:
  task:
    edit: true
    delete: false
  comment:
    edit: true
    delete: false   # comment may also omit fields — see inheritance below
```

`defaults` declares the policy applied when a bucket does not override the field. The full resolution chain for any `(bucket, entity, op)` lookup is:

1. `bucket.permissions.<entity>.<op>` — the per-bucket override.
2. Comment only: `bucket.permissions.task.<op>` — comment inherits from task at the bucket layer when the bucket has no `comment` block (or the field is `nil`).
3. `workflow.defaults.<entity>.<op>` — the workflow-level fallback.
4. Comment only: `workflow.defaults.task.<op>` — same comment-from-task inheritance at the defaults layer.
5. Implicit `true` — when nothing in the chain declares a value, the action is allowed ("no rule = allow").

The resolver picks the first non-`nil` value walking the chain top-to-bottom. Pointer booleans (`*bool`) distinguish "field omitted" from "explicitly `false`" — omitting `delete` flows to the next layer; writing `delete: false` ends the walk with a deny.

There is no hardcoded "first bucket is special" rule anymore. The default kit (`defaults/config/omakase.yaml`) declares the equivalent shape explicitly: strict defaults at the workflow level + an opt-in on the `backlog` bucket.

### `workflows[].buckets`

```yaml
buckets:
  - { id: 1, key: backlog, name: Backlog,     position: 1 }
  - { id: 2, key: dev,     name: Development, position: 2 }
```

| Field | Type | Notes |
|---|---|---|
| `id` | int, > 0, unique within workflow | Used by transitions. |
| `key` | string, required, unique within workflow | Stable identifier (also referenced by guards and view filters). |
| `name` | string, required | Display name. |
| `position` | int, >= 1 | Visual ordering in BOARD/TABLE views. The bucket at position 1 is the **default for new tasks** when none is supplied (`app.WorkflowService.ResolveDefaultBucket`); the bucket at the highest position is the **final** bucket whose entry triggers a `task.completed` event. |
| `permissions` | object, optional | Per-bucket task / comment CRUD policy. See [Bucket permissions](#bucket-permissions) below. |

#### Bucket permissions

`permissions` gates `tasks.edit`, `tasks.delete`, `comments.edit`, and `comments.delete` based on the bucket the task currently sits in. When the field is omitted at the bucket layer the resolver falls through to `workflow.defaults` and then to the implicit `true` — see [Workflow defaults](#workflow-defaults) above for the full chain.

When `permissions.comment` is partially set, only the explicit fields override; the rest inherit from `task` at the same layer. Example:

```yaml
buckets:
  - id: 1
    key: backlog
    name: Backlog
    position: 1
    permissions:
      task:
        edit: true
        delete: false   # default — kept explicit for clarity
  - id: 2
    key: dev
    name: Development
    position: 2
    permissions:
      task:
        edit: false     # frozen once accepted into dev
        delete: false
      comment:
        delete: true    # but reviewers may still purge comments
        # edit inherits task.edit (false)
  - id: 3
    key: done
    name: Done
    position: 3
    permissions:
      task:
        edit: false
        delete: true    # operators can purge completed work
```

When a CRUD operation is denied, the service emits a `guard_violation` error whose `details.hint` lists the buckets where the action *is* permitted, so the agent can suggest a remediation move.

### `workflows[].operations`

```yaml
operations:
  archive:
    guards:
      - type: comments_tagged
        tag: archive-reason
        count: 1
        hint: "Add a #archive-reason comment before archiving."
  delete:
    guards:
      - type: comments_tagged
        tag: justification
        count: 1
        hint: "Add a #justification comment before deleting."
  unarchive:
    guards: []   # default
```

`operations` declares guards that gate the non-flow lifecycle operations:

- **archive** — flips `state=archived`, moves the task into the workflow's final bucket, and **bypasses bucket permissions and transition guards** (escape hatch). Operation guards still apply.
- **delete** — hard-deletes the task with cascade (comments → event_tags → events → dependencies → tags → task). Subject to **both** the bucket's `permissions.task.delete` and `operations.delete.guards`.
- **unarchive** — flips `state=active`, leaves the bucket untouched. No guards by default.

Operation guards reuse the same shape as transition guards (`type`, `tag`, `count`, `hint`). In the MVP only `comments_tagged` is wired into the operation evaluator; other types pass validation but currently have no effect on the operation path.

### `workflows[].transitions`

```yaml
transitions:
  - from: 1            # bucket id
    to: 2              # bucket id
    guards: [ … ]      # optional
```

| Field | Type | Notes |
|---|---|---|
| `from` | int | Must reference an existing `buckets[].id` in the same workflow. |
| `to` | int | Same. |
| `guards` | list | Optional; see [guards.md](guards.md). |

Each `(from, to)` pair must be unique within a workflow. Self-transitions (`from == to`) are never enforced because same-bucket moves are no-ops.

### Guards

Guards live on a transition and run before the move persists. Five types are built in: `blockers_in`, `comments_min`, `comments_tagged`, `wave_gate`, and `subtasks_complete`.

See [guards.md](guards.md) for the full mechanics, validation rules, and worked examples. In summary:

```yaml
guards:
  - type: blockers_in
    buckets: [done]                       # required, non-empty; keys must exist in this workflow
    hint: "Move blockers to Done first."  # optional, surfaced in the error
  - type: comments_min
    count: 1                              # required, >= 1
    hint: "…"
  - type: comments_tagged
    tag: resume                           # required
    count: 1                              # required, >= 1
    hint: "…"
  - type: wave_gate                       # no extra fields; prior-wave pending count is derived
    hint: "Wait for previous waves."
  - type: subtasks_complete               # no extra fields; direct children must be in the final bucket
    hint: "Finish child tasks first."
```

---

## Per-entity wiring

### `skills`

```yaml
skills:        # optional
  - go
  - sqlite
```

When present, **only** the listed slugs activate (strict allowlist). When omitted, every `skills/*.md` file (default + `skills/custom/*.md`) is autoloaded. Each slug must match an existing file (`skills/<slug>.md`); validation fails otherwise.

A skill file's frontmatter (`internal/config/entity_loader.go:skillFrontmatter`):

```markdown
---
name: Go
description: Idiomatic Go for backends
---
Body in markdown — free-form.
```

Required: `name`. Optional: `description`. The `slug` is derived from the filename.

### `laws`

```yaml
laws:          # optional global allowlist
  - workflow-enforced
```

Top-level `laws:` declares **global** laws. Personas may declare per-persona laws and projects may declare per-project laws (`PersonaWiring.Laws`, `ProjectWiring.Laws`). Each slug must resolve to a `laws/<slug>.md` file; a single slug cannot appear in more than one scope (`validateScopeUniqueness`).

Frontmatter (`lawFrontmatter`):

```markdown
---
name: Workflow Enforced
severity: error             # info | warning | error
---
Body…
```

`severity` is required and must be `info`, `warning`, or `error` (`allowedSeverities`).

The scope (`global` / `persona` / `project`) is **not** stored in the file — it is determined by where the slug is referenced (`internal/config/loader_pick.go`).

### `personas`

```yaml
personas:
  - slug: engineer
    skills: [implementation, markdown]  # optional, must be loaded slugs, no duplicates
    laws:   [workflow-enforced]         # optional, must be loaded slugs
```

Persona body (description, free-form notes) lives in `personas/<slug>.md`. The wiring above only declares relationships:

| Field | Type | Notes |
|---|---|---|
| `slug` | string, required | Must match `personas/<slug>.md`. |
| `skills` | list of slugs | Each must resolve to a loaded skill; no duplicates within the persona. |
| `laws` | list of slugs | Each must resolve to a loaded law; the slug cannot also appear at the global or project scope. |

Frontmatter (`personaFrontmatter`):

```markdown
---
name: Backend Agent
description: Owns server-side surface
laws:                       # optional — merged with the persona's wiring laws
  - project-scope-only
---
Persona body…
```

`laws:` declared in the frontmatter is merged with the same persona's `laws:` in the wiring file (union, dedup, frontmatter first). Use this when you want the law binding to live next to the persona authoring file rather than in the active profile yaml.

### `projects`

```yaml
projects:                   # optional declarative wiring
  - slug: omakiten
    name: Omakiten
    description: …
    laws: [project-scope-only]
```

Note: the **runtime** project list lives in SQLite (`UpsertProject`); this section is purely declarative wiring used by validation/loading. Project laws referenced here must resolve to loaded files and not collide with global/persona scopes.

### `templates`

```yaml
templates:    # optional allowlist; otherwise autoload
  - pull-request
  - user-story
```

Template files live in `templates/<slug>.md`. Frontmatter (`templateFrontmatter`):

```markdown
---
name: Pull Request
description: Standard PR scaffold
entity: pr                  # optional, free-form classifier
default: pr                 # optional — must be one of config.template_defaults
project: omakiten           # optional — scopes the default to a single project
laws:                       # optional — laws bound to this template
  - template-fidelity
---
Body — used as the scaffold.
```

Template defaulting rules (`Bundle.TemplateByDefault`, `validateTemplateDefaults`):

- A template with `default: <kind>` activates as the scaffold for that kind.
- `project: <slug>` scopes the default to one project; otherwise it is the global default.
- Project-scoped wins over global when both exist for the same kind.
- At most one template per `(default, project)` pair.
- The same precedence is enforced at read time: `templates.show <global-slug>` from inside a project that shadows the same `default` kind hard-rejects with `validation_error`, naming the active project-scoped slug. Outside any registered project the slug-only lookup is preserved. See [mcp.md § templates](../mcp.md#templates).

Frontmatter `laws:` travels with the template — every law slug must resolve to a loaded law file. When the template is bound to an MCP command via `mcp_commands.<name>.templates`, these laws are folded into the command's effective law set so commands without a dedicated entry (e.g. PR creation flowing through `gh pr create`) still inherit the right guardrails when the template is shown via `templates.show`.

### `mcp_commands`

```yaml
mcp_commands:
  global:                       # reserved key — laws inherited by every okt-* prompt
    laws: [template-fidelity]
  okt-create:
    persona: engineer      # optional — must be a loaded persona slug
    laws: [workflow-enforced]   # optional — added on top of global
    templates: [user-story]     # optional — bound template slugs
  okt-imagine:
    persona: engineer
    laws_disabled:              # optional — opts out of inherited laws
      - template-fidelity
```

Binds each `okt-*` MCP prompt to the persona, laws, and templates the agent receives in the resolved PromptMessage. The reserved `global:` slot supplies laws inherited by every command; per-command entries can declare additional `laws:` or remove inherited ones via `laws_disabled:`. See [guards.md § agent guardrails](guards.md#agent-guardrails-laws-bound-to-commands-and-entities) for the resolution algorithm and worked examples.

| Field | Type | Notes |
|---|---|---|
| `persona` | string | Command persona slug; ignored on the `global` slot. Missing slugs are reported as non-fatal source warnings. |
| `laws` | list of slugs | Added to the effective law set. Missing slugs are reported as non-fatal source warnings. |
| `laws_disabled` | list of slugs | Removed from the effective law set after the union. Cannot overlap with `laws` on the same command. |
| `templates` | list of slugs | Bound template slugs; ignored on the `global` slot. Missing slugs are reported as non-fatal source warnings. |

Validation rejects duplicate slugs in any list and slugs that overlap between `laws` and `laws_disabled` on the same command. Missing persona/law/template references are warnings (`SourceWarning`) rather than hard validation failures so a partially-authored bundle can still load.

---

## Autoload, custom overrides, and slug rules

For each per-entity folder (`skills/`, `laws/`, `personas/`, `templates/`):

- The slug is the filename without the `.md` extension.
- Files at the folder root are the **default** kit.
- Files under `<folder>/custom/` are user-customs and **override** same-slug defaults — the same slug appears once with `is_custom: true` in the loaded bundle.
- When the matching top-level YAML key (`skills`, `laws`, `templates`) is omitted, every file is autoloaded; when present, only listed slugs activate (strict allowlist).
- All four fronts share the same loader skeleton (`LoadSkills` / `LoadLaws` / `LoadPersonas` / `LoadTemplates` in `internal/config/entity_loader.go`).

For the on-disk layout (`<root>/<entity>/` vs `<root>/<entity>/custom/`), see [path-resolution.md § custom shadowing](path-resolution.md#custom-shadowing).

---

## Default kit reference

What ships in `defaults/` and is materialized on first run by `configstore.EnsureDefaultFiles`. Everything below can be overridden by writing to the matching `<root>/<folder>/custom/<slug>.md` (or by editing the file in place — though defaults are overwritten on update, customs are preserved).

### Laws (`defaults/laws/`)

Each law is a single markdown file under `defaults/laws/<slug>.md` with frontmatter (`name`, `severity`, `description?`) and a body. Severity, description, and body are user-tunable per-project once the file lands in `<root>/laws/custom/<slug>.md`; the catalog therefore changes with every fork. Discover the active set with:

```sh
okt law list                 # all loaded laws (slug, severity)
okt law show <slug>          # body + frontmatter
```

Per-preset wiring (which preset binds which law via `mcp_commands.<cmd>.laws`) is captured in the [presets comparison](../presets.md); the YAML under `defaults/config/<preset>.yaml` is the source of truth.

### Skills (`defaults/skills/`)

The default kit ships only project-agnostic skills. Stack-specific skills (Go, Python, SQLite, React, etc.) belong in `<root>/skills/custom/<slug>.md` and are wired per-persona in your local active profile yaml. Discover the active set with:

```sh
okt skill list
okt skill show <slug>
```

Per-persona skill bindings: see [presets.md](../presets.md) for the per-preset comparison, with the YAML as canonical.

### Personas (`defaults/personas/`)

Each persona is a markdown file with a frontmatter `description` + a body declaring the procedural loop. Skill references happen on the wiring side, not on the persona file. Discover the active set with:

```sh
okt persona list
okt persona show <slug>
```

Each preset's `personas:` block in `defaults/config/<preset>.yaml` is the strict allowlist for that preset — entities outside the list still live on disk but are not loaded by that preset. Per-preset wiring tables: [presets.md](../presets.md).

### Templates (`defaults/templates/`)

Each template is a markdown body with frontmatter declaring `entity` (`comment` / `task` / `pr` / `decision` / …) and an optional `default` slot that marks it as the canonical scaffold for its entity kind. Bodies are fetched JIT via the MCP `templates.show` tool — they never ship inline in prompts. Discover the active set with:

```sh
okt mcp call templates.list --input '{}'      # slug + entity + default flag
okt mcp call templates.show --input '{"slug":"<slug>"}'   # body
```

Per-command template bindings: see [presets.md](../presets.md).

### Themes (`defaults/themes/`)

| Key | Vibe |
|---|---|
| `omakiten` | Dark with neon-green accent. The default. |
| `catppuccin-macchiato` | Catppuccin Macchiato — pastels on deep navy. |

See [themes.md](themes.md) for the eight color tokens consumed by the TUI.

### Default workflow (`defaults/config/omakase.yaml`)

Single workflow `omakase` with four buckets — `backlog` → `dev` → `review` → `done` — and tag-anchored guards on every forward edge plus guard-free regression paths (`dev→backlog`, `review→backlog`, `review→dev`, `done→review`, `done→dev`, `done→backlog`). Forward guards include `wave_gate` on `backlog→dev` and `subtasks_complete` on `dev→review`. Full transitions and guards: see `defaults/config/omakase.yaml` or [guards.md § worked example](guards.md#worked-example).

---

## Update when

- A new entity wiring shape lands in `internal/config/loader.go` (new persona/template field, new mcp_commands knob, new workflow operation guard slot).
- A guard type is added/removed (sync with [guards.md](guards.md)).
- The priority/severity registry contract changes (new color token, new default rule).
- Frontmatter required fields shift on any entity (skills/laws/personas/templates).
