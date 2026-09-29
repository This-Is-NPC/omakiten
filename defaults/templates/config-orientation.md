---
name: Config Orientation
description: Map of where Omakiten config lives, how the active profile is selected, and every field a user can tune to shape their workflow.
entity: orientation
laws:
  - template-fidelity
---
## Path resolution

Precedence (full contract in `.docs/configuration-guide/path-resolution.md`):

1. `--config <path>` flag (CLI / TUI).
2. A discovered project `.omakiten/config.yaml`.
3. `$OMAKITEN_HOME/config.yaml`.
4. `$XDG_CONFIG_HOME/omakiten/config.yaml`.
5. `~/.config/omakiten/config.yaml`.

The selection file references the active snapshot under `<root>/presets/<id>/`.
Installed snapshots are complete packages and run without the source checkout.

## Layout under `<root>`

| Path | Purpose |
| --- | --- |
| `config.yaml` | Active snapshot selection. |
| `presets/<id>/preset.yaml` | Package identity, version, and origin. |
| `presets/<id>/config/preset.yaml` | Entry linking settings, workflows, personas, and bindings. |
| `presets/<id>/config/*.yaml` | Modules that own each configuration section. |
| `presets/<id>/<entity>/<slug>.md` | Entity bodies (laws / skills / personas / templates). |
| `presets/<id>/themes/<slug>.yaml` | Color palettes. |
| `presets/<id>/notifications/<slug>.yaml` | Notification cards. |

## Application languages

User-wide preferences live in the Omakiten configuration root's `preferences.yaml`.
CLI and TUI translations are bundled with Omakiten. Use `okt config language show`,
`set --cli <code> --tui <code> --agent <text>`, or `reset`. These choices apply to all
projects. Workflow snapshots and exports contain workflow settings and entities.

## Workflow presets

The official catalog lists repository packages:

| Slug | Workflow shape | Tone |
| --- | --- | --- |
| `omakase` | backlog → dev → review → done (+ regressions) | Balanced delivery with branch, resume, and review evidence. |
| `izakaya` | backlog → dev → done | Lightweight experiments with hypothesis and wave gates. |
| `kaiseki` | requirements → planning → dev → review → docs → done | Formal six-stage flow with handoff guards. |
| `shokunin` | requirements → planning → dev → review → docs → done | Risk assessment, rollback plans, and dual reviews. |

- `okt preset catalog` — list names, descriptions, and repository URLs.
- `okt preset add <source>` — install a catalog entry, Git URL, or local directory.
- `okt preset use <name-or-id>` — select an installed snapshot.
- `okt config validate <path>` — validate configuration without applying it.
- `okt init --preset <source>` — install and select a project workflow.
- `okt preset export --output workflow.md` — share the complete package in one file.
- `okt preset import --file workflow.md` — install a shared package.

Use `--scope global` with `add`, `use`, `list`, or `import` for user-wide presets.
Edits through Omakiten create a modified `<name>-local` snapshot and retain the
original package. Further edits retain that name.

## Entity frontmatter

| Kind | Required | Optional |
| --- | --- | --- |
| Law | `severity` (must appear in `config.severities[].value`) | `name`, `description` |
| Skill | — | `name`, `description` |
| Persona | — | `name`, `description`, `skills`, `laws` |
| Template | — | `name`, `description`, `entity`, `default`, `project`, `laws` |

A template's `default:` value must appear in `config.template_defaults`. Templates without `default:` still load and remain bindable from `commands` — they just aren't offered in the TUI default-picker. A template with `project: <slug>` scopes the binding to that project, shadowing any global template that claims the same `(default, project=*)` slot.

## Wiring relationships

- **Persona → skills/laws.** Listed in the persona's frontmatter.
- **`commands.<cmd>` → persona / laws / templates.** Each prompt resolves one persona, the union of its bound laws, and any bound templates.
- **`commands.global.laws`** — inherited by every command unless opted out via `commands.<cmd>.laws_disabled`.
- **Effective laws for a command** = `global ∪ persona.laws ∪ command.laws ∪ templates[].laws`, deduped, minus `laws_disabled`.
- A `laws:` or `personas:` block in the profile yaml acts as a strict allowlist (only the listed slugs activate). Omitting the block auto-loads every `.md` under the corresponding folder.

## Workflow shape

Lives under `workflows[]` in the profile yaml.

```yaml
workflows:
  - id: 1
    key: omakase
    name: Omakase Workflow
    defaults:                # optional — workflow-level CRUD fallback
      task:    { edit: false, delete: false }
      comment: { edit: false, delete: false }
    buckets:
      - id: 1
        key: backlog
        name: Backlog
        position: 1
        permissions:         # optional — per-bucket CRUD override
          task:    { edit: true, delete: true }
          comment: { edit: true, delete: true }
      - { id: 2, key: dev,    name: Development, position: 2 }
      - { id: 3, key: review, name: Review,      position: 3 }
      - { id: 4, key: done,   name: Done,        position: 4 }
    transitions:
      - from: 1
        to: 2
        guards:
          - { type: comments_tagged, tag: self-branch, count: 1, hint: "..." }
      - { from: 2, to: 1 }   # regression — no guard
    operations:              # optional — non-flow guards
      delete:
        guards:
          - { type: comments_tagged, tag: peer-review, count: 1 }
```

Bucket identity is `key` (stable); `id` is the wire integer used by `transitions.from/to`. `position` orders buckets in the board view.

### Permissions resolution

For each (operation, bucket) pair the resolver walks:

1. `bucket.permissions.<task|comment>.<edit|delete>` — the per-bucket override.
2. `workflows[].defaults.<task|comment>.<edit|delete>` — workflow-level fallback.
3. Implicit `true` — no rule declared anywhere = allowed.

`comment` inherits from `task` field-by-field at every layer: declaring only `task.edit: false` denies edit on both task and comments unless `comment.edit` is set explicitly at the same or a deeper layer.

### Transition guards

Three kinds, all evaluated after the transition is allowed:

| `type` | Required fields | Meaning |
| --- | --- | --- |
| `comments_tagged` | `tag`, `count` (≥1) | The task must carry ≥ count comments tagged `#<tag>`. |
| `comments_min` | `count` (≥1) | The task must carry ≥ count total comments (any tag). |
| `blockers_in` | `buckets` (list of bucket keys) | Every dependency of the task must sit in one of the listed buckets. |

All accept `hint` (string shown in the `guard.violated` event when the guard fails). The first failing guard short-circuits with a `guard_violation` domain error carrying `operation`, `rule`, `hint`, `target`, `attempted_by`.

### Operation guards

`workflows[].operations.{archive,delete,unarchive}.guards[]` reuses the same guard shapes above. They gate the corresponding `okt archive` / `okt delete` / `okt unarchive` calls. Archive moves the task into the workflow's final bucket atomically and bypasses both bucket policy and transition guards — only operation guards apply. Unarchive flips state back to `active` while leaving the bucket untouched.

### Task state

Tasks carry `state ∈ {active, archived}`. `domain.TaskFilter.IncludeArchived` defaults to false everywhere, so archived rows are invisible in `okt list` / board / table unless explicitly included.

## Configurable enums

```yaml
config:
  priorities:
    - { id: 1, value: low,    color: success }
    - { id: 2, value: normal, default: true, color: info }
    - { id: 3, value: high,   color: error }
  severities:
    - { id: 1, value: info,    color: info }
    - { id: 2, value: warning, default: true, color: warning }
    - { id: 3, value: error,   color: error }
```

- `id` is the sort weight; list ascending.
- `value` is the label rendered by the TUI / CLI / agent DTOs.
- At most one entry may set `default: true` (consumed by `WorkflowService.CreateTask` and `LawService.Add` when the caller omits the field).
- `color` is an optional theme token name (`success | info | warning | error`) used by the badge renderer.
- Storage is by integer id, so renaming a label is a one-line edit; existing tasks / laws keep their stored id.

## CLI commands

```yaml
commands:
  global:
    laws: [template-fidelity, authorize-remote-writes]
  okt-task-implement:
    persona: builder
    laws: [bounded-self-review, no-silent-behavior-changes, conventional-commits, self-report]
    templates: [pull-request]
  okt-task-imagine:
    persona: planner
    laws_disabled: [template-fidelity]
```

Reserved entry: `global` (only `laws`). Per-command keys: `persona` (slug), `laws` / `laws_disabled` (slug lists), `templates` (slug list).

Known commands and tiers live in `.docs/command-surface.md`; canonical order comes from `internal/agent/command_table.go`. Current examples use the v2 namespaced surface (`okt-start`, `okt-shape`, `okt-run`, `okt-task-create`, `okt-task-implement`, `okt-task-review`, `okt-task-check`, `okt-pause`).

## Hooks and notifications

```yaml
config:
  hooks:
    - on: task.created
      notification: kitten_informative
      message: "A new quest has appeared."
    - on: guard.violated
      when: { operation: task.delete }
      notification: kitten_destructive
      detail_message_field: hint
    - on: error.recorded
      do: exec                       # action shape (alternative to notification:)
      args:
        argv: ["./scripts/log-error.sh"]
        timeout_ms: 3000
```

Each hook fires when `event_type == on` and every key in `when` matches the corresponding top-level key in `event.payload` (string equality; numbers/bools coerce). Two mutually-exclusive shapes:

- **Notification shape**: `notification: <slug>` references `notifications/<slug>.yaml`. Optional `message` / `message_field` / `detail_message` / `detail_message_field` provide fallbacks when the notification YAML doesn't set its own.
- **Action shape**: `do: <action_name>` + `args:`. Built-in actions: `exec` (runs `args.argv`, full event lands on stdin as JSON, `args.timeout_ms` caps runtime), `noop`.

A `notifications/<slug>.yaml` declares the card's geometry, border / background style, animation frames, footer / bubble position, and `dismiss` policy (`mode: timeout|manual`, `after_ms`, `keys`).

Mutating `config.hooks` requires restarting the app — the bundle is read once at startup.

## Events policy

```yaml
config:
  events:
    default_recent_limit: 50
    defaults: { log: true, broadcast: true, hook: true }
    overrides:
      tag.added:   { log: false }
      tag.removed: { log: false }
```

Per-event-type tri-channel policy:

- `log` — persist the row to the `events` table.
- `broadcast` — fan the event out to in-process subscribers.
- `hook` — dispatch to the YAML-driven hooks engine.

Overrides inherit any unset channel from `defaults` (pointer semantics: omit = inherit, declare `false` to opt out). Override keys must be in `domain.KnownEventTypes`; unknown keys fail at load time.

`default_recent_limit` is the fallback row count for `Store.ListRecentEvents` when callers pass `<= 0`.

## Views

```yaml
config:
  views:
    board:
      sort:   { field: created_at, order: desc }   # id | title | priority | created_at
      filter: { priority: [] }                     # subset of config.priorities[].value; [] = all
    table:
      sort:   { field: created_at, order: desc }
      filter: { priority: [], bucket: [] }         # bucket = subset of bucket keys
    graph:
      sort:   { field: id, order: asc }            # id | title only
    logs:
      sort:   { order: desc }                      # direction only
      limit:  50
      window_days: 30                              # display horizon in days
    task_activity:
      sort:   { order: asc }                       # asc = chronological, desc = newest first
```

Every block plus its sort `field` / `order` is required — the validator rejects bundles that omit one. Filter blocks default to empty lists meaning "all values allowed"; restrict by listing a subset.

## Other config blocks

| Block | Purpose | Required fields |
| --- | --- | --- |
| `config.output` | CLI JSON shape | `json_minified`, `omit_empty` |
| `config.workflow.active` | Which `workflows[].key` is live | string, must match one workflow |
| `config.theme.active` | Which `themes/<slug>.yaml` palette to use | string |
| `config.tui.token_badge` | Token badge thresholds on entity cards | `yellow_at`, `red_at` |
| `config.sqlite.busy_timeout_ms` | `PRAGMA busy_timeout` (ms) | > 0 |
| `config.events.retention` | Storage caps for persisted event rows | `defaults.{max_age_days,max_rows}` (both >= 0); optional `by_category` / `overrides` |
| `config.solutions` | `okt solution list-top` CLI caps | `default_top_limit`, `max_top_limit` |
| `config.agent` | CLI response shape | `recent_comment_limit` (> 0), `max_comment_chars` (≥ 0), `include_workflow_in_continue` (`*bool`), `next_work_limit`, `similar_task_limit` (both > 0) |
| `config.search.stopwords` | Tokens dropped before similarity scoring | list of lowercase strings |
| `config.tag_synonyms` | `NormalizeTagName` redirect table | `<non-canonical>: <canonical>` map |
| `config.template_defaults` | Allowed values for template frontmatter `default:` | list of kind strings |

Every required field is rejected by the validator if missing — error messages point at the embedded canonical kit (`defaults/config/omakase.yaml`). There is no in-code fallback at runtime; the validated bundle is the runtime source.

## Editing a workflow safely

1. **Identify the selection.** Use `okt config show` and `okt preset list`.
2. **Edit the active package.** CLI and Studio edits create an independent
   modified snapshot. Repository edits belong in the module that owns the field.
3. **Validate.** Run `okt config validate` for schema and reference checks.
4. **Select.** Use `okt preset use <name-or-id>` for an installed package.
5. **Share.** Export the complete preset with `okt preset export --output workflow.md`.

## Canonical references

For deeper detail, fetch the matching guide:

- `.docs/configuration-guide/README.md` — map of modular config guides.
- `.docs/configuration-guide/workflows.md` — workflow buckets, transitions, permissions, and operation guards.
- `.docs/configuration-guide/guards.md` — guard kinds, evaluation order, and failure payloads.
- `.docs/configuration-guide/command-bindings.md` — `commands` persona/law/template bindings.
- `.docs/agents.md` — CLI tool surface and prompt anatomy.
- `.docs/internal/data-model.md` — current SQLite schema baseline and the unified `events` log.
