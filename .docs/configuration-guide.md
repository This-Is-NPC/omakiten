# How to Configure the Active Profile YAML

The active profile yaml is the canonical write-model: every field below is parsed by `internal/config/loader.go` and validated by `internal/config/validator.go` before being imported into SQLite. YAML decoding uses `KnownFields(true)`, so unknown fields fail loud rather than silently. The embedded canonical kit ships as `defaults/config/omakase.yaml`; it is materialized into the user's `<config-dir>/` on first run alongside the other official presets (`izakaya.yaml`, `kaiseki.yaml`, `shokunin.yaml`).

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
skills:    [ <slug>, … ]    # optional allowlist
laws:      [ <slug>, … ]    # optional allowlist
templates: [ <slug>, … ]    # optional allowlist
personas:  [ { slug, skills?, laws? }, … ]
projects:  [ { slug, name, description?, laws? }, … ]
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

### `kit`

```yaml
kit:
  id: 1               # int, > 0, required
  key: default        # string, required
  name: Default Omakiten Kit   # string, required
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
    active: default           # string, required; must match workflows[].key
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

A task with five long `#resume` comments (each ~1500 chars) ships ~6000 chars (~1500 tokens) of comments alone on every `tasks.continue` call. Combined with the workflow block (~150 tokens), the tool result lands at ~2400 tokens.

Two changes cut that by more than half:

```yaml
config:
  mcp:
    recent_comment_limit: 3   # 5 → 3
    max_comment_chars: 500    # 0 → 500
```

After: 3 comments × 500 chars = ~375 tokens for comments, no truncation on the most recent (it stays under 500). Workflow still ships (~150 tokens) unless you also add `include_workflow_in_continue: false` once `/okt` ran. Total: ~525 tokens — a ~78% reduction on the comment-heavy task.

Cross-reference: `.docs/mcp-guide.md#anatomy-of-an-mcp-command` walks through how the tool result composes and which fields each setting trims.

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

`app.ConfigService.Import` (called from both composition roots — `internal/cli/root.go` and `internal/agentruntime/runtime.go`) returns a `*domain.EnumRegistry` alongside the bundle, between `LoadBundle` (validate) and `ImportBundle` (write). Composition roots inject the registry into every service that resolves labels (`TaskService`, `LawService`, `WorkflowService`, `TUIQueryService`, `ConfigService`, `ContextService`, agent `Service`); the TUI Model builds its own copy from the priorities/severities slices it already receives. There are no process-global enum tables — every lookup goes through the injected registry via `registry.PriorityLabel(id)`, `registry.PriorityFromLabel("high")`, `registry.DefaultPriority()`, etc. JSON wire format of `domain.Priority` / `domain.Severity` is the raw int id (no `MarshalJSON`); label projection happens at DTO boundaries (e.g. `agent.TaskSummary.Priority` is the label string). Tests construct a fresh registry via `testfixtures.CanonicalRegistry()` and thread it through service constructors.

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

Configurable id↔value table for law severities. Same shape and contract as `config.priorities` — code references the integer id (`tasks.severity_id` after migration 016 for laws is stored similarly), renderers (TUI badge, MCP `severity` field, JSON marshaling) resolve the human label via this table at the boundary. Renaming a label is a single YAML edit; existing law rows keep their stored `severity_id` so nothing breaks at the data layer.

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

Adding a `{id: 4, value: blocker, color: error}` entry makes `severity: blocker` a valid frontmatter value; no code change needed. Renaming `"error"` to `"critical"` updates how the badge is rendered for every existing law on the next read — `severity_id` storage absorbs the change.

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
    key: default
    name: Default Workflow
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

There is no hardcoded "first bucket is special" rule anymore. The default kit (`defaults/config/omakase.yaml`) reproduces the legacy semantics declaratively: strict defaults at the workflow level + an explicit opt-in on the `backlog` bucket.

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
- The same precedence is enforced at read time: `templates.show <global-slug>` from inside a project that shadows the same `default` kind hard-rejects with `validation_error`, naming the active project-scoped slug. Outside any registered project the legacy slug-only lookup is preserved. See `.docs/mcp-guide.md` §Templates.

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
  id: 1
  key: default
  name: Default Omakiten Kit

config:
  output:    { json_minified: true, omit_empty: true }
  context:   { default_level: 2, max_tokens: 12000 }
  workflow:  { active: default }
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
    key: default
    name: Default Workflow
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

<!-- BEGIN auto:catalog kind=laws -->
# Laws Catalog

Auto-derived from `defaults/laws/*.md` frontmatter.

| Slug | Severity | Description |
|---|---|---|
| `5w2h-elicitation` | warning | During `okt-imagine`, walk the user through the seven questions: What / Why / Who / When / Where / How / How much. Don't accept vague answers ("the user", "soon", "important"). If a question can't be answered, name the gap and propose how to close it before filing the task. |
| `acceptance-criteria-required` | error | Every feature ships with documented acceptance criteria captured during the requirements stage. The shape is the project's convention — Given/When/Then, bullet list, executable test stub, or another testable form — but the criteria must be testable, the requester must agree to them, and the reviewer must be able to verify them against the implementation. |
| `audit-trail-integrity` | error | Comments published in dev or beyond are append-only. No delete. Corrections happen via a new `#scribe-correction` comment that names the assertion being corrected, the corrected text, and the reason. The original stays in place — the trail must survive review. |
| `authorize-remote-writes` | error | Never run `git push`, `git push --force`, `gh pr create`, `gh pr edit`, `gh pr merge`, or any command publishing/mutating a remote repo without explicit user authorization in this conversation. Local commits, branches, file edits OK. Authorization is per-action: pushing one branch does not authorize future pushes; opening one PR does not authorize others. |
| `blameless-postmortem` | error | Any production incident or near-miss earns a `#postmortem` comment AND a `docs/postmortems/<YYYY-MM-DD>-<title>.md` file: timeline (UTC), detection latency, customer impact, 5-whys root cause, action items with owners and due dates. "Human error" is never a root cause — it is the system that allowed the error. |
| `blast-radius-awareness` | warning | Every change declares its blast radius: users affected, services touched, irreversibility class. The classification drives gate severity — a critical-radius change demands stricter sign-off than a contained one. Default to overestimating; reviewers can downgrade. |
| `bounded-self-review` | warning | Run tests after each increment. On failure: find root cause, apply targeted fix — never restart from scratch. Cap at 3 attempts; after 3 failures stop and report failing tests, root cause, attempted fixes, and adjustment plan. |
| `boy-scout-rule` | warning | Leave code cleaner than you found it. Opportunistic small refactors during feature work are encouraged when they touch the affected area. Document each drive-by cleanup in a `#refactor-drive-by` comment; assert no behavior change. |
| `conventional-commits` | error | Follow [Conventional Commits](https://www.conventionalcommits.org/) in English: `type(scope): summary`. Types: `feat`, `fix`, `docs`, `refactor`, `chore`, `test`, `build`, `ci`, `perf`. Append `!` for breaking (`feat!: ...`). One intent per commit; split mixed trees via non-interactive staging. |
| `coverage-gate` | error | Test coverage must not drop. New behavioral code targets ≥80% line coverage on the affected packages. Exact numbers (delta + absolute) appear in the `#tests-passing` comment. Exemptions require a documented rationale signed by a reviewer. |
| `decision-record-on-divergence` | error | Significant decisions — adopting a new dependency, replacing a load-bearing component, deviating from precedent, or any choice future maintainers will want to trace back — get a decision record (`docs/decisions/<NNNN>-<title>.md`, or the repo's preferred location and format) BEFORE the change lands. The format is the project's convention, not a mandate. |
| `design-recorded` | error | Implementation starts only after the design approach is documented in the repo's preferred format — decision record, RFC, design doc, sketch, or whatever the project already uses. The artifact is architecture-agnostic; what matters is that the approach is written down before code lands, so reviewers and future maintainers can audit the choice. |
| `dual-peer-review` | error | At minimum two independent peer reviewers — neither the task author nor a co-author of the change. Each leaves a `#peer-review` comment with verdict, approval scope, and concerns. A single reviewer is not a peer review; it is a hand-off. |
| `error-budget-aware` | warning | Reliability-affecting changes cite the current error-budget consumption before shipping. If the budget is exhausted, only fixes and rollbacks ship — features wait. Reference the SLO definition the change touches; do not invent budgets per task. |
| `feasibility-gate` | error | If the request is not implementable in current architecture/dependencies, stop before authoring. Report technical reasons, concrete blockers, viable alternatives — then wait for the user. Do not soften infeasibility into "we could try". |
| `green-main-always` | error | Never push code that breaks the build or tests on main. Verify locally (or via a pre-push CI run) before pushing. A broken main blocks every other contributor — fix-forward or revert within 10 minutes; do not "investigate later". |
| `hypothesis-required` | error | Every spike answers a written question. The hypothesis lives on the task body or in a `#hypothesis` comment before any code is written. If the question can't be stated in one sentence with a falsifiable signal, the spike isn't ready — it is wandering. |
| `invest-stories` | warning | User stories satisfy INVEST: Independent (can ship alone), Negotiable (room for the team to shape it), Valuable (clear benefit to a real user), Estimable (team can size it), Small (fits in a sprint or shorter), Testable (acceptance criteria are verifiable). Flag the missing letters when the story falls short. |
| `no-assumptions` | warning | Every claim must be traceable to code, configuration, or explicit user input. When info missing: ask, mark `[assumption]` with the guess explicit, or `[user-provided]` when the user said so without code backing. Never invent versions, file paths, or business rules to fill a section. |
| `no-silent-behavior-changes` | error | Every behavioral change ships with explicit evidence: a failing-then-passing test, a `#resume` comment naming the change, or a commit message calling it out. Incidental shifts inside a refactor are still behavior changes — document them. |
| `non-functional-explicit` | warning | Functional and non-functional requirements live in separate sections. NFRs cover performance, security, usability, observability, scale, accessibility, and compliance — each named with a target or marked "not applicable + reason". Burying NFRs inside the user story or treating "make it fast" as a requirement hides the real constraints. |
| `outcome-over-output` | warning | A feature is not "shipped" because the code merged; it is shipped when the targeted outcome moves. Every task names the user or business outcome it should produce, not just the artifact it produces. If you can't state the expected outcome, the work isn't ready to start. |
| `pdca-aware` | warning | Recognize which phase of Plan-Do-Check-Act each okt-* command represents. `okt-imagine` = PLAN; `okt-create` = PLAN → DO handoff; `okt-implement` = DO + ACT + CHECK as the task progresses through dev → review. Name the phase to the user when context shifts; users orient on the cycle even when the work stack is deep. |
| `peer-review-required` | error | At least one independent peer review (reviewer is not the task author or a co-author of the change) before the task moves past review. The reviewer leaves a `#peer-review` comment naming the verdict (approve / request-changes / reject), the approval scope, and any open concerns. A single thumbs-up or a self-review does not satisfy this gate. |
| `pre-mortem-required` | error | Before implementation, imagine the change has failed in production and write what went wrong. The `#pre-mortem` comment names failure modes, detection signals, and mitigations. No code lands before the pre-mortem is filed and reviewed. |
| `prioritization-recorded` | warning | When the user brings more than one option to `okt-imagine` or `okt-create`, record the prioritization rationale before committing. Use MoSCoW (Must / Should / Could / Won't) for qualitative ranking; use RICE (Reach × Impact × Confidence ÷ Effort) when the team needs to compare across teams or quarters. "We picked the obvious one" is not a record. |
| `project-scope-only` | error | Never mix tasks or context from different projects. |
| `requirements-signed-off` | error | A task moves past requirements only after the requester signs off in writing on the user story, the acceptance criteria, and the non-functional constraints. "Signed off" means a comment from the requester naming the agreement; verbal nods or chat reactions do not survive review and do not count. |
| `rollback-plan-mandatory` | error | Every change ships with a rollback plan: revert steps, validation post-rollback, comms plan. Non-trivial rollbacks (multi-step migrations, schema or data shape changes) require explicit reviewer sign-off on the strategy. |
| `self-report` | error | Record any error that needed more than one fix attempt — the second attempt is the trigger. Call `errors.record` (one-line description, context, specific tags) and `solutions.add` against the returned id with the resolution that worked. Use `solutions.confirm` when applying a previously recorded solution from `errors.search`. |
| `small-batches` | warning | Prefer many small PRs (<400 LOC diff) over one large PR. Small batches review faster, revert cheaper, ship sooner, and shrink the blast radius of a regression. Optimizes the DORA lead-time and change-failure-rate metrics simultaneously. |
| `smart-success` | warning | Every task carries a success definition that satisfies SMART — Specific outcome, Measurable signal, Achievable given the constraints, Relevant to the stated goal, Time-bound for re-evaluation. "Improve things" is not SMART; "p95 latency under 200ms on the canonical workload by end of sprint" is. |
| `template-fidelity` | warning | Fill template placeholders with verifiable content from the working context. Leave empty or remove sections you cannot back with facts. Never fabricate issue numbers, links, file paths, or decisions the template did not declare. |
| `test-evidence` | error | Behavioral changes ship with reproducible test evidence: a failing-then-passing test added in the same diff (TDD), or a `#tests-passing` comment with the test command, an output snippet, and a duration. "I tested locally" without an artifact the reviewer can rerun is not evidence. |
| `time-boxed-spike` | error | Every spike declares a time-box up front (hours or days, on the task body). Past the box: stop, write a `#discard` or `#promote` comment, and either kill the spike, escalate the box explicitly with reason, or convert it to a proper task. Open-ended spikes are not spikes — they are wandering. |
| `tracer-bullet` | warning | Ship a thin end-to-end slice — input to output, demoable — before adding depth or polish to any single piece. Connect the wires first; flesh out logic only after the whole shape is observable. Half a feature is worse than a thin slice of the whole. |
| `workflow-enforced` | error | Only move tasks through explicit workflow transitions. |
| `yagni-first` | warning | Build only what the active hypothesis demands. Anything beyond gets a postit (followup comment, backlog task), not code. Generality, future-proofing, and "while I'm at it" cleanups belong to the next spike, not this one. |
| `yaml-is-canonical` | error | Persist changes to laws, workflows, personas, skills, and config in omakiten.yaml. |
<!-- END auto:catalog -->

Per-preset wiring (which preset binds which law via `mcp_commands.<cmd>.laws`) lives in [`_generated/presets-<preset>.md`](./_generated/) — one file per preset.

### Skills (`defaults/skills/`)

The default kit ships only project-agnostic skills. Stack-specific skills (Go, Python, SQLite, React, etc.) belong in `<root>/skills/custom/<slug>.md` and are wired per-persona in your local active profile yaml.

<!-- BEGIN auto:catalog kind=skills -->
# Skills Catalog

Auto-derived from `defaults/skills/*.md` frontmatter.

| Slug | Description |
|---|---|
| `acceptance-criteria-writing` | Testable acceptance shapes (Given/When/Then or alternatives); criteria the requester and reviewer can verify. |
| `architecture-mapping` | Tech stack, dependencies, design patterns, infrastructure, code metrics with measurable references. |
| `change-management` | Approval matrix, sign-off discipline, audit-trail integrity, regulated-environment habits. |
| `continuous-integration` | Pre-push verification, CI as source of truth for green, fix-forward vs revert decision discipline. |
| `decision-records` | When to record a decision; concise context / decision / consequences; discoverable filenames and links. |
| `design-documentation` | Capture the approach in the repo's preferred format (decision record / RFC / design doc) before coding. |
| `discovery` | Feasibility analysis, clarifying questions, scope boundaries, surfacing hidden constraints before code. |
| `documentation` | Generates and reviews architecture, requirements, and contributor docs; claims traceable to code. |
| `dora-mindset` | Optimize for lead time, deploy frequency, MTTR, and change failure rate — small batches help all four. |
| `five-w-two-h` | Structured elicitation — What / Why / Who / When / Where / How / How much. Surface gaps; don't accept vague answers. |
| `implementation` | Small coherent increments, tests for new and impacted behavior, regression analysis, bounded self-review. |
| `invest-stories` | Wake (2003) checklist — Independent / Negotiable / Valuable / Estimable / Small / Testable. Flag missing letters. |
| `lean-experimentation` | MVP design, falsifiable hypotheses, acceptance signals, build-measure-learn loops over polish. |
| `markdown` | Frontmatter, tables, code fences, mermaid; renders correctly in GitHub and editor previews. |
| `moscow-prioritization` | Qualitative ranking — Must / Should / Could / Won't (this iteration). Record rationale per item. |
| `non-functional-requirements` | Quality attributes — performance, security, usability, observability, scale, accessibility, compliance — captured separately from FRs. |
| `okr-framing` | Objective + Key Results — outcome-driven goal shape. Each KR has baseline, target, and timeframe. |
| `pdca-cycle` | Plan-Do-Check-Act awareness — recognize which phase each okt-* command represents and name it for the user. |
| `postmortem-authoring` | Blameless 5-whys, timeline reconstruction (UTC), action items with owners and due dates. |
| `readme-curation` | Keeps install, usage, and examples in sync with the actual code surface. |
| `requirements-elicitation` | Gather needs from stakeholders; INVEST-style user stories; testable acceptance criteria; documented sign-off. |
| `requirements-mapping` | Extracts functional, non-functional, and business rules with source-file references. |
| `rice-scoring` | Quantitative priority — Reach × Impact × Confidence ÷ Effort. Use when comparing across teams or quarters. |
| `risk-driven-development` | Pre-mortem authoring, blast-radius analysis, irreversibility classification, mitigation-first design. |
| `smart-goals` | Specific / Measurable / Achievable / Relevant / Time-bound success criteria; turn intent into a verifiable signal. |
| `sre-discipline` | SLI / SLO / error-budget thinking; four golden signals (latency, traffic, errors, saturation). |
| `staged-delivery` | Move through requirements → planning → dev → review → docs → done with explicit gates and recorded handoffs. |
| `static-analysis-discipline` | Lint / security (SAST) / coverage / SCA gates as part of Definition of Done; no merge with new warnings. |
| `test-driven-development` | Red → green → refactor; tests-first for new behavior; regression test on every bugfix. |
| `test-driven-development-strict` | Red → green → refactor with coverage-gate awareness; tests-first + coverage delta + perf regression check. |
| `time-box-discipline` | Declare boxes up front; recognize when to kill, promote, or extend with explicit reason. |
| `tracer-bullet-shipping` | Walking-skeleton end-to-end first; depth comes only after the full shape is observable. |
| `trunk-based-development` | Short-lived branches (<1 day), frequent rebases on main, feature flags for incomplete work, fast revert. |
| `user-story-writing` | Authoring user stories — Description, AC, DoD, scope; matches the task template verbatim. |
<!-- END auto:catalog -->

Per-persona skill bindings (which persona pulls which skill in each preset) live in [`_generated/presets-<preset>.md`](./_generated/).

### Personas (`defaults/personas/`)

<!-- BEGIN auto:catalog kind=personas -->
# Personas Catalog

Auto-derived from `defaults/personas/*.md` frontmatter.

| Slug | Description | Skills |
|---|---|---|
| `craftsperson` | Treats every change as regulated — pre-mortem, rollback plan, dual sign-off, blameless postmortem. | — |
| `documentation-agent` | Keeps the project narrative in sync with code; surfaces material work as new tasks rather than editing in place. | — |
| `engineer` | Trunk-based contributor — small batches, green main always, test-first, opportunistic cleanup. | — |
| `methodical-engineer` | Works in stages — requirements before design, design before code, decisions recorded, peer review mandatory. | — |
| `product-owner` | PLAN phase — interrogate the user via 5W2H, frame success in SMART, hand off only when concrete enough. | — |
| `tinkerer` | Hypothesis-driven explorer — writes the question first, ships walking-skeleton, kills early. | — |
<!-- END auto:catalog -->

Each preset's `personas:` block in `defaults/config/<preset>.yaml` is the strict allowlist for that preset — entities outside the list still live on disk but are not loaded by that preset. Per-preset wiring tables: [`_generated/presets-<preset>.md`](./_generated/).

### Templates (`defaults/templates/`)

<!-- BEGIN auto:catalog kind=templates -->
# Templates Catalog

Auto-derived from `defaults/templates/*.md` frontmatter.

| Slug | Entity | Default | Description |
|---|---|---|---|
| `comment-5w2h` | comment | — | Structured elicitation — answer the seven questions. Surface gaps explicitly when no answer is yet known. |
| `comment-acceptance` | comment | — | Fills the `#acceptance` guard. Project picks the format — Given/When/Then or any other testable shape. |
| `comment-design-decision` | comment | — | Inline pointer to a decision record introduced by the change. Format is the project's convention. |
| `comment-discard` | comment | — | Closes a spike that did not confirm its hypothesis. Records the lesson and the cost saved. |
| `comment-documentation` | comment | comment-documentation | Closing checklist — fills the `#documentation` guard (review → done). |
| `comment-hypothesis` | comment | — | Captures the question a spike answers; fills the `#hypothesis` guard (backlog → dev). |
| `comment-lessons-learned` | comment | — | Closing reflection — what worked, what didn't, process changes proposed. |
| `comment-moscow` | comment | — | Qualitative priority — Must / Should / Could / Won't (this iteration). Rationale per item. |
| `comment-non-functional` | comment | — | NFRs separated from functional — performance / security / usability / observability / scale / accessibility / compliance. |
| `comment-okr` | comment | — | Objective + Key Results — outcome-driven goal shape. Each KR has baseline, target, and timeframe. |
| `comment-peer-review` | comment | — | Reviewer sign-off for the `#peer-review` guard (review → docs). |
| `comment-peer-review-strict` | comment | — | Strict sign-off — one of N independent reviewers required by the dual-peer-review law. |
| `comment-postmortem` | comment | — | Blameless incident or near-miss writeup. Mirrors `docs/postmortems/<YYYY-MM-DD>-<title>.md`. |
| `comment-pre-mortem` | comment | — | Fills the `#pre-mortem` guard before implementation. Imagine the change has already failed. |
| `comment-promote` | comment | — | Promotes a confirmed spike to real work. Names the production gaps that remain. |
| `comment-refactor-drive-by` | comment | — | Documents an opportunistic Boy-Scout cleanup that rode along with a feature or fix. |
| `comment-requirements` | comment | — | User story + acceptance signals for the `#requirements` guard (requirements → planning). |
| `comment-resume` | comment | comment-resume | Implementation handoff — fills the `#resume` guard (dev → review). |
| `comment-rice-score` | comment | — | Quantitative priority — Reach × Impact × Confidence ÷ Effort, with computed score per option. |
| `comment-risk-assessment` | comment | — | Fills the `#risk-assessment` guard. Names top risks, mitigations, and residual risk accepted. |
| `comment-rollback-plan` | comment | — | Fills the `#rollback-plan` requirement before review. Names the path back to safety. |
| `comment-scribe-correction` | comment | — | Append-only correction to a prior comment. The original stays; the trail survives. |
| `comment-selfbranch` | comment | comment-selfbranch | Branch declaration — fills the `#self-branch` guard required to move backlog → dev. |
| `comment-smart-success` | comment | — | Success criteria in SMART form — Specific / Measurable / Achievable / Relevant / Time-bound. |
| `comment-tests-passing` | comment | — | Test evidence for `test-evidence` law; fills the `#tests-passing` guard (dev → review). |
| `comment-tests-passing-strict` | comment | — | Test evidence with coverage delta + types + perf regression check; satisfies the coverage-gate law. |
| `config-orientation` | orientation | — | Map of where Omakiten config lives, how the active profile is selected, and every field a user can tune to shape their workflow. |
| `decision-record` | decision | — | Generic decision-record scaffold — status, context, decision, consequences, alternatives. Project picks file path. |
| `design-doc` | design | — | Generic design-doc scaffold — problem, approach, trade-offs, open questions. Architecture-agnostic. |
| `pull-request` | pr | pr | PR scaffold — before/after, changes, files, validation, deviations, risks, references. |
| `task-bugfix` | task | — | Bugfix scaffold — reproduction, root cause, fix, regression test. |
| `task-change-request` | task | — | Change-control scaffold — risk class, SLO impact, pre-mortem, rollback strategy, approval matrix. |
| `task-feature` | task | — | Feature scaffold for staged delivery — requirements summary, approach, acceptance criteria, risks. |
| `task-spike` | task | — | Spike scaffold — hypothesis, falsifiable signal, time-box, discard plan. |
| `user-story` | task | task | Task scaffold — Description, AC, DoD, INVEST check, Scope, Feasibility. |
<!-- END auto:catalog -->

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

### Resetting

`mise run purge` removes both `~/.config/omakiten` and `~/.local/share/omakiten` (`.mise.toml`). Re-run `okt init` to reseed defaults. Customs under `<entity>/custom/` are also removed by purge — back them up first if you care.
