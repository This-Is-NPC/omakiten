---
name: Config Orientation
description: Map of where Omakiten config lives, how the active profile is selected, and every field a user can tune to shape their workflow.
entity: orientation
laws:
  - template-fidelity
---
## Path resolution

The active yaml profile is resolved by `internal/paths/paths.go` in this precedence:

1. `--config <path>` flag (CLI / TUI / MCP).
2. `$OMAKITEN_HOME/config/<active>.yaml`.
3. `$XDG_CONFIG_HOME/omakiten/config/<active>.yaml`.
4. `~/.config/omakiten/config/<active>.yaml`.

`<active>` is the basename written in `<config-dir>/.active` (a one-line state file). When `.active` is missing, blank, or names a profile that exists neither at `<config-dir>/<name>.yaml` nor `<config-dir>/custom/<name>.yaml`, the resolver falls through to discovery: first alphabetical `.yaml` at the root, then under `custom/`. A `custom/<name>.yaml` always wins over a same-named root file — that is how a user-authored override shadows the embedded default. The yaml's siblings (the entity folders) are located via the same root regardless of whether the active profile sits at the root or under `custom/`.

## Layout under `<root>`

| Folder | Purpose |
| --- | --- |
| `config/<profile>.yaml` | Workflow profile (write-model). Embedded presets ship here; refreshed on update. |
| `config/custom/<profile>.yaml` | User-authored profile that survives default refreshes. Wins over a same-named root profile. |
| `config/.active` | One-line state file naming the active profile basename. |
| `laws/<slug>.md` | Constraint entities. Frontmatter: `severity` (required, matches `config.severities[].value`). |
| `skills/<slug>.md` | Capability bundles bound to personas. |
| `personas/<slug>.md` | Role bodies. Frontmatter may declare `description`, `skills`, `laws`. |
| `templates/<slug>.md` | Scaffolds. Frontmatter may declare `entity`, `default`, `project`, `laws`. |
| `themes/<slug>.yaml` | TUI palettes (key + name + token table). |
| `notifications/<slug>.yaml` | TUI notification cards (geometry, animation, dismiss policy). |
| `<entity>/custom/` | User-authored overrides under any entity folder. Preserved across kit refreshes. |

User-authored entities under `custom/` shadow same-named defaults at the entity-folder root. A future kit update can ship a new default without overwriting the user's tweak.

## Workflow presets

Four official presets ship in the embedded kit, listed in menu order:

| Slug | Workflow shape | Tone |
| --- | --- | --- |
| `omakase` | backlog → dev → review → done (+ regressions) | Default balanced kit, also the canonical full config. |
| `izakaya` | backlog → dev → done | Minimal; no transition guards. |
| `kaiseki` | requirements → planning → dev → review → docs → done | Formal six-stage flow with handoff guards. |
| `shokunin` | requirements → planning → dev → review → docs → done | Kaiseki + stricter operation guards (peer-review on delete). |

CLI / TUI surface:

- `okt config presets` — list the menu (CLI-only metadata read).
- `okt config validate <path>` — run the strict loader against a yaml without applying it.
- `okt init --preset <name>` — copy the preset's yaml into `<project-root>/.omakiten/config/<name>.yaml` and set it active for that project. Requires `--preset-force` to overwrite.
- **Switch the global active profile** — TUI only today (Settings › Config picker, which writes `<config-dir>/.active`). The CLI accepts a per-invocation override via `--config <path>` or by editing `<config-dir>/.active` directly.

## Entity frontmatter

| Kind | Required | Optional |
| --- | --- | --- |
| Law | `severity` (must appear in `config.severities[].value`) | `name`, `description` |
| Skill | — | `name`, `description` |
| Persona | — | `name`, `description`, `skills`, `laws` |
| Template | — | `name`, `description`, `entity`, `default`, `project`, `laws` |

A template's `default:` value must appear in `config.template_defaults`. Templates without `default:` still load and remain bindable from `mcp_commands` — they just aren't offered in the TUI default-picker. A template with `project: <slug>` scopes the binding to that project, shadowing any global template that claims the same `(default, project=*)` slot.

## Wiring relationships

- **Persona → skills/laws.** Listed in the persona's frontmatter.
- **`mcp_commands.<cmd>` → persona / laws / templates.** Each prompt resolves one persona, the union of its bound laws, and any bound templates.
- **`mcp_commands.global.laws`** — inherited by every command unless opted out via `mcp_commands.<cmd>.laws_disabled`.
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

`workflows[].operations.{archive,delete,unarchive}.guards[]` reuses the same guard shapes above. They gate the corresponding `tasks.archive` / `tasks.delete` / `tasks.unarchive` calls. Archive moves the task into the workflow's final bucket atomically and bypasses both bucket policy and transition guards — only operation guards apply. Unarchive flips state back to `active` while leaving the bucket untouched.

### Task state

Tasks carry `state ∈ {active, archived}`. `domain.TaskFilter.IncludeArchived` defaults to false everywhere, so archived rows are invisible in `tasks.list` / board / table unless explicitly included.

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

## MCP commands

```yaml
mcp_commands:
  global:
    laws: [template-fidelity, authorize-remote-writes]
  okt-implement:
    persona: engineer
    laws: [bounded-self-review, no-silent-behavior-changes, conventional-commits, self-report]
    templates: [pull-request]
  okt-imagine:
    persona: product-owner
    laws_disabled: [template-fidelity]
```

Reserved entry: `global` (only `laws`). Per-command keys: `persona` (slug), `laws` / `laws_disabled` (slug lists), `templates` (slug list).

Known commands (canonical order): `okt`, `okt-imagine`, `okt-create`, `okt-resume`, `okt-continue`, `okt-implement`, `okt-document`, `okt-config`.

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
      filter: { source: [] }                       # subset of [cli, tui, mcp]
    task_activity:
      sort:   { order: asc }                       # asc = chronological, desc = newest first
```

Every block plus its sort `field` / `order` is required — the validator rejects bundles that omit one. Filter blocks default to empty lists meaning "all values allowed"; restrict by listing a subset.

## Other config blocks

| Block | Purpose | Required fields |
| --- | --- | --- |
| `config.output` | CLI JSON shape | `json_minified`, `omit_empty` |
| `config.context` | `context.dump` tunings | `default_level`, `max_tokens` |
| `config.workflow.active` | Which `workflows[].key` is live | string, must match one workflow |
| `config.theme.active` | Which `themes/<slug>.yaml` palette to use | string |
| `config.tui.token_badge` | Token badge thresholds on entity cards | `yellow_at`, `red_at` |
| `config.sqlite.busy_timeout_ms` | `PRAGMA busy_timeout` (ms) | > 0 |
| `config.activity_log` | Per-call `operation` log retention | `max_rows`, `max_age_days` (both > 0) |
| `config.solutions` | `solutions.list_top` MCP caps | `default_top_limit`, `max_top_limit` |
| `config.mcp` | MCP response shape | `recent_comment_limit` (> 0), `max_comment_chars` (≥ 0), `include_workflow_in_continue` (`*bool`), `cache_prompts` (`*bool`), `recent_context_limit`, `next_work_limit`, `similar_task_limit` (all > 0) |
| `config.search.stopwords` | Tokens dropped before similarity scoring | list of lowercase strings |
| `config.tag_synonyms` | `NormalizeTagName` redirect table | `<non-canonical>: <canonical>` map |
| `config.template_defaults` | Allowed values for template frontmatter `default:` | list of kind strings |

Every required field is rejected by the validator if missing — error messages point at the embedded kit (`defaults/omakiten.yaml` in older messages, `defaults/config/omakase.yaml` in the actual file) as the canonical reference. There is no in-code fallback at runtime; the validated bundle is the runtime source.

## Editing a workflow safely

1. **Identify the active file.** Read `<config-dir>/.active` and resolve via the precedence above. Default config-dir: `~/.config/omakiten/config/`.
2. **Copy before editing.** Either copy the profile under `<config-dir>/custom/<name>.yaml` (so a future kit refresh doesn't overwrite it) or override field-by-field inside the existing active file.
3. **Edit and validate.** `okt config validate <path>` runs the same strict parser the runtime uses — missing required fields, dangling refs, unknown event types, and guard typos surface here.
4. **Activate.** TUI: Settings › Config picker. CLI: edit `<config-dir>/.active` to the new basename, or pass `--config <path>` per invocation.
5. **Reload.** CLI / MCP reload on next invocation. The TUI hot-reloads on picker selection; otherwise relaunch.

## Canonical references

For deeper detail, fetch the matching guide:

- `.docs/configuration-guide.md` — every yaml field, semantics, validation rules.
- `.docs/guards-guide.md` — guard kinds, evaluation order, MCP-prompt guardrails.
- `.docs/mcp-guide.md` — MCP tool surface, prompt anatomy, token costs.
- `.docs/data-model-guide.md` — SQLite schema and migration history.
- `.docs/domain-events.md` — `events` table catalog and payload contracts.
