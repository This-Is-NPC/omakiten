# How to Configure the Active Profile YAML

The active profile yaml is the canonical source of truth: every field below is parsed by `internal/config/loader.go` and validated by `internal/config/validator.go`, then `config.BuildSnapshot(bundle)` materialises the immutable `*config.Snapshot` every app service reads through. Migration 020 dropped every SQL config table; the runtime no longer mirrors the bundle into SQLite — only the `bundle.imported` audit event is recorded. YAML decoding uses `KnownFields(true)`, so unknown fields fail loud rather than silently. The embedded canonical kit ships as `defaults/config/omakase.yaml`; it is materialized into the user's `<config-dir>/` on first run alongside the other official presets (`izakaya.yaml`, `kaiseki.yaml`, `shokunin.yaml`).

The active runtime path is, in precedence (`internal/paths/paths.go`):

1. `--config <path>` flag (CLI/TUI/MCP).
2. `$OMAKITEN_HOME/config/<active>.yaml`.
3. `$XDG_CONFIG_HOME/omakiten/config/<active>.yaml`.
4. `~/.config/omakiten/config/<active>.yaml`.

`<active>` is the basename written in `<config-dir>/.active` (a one-line state file). The resolver prefers `<config-dir>/custom/<name>.yaml` over `<config-dir>/<name>.yaml`, so a user-authored override at `custom/` shadows the embedded default. When `.active` is missing, blank, or names a profile that exists in neither location, the resolver falls through to discovery: first alphabetical `.yaml` at the root, then under `custom/`. This degrades cleanly when the canonical kit is renamed or removed across releases.

The yaml lives under `<root>/config/`; per-entity folders are siblings of `config/`, not nested inside it. See **Paths and backups** at the bottom for the full layout. The default kit (`defaults/`) is materialized into the entity folders on first run by `configstore.EnsureDefaultFiles`; legacy flat layouts are auto-migrated by `configstore.MigrateLayout`. The composition roots (`internal/cli/root.go:open`, `internal/agentruntime/runtime.go:Open`) run `MigrateLayout` + `EnsureDefaultFiles` **before** resolving the active yaml, so a profile relocated into `custom/` during the same boot is honored.

---

## Top-level shape

```yaml
version: 1
kit: { id, key, name }
config: { … }
workflows: [ … ]
skills:       [ <slug>, … ]    # optional allowlist
laws:         [ <slug>, … ]    # optional allowlist
templates:    [ <slug>, … ]    # optional allowlist
personas:     [ { slug, skills?, laws? }, … ]
projects:     [ { slug, name, description?, laws? }, … ]
mcp_commands: { <slug>: { persona?, laws?, laws_disabled?, templates? } }
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `version` | int | yes | Must be exactly `1`. Anything else fails validation. |
| `kit` | object | yes | See **kit**. |
| `config` | object | yes | See **config**. |
| `workflows` | list | yes | At least one. See **workflows**. |
| `skills` / `laws` / `templates` | list of slug strings | no | Strict allowlist when present; **autoload** otherwise. |
| `personas` | list of `PersonaWiring` | no | Persona wiring (skill/law refs); body lives in `personas/<slug>.md`. |
| `projects` | list of `ProjectWiring` | no | Declarative project wiring; the runtime project list is in SQLite. |
| `mcp_commands` | map | no | Binds `okt-*` MCP prompts to a persona, laws, and templates. See **`mcp_commands`**. |

Two additional inputs are loaded from sibling folders rather than `omakiten.yaml` top-level keys: `notifications/<slug>.yaml` (kit-wide notification cards referenced from `config.hooks`) and `languages/<code>.yaml` (CLI/TUI language packs picked via `config.languages.{cli,tui}`). They appear on the in-memory `Bundle` as `Notifications` and `Languages`, are validated alongside the YAML, and ship under `defaults/notifications/` and `defaults/languages/`.

### `kit`

```yaml
kit:
  id: 101                       # int, > 0, required
  key: omakase                  # string, required
  name: Omakase Workflow Preset # string, required
```

`kit` identifies the bundle distribution. All three fields are required (`requireKitFields` → `requireIDKeyName`).

---

## `config`

```yaml
config:
  output:
    json_minified: true       # bool
    omit_empty:    true       # bool
  context:
    default_level: 2          # int, 1..3
    max_tokens:    12000      # int, >= 0
  workflow:
    active: omakase           # string, required; must match workflows[].key
  theme:
    active: omakiten          # string, required; must match a themes/<key>.yaml file
  template_defaults: [task, pr, comment-resume, comment-selfbranch]
  views: { … }
```

### `config.output`

| Field | Type | Effect |
|---|---|---|
| `json_minified` | bool | When true, CLI envelopes are emitted as a single line (`internal/output/json.go`). |
| `omit_empty` | bool | Drop empty/zero fields from the JSON envelope. |

### `config.context`

| Field | Type | Range | Effect |
|---|---|---|---|
| `default_level` | int | 1, 2, or 3 | Default level for `context.dump` when the caller omits one. Levels: **1** = context entries only · **2** adds workflow + tasks + dependencies · **3** adds comments + active laws (`internal/app/context_service.go`). |
| `max_tokens` | int | `>= 0` | Token budget for `context.dump`. Truncation is newest-first; the response sets `truncated: true` once the budget is exceeded (`internal/app/context_service.go:contextBudget`). |

### `config.workflow`

| Field | Required | Effect |
|---|---|---|
| `active` | yes | Selects which `workflows[].key` is currently active. Must match an entry; otherwise validation fails with `config.workflow.active "<x>" does not match any workflow`. |

### `config.theme`

| Field | Required | Effect |
|---|---|---|
| `active` | yes | Theme key — must match a `themes/<key>.yaml` file. The bundled defaults are `omakiten` and `catppuccin-macchiato`. Custom themes go in `themes/custom/<key>.yaml` (preserved across kit refreshes). |

Theme files are validated separately (`ValidateTheme`): `version: 1`, non-empty `key`, `name`, and `colors`. See `defaults/themes/omakiten.yaml` for the canonical color keys (`background`, `foreground`, `primary`, `secondary`, `success`, `warning`, `error`, `border`, `highlight`).

### `config.template_defaults`

```yaml
template_defaults: [task, pr, comment-resume, comment-selfbranch]
```

The list of "kinds" that templates may claim as their `default:` slot. When omitted, falls back to the canonical default set (`config.DefaultTemplateKinds`).

A template `.md` whose frontmatter declares `default: <kind>` activates as the scaffold for that kind. The validator enforces:

- Every template's `default:` value must be in `template_defaults` (otherwise `default %q is not in config.template_defaults`).
- At most one template per `(default, project)` pair (`only one may`).

The TUI's template-default picker offers exactly the kinds in this list.

### `config.mcp`

Tunes how MCP responses are shaped to fit the agent's context window. **Every field is required** — the validator rejects bundles missing any. The canonical values live in `defaults/config/omakase.yaml` (the embedded kit YAML the installer materialises into the user's config root); your local file inherits at install time. Customise by editing values, never by removing fields.

Pointer booleans (`*bool`) require an explicit `true` or `false` — there is no "not declared" state.

```yaml
config:
  mcp:
    recent_comment_limit: 5            # int >0
    max_comment_chars: 0               # int >=0; 0 = no truncation
    include_workflow_in_continue: true # bool; required
    cache_prompts: true                # bool; required
    recent_context_limit: 3            # int >0
    next_work_limit: 5                 # int >0
    similar_task_limit: 5              # int >0
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `recent_comment_limit` | int | `> 0` | Caps how many recent comments tools like `tasks.continue` and `project.overview` ship per call. Reverse-chronological — the most recent N. |
| `max_comment_chars` | int | `>= 0` | Truncates comment bodies past this many runes when shipped over MCP, appending `…`. Zero disables truncation. Use to bound `tasks.continue` payloads on tasks with verbose `#resume` and `#documentation` comments. Does not affect `comments.list` (which is the read-the-full-thread endpoint). |
| `include_workflow_in_continue` | `*bool` | required | Toggles the `workflow` block in `tasks.continue` responses. Per-call `include_workflow` argument overrides this default — set false once `/okt` already loaded the workflow shape for the session. |
| `cache_prompts` | `*bool` | required | Emits a `_meta.anthropic.cache_control` hint on `prompts/get` content. Anthropic-aware MCP clients (recent Claude Code) reuse the cached prompt across calls; unaware clients ignore the hint silently. Disable only to work around a misbehaving client. |
| `recent_context_limit` | int | `> 0` | Caps how many recent `context.add` entries flow into `tasks.continue` / `project.overview` / `project.resume`. Smaller than `recent_comment_limit` because each entry can be paragraphs of free-form notes. |
| `next_work_limit` | int | `> 0` | Caps the "likely next work" suggestion list shipped in `project.resume`. Increase for project-overview screens; keep small for narrow agent contexts. |
| `similar_task_limit` | int | `> 0` | Caps how many similar-task hints `tasks.create_intent` surfaces during the dedup check. Tune up if you frequently create near-duplicate intents and want broader dedup coverage. |

#### Validation rules (parse-time)

Every field above is required and validated. Missing or out-of-range values fail loud with messages pointing back at `defaults/config/omakase.yaml` so the fix is obvious.

| Rule | Error message shape |
|---|---|
| `recent_comment_limit <= 0` | `config.mcp.recent_comment_limit: must be > 0 (see defaults/config/omakase.yaml for canonical values)` |
| `max_comment_chars < 0` | `config.mcp.max_comment_chars: must be >= 0 (0 = no truncation)` |
| `include_workflow_in_continue` omitted | `config.mcp.include_workflow_in_continue: required boolean (see defaults/config/omakase.yaml)` |
| `cache_prompts` omitted | `config.mcp.cache_prompts: required boolean (see defaults/config/omakase.yaml)` |
| Same shape for `recent_context_limit / next_work_limit / similar_task_limit`. |

#### Worked example — taming a long-lived task

A task with many long `#resume` comments dominates every `tasks.continue` call: each comment ships in full, the workflow block ships once, and a fresh task vs. a year-old task differ by an order of magnitude in payload size.

Two settings collapse the bulk:

```yaml
config:
  mcp:
    recent_comment_limit: 3   # 5 → 3
    max_comment_chars: 500    # 0 → 500
```

`recent_comment_limit: 3` keeps the most recent three; `max_comment_chars: 500` hard-caps each body. Add `include_workflow_in_continue: false` once `/okt` has loaded the workflow in the session to drop the per-call workflow block too. The exact byte saving depends on each comment's length and the active workflow's shape — the qualitative effect is "load only what is new since last call".

Cross-reference: `.docs/mcp-guide.md#tuning-context-cost` walks through how the tool result composes and which fields each setting trims.

### `config.tui`

Tunes terminal UI presentation. **Every field is required** (validator rejects omissions). Canonical values come from `defaults/config/omakase.yaml`.

```yaml
config:
  tui:
    token_badge:
      yellow_at: 150   # int >=0; 0 keeps default
      red_at:    400   # int >=0; 0 keeps default
```

`token_badge` drives the colored `TOKENS:N` badge on entity cards (laws, personas, skills, templates). Above `red_at` → red; above `yellow_at` → yellow; else green. Token counts use the same approximation as the right-rail token budget panel, so tuning here matches what the rail shows.

| Field | Type | Default | What it does |
|---|---|---|---|
| `yellow_at` | int | `150` | Threshold above which the badge turns yellow. Calibrated for the default kit: most laws land in the 70–190 token range with their few-shot examples, so 150 keeps the green band signal-rich rather than uniformly yellow. |
| `red_at` | int | `400` | Threshold above which the badge turns red. Raise if you author heavy laws/personas; lower if you keep entities terse. |

Non-positive values fall back to the canonical defaults silently.

### `config.priorities`

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

#### Validation rules (parse-time)

| Rule | Error message shape |
|---|---|
| `id` <= 0 | `config.priorities: id must be positive, got 0 for value "low"` |
| missing `value` | `config.priorities[id=1]: value is required` |
| duplicate `id` | `config.priorities: id 1 declared twice (values "low" and "...")` |
| duplicate `value` | `config.priorities: value "low" declared twice (ids 1 and 4)` |
| more than one `default: true` | `config.priorities: at most one entry may set default: true (got 2)` |

#### How the runtime wires it

`app.ConfigService.Import` (called from both composition roots — `internal/cli/root.go` and `internal/agentruntime/runtime.go`) returns a `*domain.EnumRegistry` alongside the bundle, between `LoadBundle` (validate) and `config.BuildSnapshot` (materialise the immutable per-project snapshot). Composition roots inject the registry into every service that resolves labels (`TaskService`, `LawService`, `WorkflowService`, `TUIQueryService`, `ConfigService`, `ContextService`, agent `Service`); the TUI Model builds its own copy from the priorities/severities slices it already receives. There are no process-global enum tables — every lookup goes through the injected registry via `registry.PriorityLabel(id)`, `registry.PriorityFromLabel("high")`, `registry.DefaultPriority()`, etc. JSON wire format of `domain.Priority` / `domain.Severity` is the raw int id (no `MarshalJSON`); label projection happens at DTO boundaries (e.g. `agent.TaskSummary.Priority` is the label string). Tests construct a fresh registry via `testfixtures.CanonicalRegistry()` and thread it through service constructors.

#### Worked example — adding an "urgent" priority

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

### `config.severities`

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

### `config.views`

Per-view defaults seeded into the TUI on startup. Every field is optional; omitted values fall back to canonical defaults via `Settings.EffectiveViews()` (`internal/config/bundle.go`).

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
    sort:   { order: desc }                           # field is rejected — only direction is configurable
    limit:  50                                        # int, >= 0
    filter: { source: [] }                            # subset of [cli, tui, mcp]
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
| `logs.limit` | int `>= 0` | `50` |
| `logs.filter.source` | subset of `cli`, `tui`, `mcp` | `[]` (no filter) |
| `task_activity.sort.order` | `asc`, `desc` (no `field`) | `asc` |
| `*.filter.priority` | subset of `low`, `normal`, `high` | `[]` |
| `table.filter.bucket` | subset of bucket keys in the active workflow | `[]` |

Typos surface with errors like `config.views.board.sort.field "creatd_at" is not one of [id title priority created_at]` rather than being silently ignored.

### `config.sqlite`

Connection-level engine knobs the Store applies at Open. **Required block** — the kit ships the canonical `busy_timeout` the user inherits at install time. Other PRAGMAs (`foreign_keys=ON`, `journal_mode=WAL`, `synchronous=NORMAL`) intentionally stay in code: they encode the engine-level contract Omakiten depends on, not user preference.

```yaml
config:
  sqlite:
    busy_timeout_ms: 5000     # int >0; PRAGMA busy_timeout
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `busy_timeout_ms` | int | `> 0` | Sets `PRAGMA busy_timeout`. Larger DBs or systems with concurrent writers (TUI + MCP server sharing a Store) may need a higher value to avoid `database is locked` errors. |

### `config.activity_log`

Retention window for the per-call `operation` event log used by the activity feed. **Required block** — disabling retention is not a supported mode (the activity log would grow unbounded). Each `BeginActivityLog` runs a synchronous prune pass after insert; the writer never blocks longer than O(rows-deleted).

```yaml
config:
  activity_log:
    max_rows: 500             # int >0
    max_age_days: 7           # int >0
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `max_rows` | int | `> 0` | Hard ceiling on `operation` rows. Older rows are deleted in id-DESC order after every insert. |
| `max_age_days` | int | `> 0` | Sliding window — rows older than this many days are deleted on every insert. |

### `config.solutions`

Caps the `solutions.list_top` MCP response shape. **Required block** — `default_top_limit` applies when the caller passes `<=0`; `max_top_limit` clamps caller-supplied limits so MCP responses stay bounded regardless of what the agent asks for.

```yaml
config:
  solutions:
    default_top_limit: 10     # int >0
    max_top_limit: 100        # int >= default_top_limit
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `default_top_limit` | int | `> 0` | Limit applied when the caller omits one. |
| `max_top_limit` | int | `>= default_top_limit` | Hard cap on caller-supplied limits. Validator rejects inverted ranges so the runtime always has a sane window. |

### `config.events`

Fallback recent-events limit used by `Store.ListRecentEvents` when callers pass `<=0`. **Required block.**

```yaml
config:
  events:
    default_recent_limit: 50  # int >0
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `default_recent_limit` | int | `> 0` | Fallback row count applied when the caller passes `<=0`. The query is indexed on `(event_type, created_at, id)` so larger values are cheap. |

### `config.search`

Tunes text-similarity heuristics shared across agent-side ranking (similar-task hints in `tasks.create_intent`, query overlap scoring). **Required block** — multilingual users add Portuguese/Spanish/etc. words here without a code change.

```yaml
config:
  search:
    stopwords: [and, are, for, from, into, the, this, that, with]
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `stopwords` | list of strings | non-empty, lowercase, unique | Tokens dropped before computing overlap scores. Validator rejects empties, duplicates, and uppercase entries (must match the tokenizer's lowercased output). |

### `config.tag_synonyms`

Maps non-canonical tag slugs to their canonical form. **Required block.** `NormalizeTagName` collapses input → kebab-case, then applies one hop of substitution (no chains). Edit this block to align tag names with your team's vocabulary; existing rows survive because storage is by tag id, so renaming the canonical label here re-routes future writes without touching history.

```yaml
config:
  tag_synonyms:
    golang: go
    javascript: js
    typescript: ts
    nodejs: node
    node-js: node
    postgres: postgresql
    psql: postgresql
    mongo: mongodb
    k8s: kubernetes
    tf: terraform
    py: python
```

Validation rules:

| Rule | Error message shape |
|---|---|
| empty key or value | `config.tag_synonyms: empty key` / `config.tag_synonyms[<key>]: empty target` |
| self-loop (`go: go`) | `config.tag_synonyms[<key>]: maps to itself` |
| two-hop chain (target is also a key) | `config.tag_synonyms[<key>]: target <value> is itself a key (two-hop chains are not resolved)` |

The runtime applies one substitution; `golang` → `go` works, but if you also declared `go: golang` (which the validator catches anyway as a self-loop pair), the runtime would not chase the chain.

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
| `defaults` | object, optional | Workflow-level fallback for task / comment edit / delete. See **Workflow defaults**. |
| `buckets` | list, non-empty | See **buckets**. |
| `transitions` | list, optional | See **transitions**. Empty list = no moves allowed. |
| `operations` | object, optional | Guards for archive / delete / unarchive. See **workflows[].operations**. |

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
| `permissions` | object, optional | Per-bucket task / comment CRUD policy. See **Bucket permissions** below. |

#### Bucket permissions

`permissions` gates `tasks.edit`, `tasks.delete`, `comments.edit`, and `comments.delete` based on the bucket the task currently sits in. When the field is omitted at the bucket layer the resolver falls through to `workflow.defaults` and then to the implicit `true` — see **Workflow defaults** above for the full chain.

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
| `guards` | list | Optional; see **Guards** below. |

Each `(from, to)` pair must be unique within a workflow. Self-transitions (`from == to`) are never enforced because same-bucket moves are no-ops.

### Guards

Guards live on a transition and run before the move persists. Three types: `blockers_in`, `comments_min`, `comments_tagged`.

See **`.docs/guards-guide.md`** for the full mechanics, validation rules, and worked examples. In summary:

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
- The same precedence is enforced at read time: `templates.show <global-slug>` from inside a project that shadows the same `default` kind hard-rejects with `validation_error`, naming the active project-scoped slug. Outside any registered project the slug-only lookup is preserved. See `.docs/mcp-guide.md` §Templates.

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

Binds each `okt-*` MCP prompt to the persona, laws, and templates the agent receives in the resolved PromptMessage. The reserved `global:` slot supplies laws inherited by every command; per-command entries can declare additional `laws:` or remove inherited ones via `laws_disabled:`. See `.docs/guards-guide.md#agent-guardrails-laws-bound-to-commands-and-entities` for the resolution algorithm and worked examples.

| Field | Type | Notes |
|---|---|---|
| `persona` | string | Must resolve to a loaded persona; ignored on the `global` slot. |
| `laws` | list of slugs | Added to the effective law set; each slug must resolve to a loaded law. |
| `laws_disabled` | list of slugs | Removed from the effective law set after the union. Cannot overlap with `laws` on the same command. |
| `templates` | list of slugs | Bound template slugs; each must resolve to a loaded template; ignored on the `global` slot. |

Validation rejects duplicate slugs in any list, slugs that overlap between `laws` and `laws_disabled`, and any reference that does not match a loaded entity.

---

## How config reads work at runtime (in-memory providers + per-project cache)

Phase 2 of the config refactor dropped every SQL config table — workflows, workflow_buckets, workflow_transitions, personas, persona_skills, skills, laws, settings, config_bundles (migration 020). Phase 2-bis then stripped every config-side method from the SQL adapter so `*sqlite.Store` carries zero `config.Bundle` / `config.Snapshot` references in production. The bundle YAML is the single source of truth; reads land on an immutable `*config.Snapshot` materialised by `config.BuildSnapshot(bundle)`. Every app service captures the `*config.Snapshot` pointer at construction; the pointer never mutates after build, so concurrent readers always see a consistent shape — hot-reload installs a new pointer in a new `*ProjectRuntime` entry rather than mutating the live one.

Phase 3 layered per-project bundles on top: `agentruntime.BundleCache` holds one `*ProjectRuntime` per project id. Each entry aggregates the per-project `*config.Snapshot`, an `agent.Service` (which holds the same Snapshot via `SetSnapshot`), a `hooks.Engine`, the action registry, the notification snapshot, and the enum registry built from THAT project's YAML. Cache rebuilds happen automatically on mtime change (every Resolve stat-checks the SourcePath) or explicitly via `Reload` (TUI Settings → Config picker). MCP `Adapter.CallTool` peeks `project` / `project_id` from incoming args and routes the dispatch against the matching entry; calls without those args fall back to the default project resolved at boot.

Hot-reload of the active YAML no longer touches SQLite — `cache.Reload` re-parses, runs the validator, calls `config.BuildSnapshot`, installs the fresh `*ProjectRuntime` (carrying the new Snapshot pointer plus the previous one for the orphan flow), stops the prior engine, and emits `bundle.imported` for audit via `Store.RecordEntityEvent` from the composition root. In-flight callers that captured the prior Snapshot pointer keep reading the old shape until they release it. Validator rejection leaves the previous entry in place.

## Autoload, custom overrides, and slug rules

For each per-entity folder (`skills/`, `laws/`, `personas/`, `templates/`):

- The slug is the filename without the `.md` extension.
- Files at the folder root are the **default** kit.
- Files under `<folder>/custom/` are user-customs and **override** same-slug defaults — the same slug appears once with `is_custom: true` in the loaded bundle.
- When the matching top-level YAML key (`skills`, `laws`, `templates`) is omitted, every file is autoloaded; when present, only listed slugs activate (strict allowlist).
- All four fronts share the same loader skeleton (`LoadSkills` / `LoadLaws` / `LoadPersonas` / `LoadTemplates` in `internal/config/entity_loader.go`).

---

## Validation summary

`okt config validate` (and every bundle import) runs `ValidateBundle` (`internal/config/validator.go`). Common failure shapes:

| Cause | Error |
|---|---|
| `version != 1` | `version must be 1` |
| Missing `kit.id` / `kit.key` / `kit.name` | `kit.id must be positive` / `kit.key is required` / `kit.name is required` |
| Bad context level | `config.context.default_level must be between 1 and 3` |
| Negative `max_tokens` | `config.context.max_tokens cannot be negative` |
| Empty `config.workflow.active` | `config.workflow.active is required` |
| `active` not found | `config.workflow.active "<x>" does not match any workflow` |
| Duplicated workflow / bucket id or key | `workflows has duplicated id <n>` / `… duplicated key "<k>"` |
| Bucket position `< 1` | `workflows.<wf>.buckets.<key>.position must be positive` |
| Transition referencing missing bucket id | `workflows.<wf> transitions from missing bucket id <n>` |
| Duplicated transition pair | `workflows.<wf> has duplicated transition <a> -> <b>` |
| Unknown guard type / bad guard payload | See **`.docs/guards-guide.md`** §"Validation rules". |
| Reference to a non-existent skill/law/persona/template | `<section>: ref "<slug>" has no matching file` |
| Law in two scopes | `laws.<slug> declared in multiple scopes (<a> and <b>)` |
| Bad template `default` | `templates.<slug>: default "<kind>" is not in config.template_defaults` |
| Two templates claiming the same `(default, project)` | `templates.<a> and templates.<b> both declare default="<kind>" (<scope>)` |
| Bad view sort/filter | `config.views.<view>.* "<v>" is not one of [...]` |
| Project laws referencing a non-existent law | `projects.<slug> laws: ref "<slug>" has no matching law file` |
| Missing/zero `config.sqlite.busy_timeout_ms` | `config.sqlite.busy_timeout_ms: must be > 0 (see defaults/config/omakase.yaml)` |
| Missing/zero `config.activity_log.{max_rows, max_age_days}` | `config.activity_log.max_rows: must be > 0 (see defaults/config/omakase.yaml)` |
| Missing/zero `config.solutions.*` or inverted range | `config.solutions: max_top_limit (<n>) must be >= default_top_limit (<n>)` |
| Missing/zero `config.events.default_recent_limit` | `config.events.default_recent_limit: must be > 0 (see defaults/config/omakase.yaml)` |
| Empty/uppercase/duplicate `config.search.stopwords` | `config.search.stopwords: entry "X" must be lowercase (matching tokenizer output)` |
| Empty / self-loop / two-hop `config.tag_synonyms` | `config.tag_synonyms[<key>]: target "<v>" is itself a key (two-hop chains are not resolved)` |

All errors are returned as plain Go errors in CLI flows (rendered through the JSON envelope) and as `config_invalid` coded errors when surfaced through the agent layer (`internal/domain/errors.go`).

---

## Worked example (annotated)

```yaml
version: 1

kit:
  id: 101
  key: omakase
  name: Omakase Workflow Preset

config:
  output:    { json_minified: true, omit_empty: true }
  context:   { default_level: 2, max_tokens: 12000 }
  workflow:  { active: omakase }
  theme:     { active: omakiten }
  template_defaults: [task, pr, comment-resume, comment-selfbranch]
  views:
    board: { sort: { field: created_at, order: desc }, filter: { priority: [high, normal] } }
    table: { sort: { field: title,      order: asc  } }
    logs:  { limit: 100 }
  sqlite:       { busy_timeout_ms: 5000 }
  activity_log: { max_rows: 500, max_age_days: 7 }
  solutions:    { default_top_limit: 10, max_top_limit: 100 }
  events:       { default_recent_limit: 50 }
  search:       { stopwords: [and, are, for, from, into, the, this, that, with] }
  tag_synonyms: { golang: go, javascript: js, k8s: kubernetes }

workflows:
  - id: 1
    key: omakase
    name: Omakase Workflow
    defaults:
      task:    { edit: false, delete: false }    # workflow-level baseline — buckets opt in
      comment: { edit: false, delete: false }
    operations:
      archive:
        guards:
          - { type: comments_tagged, tag: archive-reason, count: 1, hint: "Tag #archive-reason first." }
      delete:
        guards:
          - { type: comments_tagged, tag: justification,  count: 1, hint: "Tag #justification first." }
    buckets:
      - id: 1
        key: backlog
        name: Backlog
        position: 1                                # default bucket for new tasks
        permissions:
          task: { edit: true }                     # planning bucket — edit allowed
      - { id: 2, key: dev,    name: Development, position: 2 }
      - { id: 3, key: review, name: Review,      position: 3 }
      - id: 4
        key: done
        name: Done
        position: 4                                # final → emits task.completed
        permissions:
          task: { delete: true }                   # only Done permits hard delete
    transitions:
      - from: 1
        to: 2
        guards:
          - type: comments_tagged
            tag: self-branch
            count: 1
            hint: "Tag #self-branch with the branch name before starting."
      - from: 2
        to: 3
        guards:
          - type: blockers_in
            buckets: [done]
            hint: "Move blockers to Done first."
          - type: comments_tagged
            tag: resume
            count: 1
      - from: 3
        to: 4
      - from: 3
        to: 2          # kickback path, no guards
      - from: 4
        to: 3          # re-open path, no guards

# Strict allowlist — only these skills activate even if more files exist.
skills: [implementation, markdown]

# Global laws.
laws: [workflow-enforced, yaml-is-canonical]

personas:
  - slug: engineer
    skills: [implementation, markdown]
    laws:   [project-scope-only]   # persona-scoped — must NOT also appear in top-level laws

projects:
  - slug: omakiten
    name: Omakiten
    description: Opinionated checkpoints for AI-driven development
    # Project-scoped laws also must not collide with global/persona scopes.

# Templates: omitted → every templates/*.md autoloads. Frontmatter `default:` activates each.
```

The file references in this doc point at the source-of-truth code; if behavior ever drifts, those files are authoritative — update this doc.

---

## Default kit reference

What ships in `defaults/` and is materialized on first run by `configstore.EnsureDefaultFiles`. Everything below can be overridden by writing to the matching `<root>/<folder>/custom/<slug>.md` (or by editing the file in place — though defaults are overwritten on update, customs are preserved).

### Laws (`defaults/laws/`)

Each law is a single markdown file under `defaults/laws/<slug>.md` with frontmatter (`name`, `severity`, `description?`) and a body. Severity, description, and body are user-tunable per-project once the file lands in `<root>/laws/custom/<slug>.md`; the catalog therefore changes with every fork. Discover the active set with:

```sh
okt law list                 # all loaded laws (slug, severity)
okt law show <slug>          # body + frontmatter
```

Per-preset wiring (which preset binds which law via `mcp_commands.<cmd>.laws`) lives in [`_generated/presets-<preset>.md`](./_generated/) — one file per preset.

### Skills (`defaults/skills/`)

The default kit ships only project-agnostic skills. Stack-specific skills (Go, Python, SQLite, React, etc.) belong in `<root>/skills/custom/<slug>.md` and are wired per-persona in your local active profile yaml. Discover the active set with:

```sh
okt skill list
okt skill show <slug>
```

Per-persona skill bindings (which persona pulls which skill in each preset) live in [`_generated/presets-<preset>.md`](./_generated/).

### Personas (`defaults/personas/`)

Each persona is a markdown file with a frontmatter `description` + a body declaring the procedural loop. Skill references happen on the wiring side, not on the persona file. Discover the active set with:

```sh
okt persona list
okt persona show <slug>
```

Each preset's `personas:` block in `defaults/config/<preset>.yaml` is the strict allowlist for that preset — entities outside the list still live on disk but are not loaded by that preset. Per-preset wiring tables: [`_generated/presets-<preset>.md`](./_generated/).

### Templates (`defaults/templates/`)

Each template is a markdown body with frontmatter declaring `entity` (`comment` / `task` / `pr` / `decision` / …) and an optional `default` slot that marks it as the canonical scaffold for its entity kind. Bodies are fetched JIT via the MCP `templates.show` tool — they never ship inline in prompts. Discover the active set with:

```sh
okt mcp call templates.list --input '{}'      # slug + entity + default flag
okt mcp call templates.show --input '{"slug":"<slug>"}'   # body
```

Per-command template bindings (which `okt-*` command pulls which template per preset) live in [`_generated/presets-<preset>.md`](./_generated/).


### Themes (`defaults/themes/`)

| Key | Vibe |
|---|---|
| `omakiten` | Dark with neon-green accent. The default. |
| `catppuccin-macchiato` | Catppuccin Macchiato — pastels on deep navy. |

See `.docs/theming-guide.md` for the eight color tokens consumed by the TUI.

### Default workflow (`defaults/config/omakase.yaml`)

Single workflow `default` with four buckets — `backlog` → `dev` → `review` → `done` — and tag-anchored guards on every forward edge plus guard-free kickback paths (`review→dev`, `done→review`). Full transitions and guards: see `defaults/config/omakase.yaml` or `.docs/guards-guide.md` §"Worked example".

---

## Paths and backups

### Layout under the resolved root

`<root>` is one of (highest precedence first):

1. `$OMAKITEN_HOME`
2. `$XDG_CONFIG_HOME/omakiten`
3. `~/.config/omakiten`

```
<root>/
  config/
    omakase.yaml           # canonical kit (also a workflow preset)
    izakaya.yaml           # slim preset — workflow only
    kaiseki.yaml           # six-stage formal preset
    shokunin.yaml          # six-stage strict preset
    .active                # one-line state: basename of the active profile (optional)
    custom/                # user-authored profile yamls (preserved across default refresh)
      <profile>.yaml
  laws/
    <slug>.md              # default-kit law
    custom/<slug>.md       # user-authored law (preserved; overrides same-slug default)
  skills/
    <slug>.md
    custom/<slug>.md
  personas/
    <slug>.md
    custom/<slug>.md
  templates/
    <slug>.md
    custom/<slug>.md
  themes/
    <key>.yaml
    custom/<key>.yaml
  notifications/
    <slug>.yaml
    custom/<slug>.yaml
```

Source: `internal/paths/paths.go:ConfigRoot`, `EntityDir`, `EntityCustomDir`, `ActiveConfigFile`. Legacy flat layouts (`<root>/<name>.yaml` at the root with no `config/` subdir) are tolerated by `ConfigRootFromYAMLPath` and migrated forward by `configstore.MigrateLayout` on next connect. `ConfigRootFromYAMLPath` also recognizes `<root>/config/custom/<name>.yaml`, so entity folders resolve correctly when the active profile lives under `custom/`.

### Repo-local `.omakiten/` standalone install

When `.omakiten/` is present at (or above) the current working directory, the runtime treats it as a **complete standalone install** and ignores the user-global ConfigRoot entirely. There is no merge, no overlay, no layered fallback. The only thing that stays global is the SQLite database — `.omakiten/` is config-only.

Discovery: `config.FindRepoLocal(CWD)` walks the parent chain, stopping at the first `.omakiten/`, at `$HOME`, or at the filesystem root. When the walker finds a directory, `runtimeOptions.resolvedConfigRoot` and `resolvedConfigPath` switch to that root for the rest of the process. The `--config` flag overrides discovery — when present, the flag is the authoritative source and the badge reflects "global".

Layout mirrors the user-global root exactly:

```
<repo>/.omakiten/
  config/
    <active>.yaml          # picked preset (or user-authored profile)
    .active                # one-line state: basename of the active profile
    custom/                # user-authored profile yamls
  skills/<slug>.md   + custom/
  laws/<slug>.md     + custom/
  personas/<slug>.md + custom/
  templates/<slug>.md + custom/
  themes/<key>.yaml  + custom/
  notifications/<slug>.yaml + custom/
```

The expected workflow is `okt config init --scope local --preset <name>`: that single call materialises every entity folder via `EnsureDefaultFiles`, copies every shipped preset yaml under `config/`, and points `.active` at the chosen one. The result is a self-contained install — `LoadBundle(<repo>/.omakiten/config/<active>.yaml)` succeeds without any merge step.

Source: `internal/config/repo_local.go:FindRepoLocal`, `internal/config/seed_install.go:SeedInstall`, `internal/cli/root.go:runtimeOptions.discoverRepoLocalRoot`.

### Inspecting the active layer — `okt config <sub>`

| Subcommand | Purpose |
| --- | --- |
| `okt config init --scope <global\|local> --preset <name> [--force]` | Materialise a complete install (config + entity folders + preset library) into the chosen scope. `--force` re-copies every embedded shipped file; user `custom/` subtrees are never touched. |
| `okt config show --scope <global\|local>` | Print the raw bytes of the chosen scope's active yaml. |
| `okt config path --scope <global\|local>` | Print the install root directory (the ConfigRoot for global, the discovered `.omakiten/` for local). |
| `okt config why <key> [--layer <global\|local>]` | Walk the active config (or a pinned layer) by dotted YAML key path and report `{key, value, source, path}`. Missing keys return `source = "not_set"`. |
| `okt config diff <left> <right>` | Structural YAML diff between two sources. Operands accept `global`, `local`, `local:<path>`, or any raw yaml file path. Emits one entry per divergent leaf (`added` / `removed` / `changed`). |

### TUI scope badge

Settings › General shows a `scope` row that reads:
- `global` — runtime is loading the user-global install.
- `local (<.omakiten path>)` — runtime is loading a discovered repo-local install.

The badge reflects what the loader actually picked, not the discovery candidates. Using `--config <path>` clears the badge to `global` because the explicit flag bypasses walk-up discovery.

### SQLite database

```
<data-root>/omakiten.db
```

`<data-root>` is one of (highest precedence first):

1. `$OMAKITEN_HOME/data`
2. `$XDG_DATA_HOME/omakiten`
3. `~/.local/share/omakiten`

The DB is a single file. Schema migrations are applied transactionally on every connect (`internal/sqlite/store.go:Open`). Source: `internal/paths/paths.go:DataDir`, `DatabaseFile`.

### Profiles (advanced)

The resolver supports multiple yaml profiles under `<root>/config/`. The active one is selected by writing its basename into `<root>/config/.active`; `<root>/config/custom/<name>.yaml` is tried before `<root>/config/<name>.yaml`. Profile switching today happens via the TUI Settings › Config picker (which calls `paths.SetActiveConfig`); the CLI accepts a per-invocation override via `--config <path>` or by editing `.active` directly. When `.active` is missing, blank, or names a profile that exists in neither location, the resolver falls through to discovery: first alphabetical `.yaml` at the root, then under `custom/` — so a renamed or removed canonical kit degrades to "first available preset" instead of breaking init.

### Backup

Everything Omakiten persists is on the local filesystem. Two paths cover full state:

```sh
# Config (yaml + entity files + themes + customs)
cp -a "${OMAKITEN_HOME:-$HOME/.config/omakiten}" /backup/omakiten-config

# Data (SQLite)
cp -a "${OMAKITEN_HOME:+$OMAKITEN_HOME/data}${OMAKITEN_HOME:-$HOME/.local/share/omakiten}" /backup/omakiten-data
```

The DB file can be copied while `okt` is not running; for a hot backup use SQLite's `.backup` command or `VACUUM INTO`. There is no concurrent multi-writer story — the tool is single-user, single-process by design.

#### Rolling snapshots — `okt db backup`

The in-binary `okt db backup` writes the live SQLite file to a timestamped snapshot under `$XDG_STATE_HOME/omakiten/backups/<utc-iso>.db` (defaults to `~/.local/state/omakiten/backups/`). The copy is atomic (tmp + rename) and prunes older snapshots according to `settings.backup.retention_count`:

```yaml
settings:
  backup:
    retention_count: 5   # keep the 5 newest snapshots; 0 disables prune
```

Every destructive command — `okt projects delete`, `okt update`, the TUI Home `d`+`d` confirm — runs the same routine before mutating state. Backup failure aborts the destructive flow with a coded error; the snapshot is the recovery artefact you reach for if the cascade went further than expected. `okt uninstall` does NOT auto-backup (uninstall removes user-owned data by intent); run `okt db backup` first if you want a snapshot to keep.

The strict snapshot filename pattern (`<yyyy-mm-dd>T<hh-mm-ss>Z.db`) means manual `.db` files you drop in the same directory are ignored by the prune pass — only files matching the pattern are rotated.

This complements the surface policy: destructive ops live on CLI + TUI but not MCP. See [surface-policy.md](surface-policy.md) for the full table and criteria.

### Resetting

`mise run purge` removes both `~/.config/omakiten` and `~/.local/share/omakiten` (`.mise.toml`). Re-run `okt init` to reseed defaults. Customs under `<entity>/custom/` are also removed by purge — back them up first if you care.
