# System configuration — runtime knobs

The `config:` block in the active profile yaml carries the runtime knobs every service reads: output shape, context budgets, MCP shaping, TUI thresholds, SQLite engine pragmas, retention policies, search heuristics, tag synonyms. Every field below is parsed by `internal/config/loader.go` and validated by `internal/config/validator.go`, then `config.BuildSnapshot(bundle)` materialises the immutable `*config.Snapshot` every app service reads through.

YAML decoding uses `KnownFields(true)`, so unknown fields fail loud rather than silently. The embedded canonical kit ships as `defaults/config/omakase.yaml`; it is materialized into the user's `<config-dir>/` on first run alongside the other official presets (`izakaya.yaml`, `kaiseki.yaml`, `shokunin.yaml`).

For ConfigRoot precedence, `.active` resolution, and the `<root>/` layout, see [path-resolution.md](path-resolution.md). For per-project layering (`.omakiten/` walk-up, hot-reload, per-project Snapshot), see [project-overrides.md](project-overrides.md). For per-entity wiring (workflows, personas, laws, templates, priorities, severities, views), see [entities.md](entities.md).

## Contents

- [Top-level shape](#top-level-shape)
- [`kit`](#kit)
- [`config.output`](#configoutput)
- [`config.context`](#configcontext)
- [`config.workflow`](#configworkflow)
- [`config.theme`](#configtheme)
- [`config.languages`](#configlanguages)
- [`config.mcp`](#configmcp)
- [`config.tui`](#configtui)
- [`config.sqlite`](#configsqlite)
- [`config.activity_log`](#configactivity_log)
- [`config.solutions`](#configsolutions)
- [`config.backup`](#configbackup)
- [`config.events`](#configevents)
- [`config.hooks`](#confighooks)
- [`config.search`](#configsearch)
- [`config.tag_synonyms`](#configtag_synonyms)
- [Validation summary](#validation-summary)
- [Worked example (annotated)](#worked-example-annotated)
- [Update when](#update-when)

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
| `kit` | object | yes | See [`kit`](#kit). |
| `config` | object | yes | See sections below. |
| `workflows` | list | yes | At least one. See [entities.md § workflows](entities.md#workflows). |
| `skills` / `laws` / `templates` | list of slug strings | no | Strict allowlist when present; **autoload** otherwise. See [entities.md § autoload](entities.md#autoload-custom-overrides-and-slug-rules). |
| `personas` | list of `PersonaWiring` | no | Persona wiring (skill/law refs); body lives in `personas/<slug>.md`. See [entities.md § personas](entities.md#personas). |
| `projects` | list of `ProjectWiring` | no | Declarative project wiring; the runtime project list is in SQLite. See [entities.md § projects](entities.md#projects). |
| `mcp_commands` | map | no | Binds `okt-*` MCP prompts to a persona, laws, and templates. See [entities.md § mcp_commands](entities.md#mcp_commands). |

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

## `config.output`

| Field | Type | Effect |
|---|---|---|
| `json_minified` | bool | When true, CLI envelopes are emitted as a single line (`internal/output/json.go`). |
| `omit_empty` | bool | Drop empty/zero fields from the JSON envelope. |

## `config.context`

| Field | Type | Range | Effect |
|---|---|---|---|
| `default_level` | int | 1, 2, or 3 | Default level for `context.dump` when the caller omits one. Levels: **1** = context entries only · **2** adds workflow + tasks + dependencies · **3** adds comments + active laws (`internal/app/context_service.go`). |
| `max_tokens` | int | `>= 0` | Token budget for `context.dump`. Truncation is newest-first; the response sets `truncated: true` once the budget is exceeded (`internal/app/context_service.go:contextBudget`). |

## `config.workflow`

| Field | Required | Effect |
|---|---|---|
| `active` | yes | Selects which `workflows[].key` is currently active. Must match an entry; otherwise validation fails with `config.workflow.active "<x>" does not match any workflow`. |

## `config.theme`

| Field | Required | Effect |
|---|---|---|
| `active` | yes | Theme key — must match a `themes/<key>.yaml` file. The bundled defaults are `omakiten` and `catppuccin-macchiato`. Custom themes go in `themes/custom/<key>.yaml` (preserved across kit refreshes). |

Theme files are validated separately (`ValidateTheme`): `version: 1`, non-empty `key`, `name`, and `colors`. See [themes.md](themes.md) for the canonical color tokens and authoring recipe.

## `config.languages`

Stores the language selected per surface. CLI and TUI values must resolve to loaded `languages/<code>.yaml` packs (bundled or `languages/custom/`), while `agent_output` is free-form text appended to MCP prompt composition as an output-language directive.

```yaml
config:
  languages:
    cli: en
    tui: pt-br
    agent_output: "Português (Brasil)"
```

| Field | Type | Validation | Effect |
|---|---|---|---|
| `cli` | language code | loaded pack, defaults to `en` when empty | CLI labels / help / CLI-owned errors. |
| `tui` | language code | loaded pack, defaults to `en` when empty | Terminal UI labels and notifications. |
| `agent_output` | string | free-form | Natural-language directive sent to the agent in composed MCP prompts. Empty means no directive. |

See [languages.md](languages.md) for the bundled pack catalog, parity rule, and recipe for adding a new pack.

## `config.mcp`

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

### Validation rules (parse-time)

Every field above is required and validated. Missing or out-of-range values fail loud with messages pointing back at `defaults/config/omakase.yaml` so the fix is obvious.

| Rule | Error message shape |
|---|---|
| `recent_comment_limit <= 0` | `config.mcp.recent_comment_limit: must be > 0 (see defaults/config/omakase.yaml for canonical values)` |
| `max_comment_chars < 0` | `config.mcp.max_comment_chars: must be >= 0 (0 = no truncation)` |
| `include_workflow_in_continue` omitted | `config.mcp.include_workflow_in_continue: required boolean (see defaults/config/omakase.yaml)` |
| `cache_prompts` omitted | `config.mcp.cache_prompts: required boolean (see defaults/config/omakase.yaml)` |
| Same shape for `recent_context_limit / next_work_limit / similar_task_limit`. |

### Worked example — taming a long-lived task

A task with many long `#resume` comments dominates every `tasks.continue` call: each comment ships in full, the workflow block ships once, and a fresh task vs. a year-old task differ by an order of magnitude in payload size.

Two settings collapse the bulk:

```yaml
config:
  mcp:
    recent_comment_limit: 3   # 5 → 3
    max_comment_chars: 500    # 0 → 500
```

`recent_comment_limit: 3` keeps the most recent three; `max_comment_chars: 500` hard-caps each body. Add `include_workflow_in_continue: false` once `/okt` has loaded the workflow in the session to drop the per-call workflow block too. The exact byte saving depends on each comment's length and the active workflow's shape — the qualitative effect is "load only what is new since last call".

Cross-reference: [mcp.md § tuning-context-cost](../mcp.md#tuning-context-cost) walks through how the tool result composes and which fields each setting trims.

## `config.tui`

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

Both thresholds are required and strictly positive; `red_at` must be greater than `yellow_at`. Non-positive or inverted values fail validation instead of falling back silently.

## `config.sqlite`

Connection-level engine knobs the Store applies at Open. **Required block** — the kit ships the canonical values the user inherits at install time. Other PRAGMAs (`foreign_keys=ON`, `journal_mode=WAL`, `synchronous=NORMAL`) intentionally stay in code: they encode the engine-level contract Omakiten depends on, not user preference.

```yaml
config:
  sqlite:
    busy_timeout_ms: 5000     # int >0; PRAGMA busy_timeout
    cache_size_kb: 1024       # int >0; PRAGMA cache_size in negative KiB form
    mmap_size_bytes: 0        # int >=0; PRAGMA mmap_size, 0 disables mmap
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `busy_timeout_ms` | int | `> 0` | Sets `PRAGMA busy_timeout`. Larger DBs or systems with concurrent writers (TUI + MCP server sharing a Store) may need a higher value to avoid `database is locked` errors. |
| `cache_size_kb` | int | `> 0` | Sets `PRAGMA cache_size` using SQLite's negative-kilobyte form so hot task/event/dependency pages stay in cache across TUI read fan-out. |
| `mmap_size_bytes` | int | `>= 0` | Sets `PRAGMA mmap_size`. Zero disables mmap; raise only on local filesystems where memory-mapped reads are safe. |

## `config.activity_log`

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

## `config.solutions`

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

## `config.backup`

Tunes rolling database snapshots written by `okt db backup` and destructive flows that call `app.BackupService` before mutating state.

```yaml
config:
  backup:
    retention_count: 5   # int >=0; 0 disables prune
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `retention_count` | int | `>= 0` | Keeps the N newest snapshots in `StateDir()/backups`. Zero disables pruning so snapshots accumulate until managed externally. |

For the snapshot filename pattern, atomic copy, and `mise run purge` interaction, see [path-resolution.md § backups](path-resolution.md#backups).

## `config.events`

Fallback recent-events limit plus per-event channel policy. **Required block.** `defaults` must declare all three channels; `overrides` can change any subset per event type and inherits unspecified channels from `defaults`.

```yaml
config:
  events:
    default_recent_limit: 50  # int >0
    defaults:
      log: true               # persist to events table
      broadcast: true         # fan out over the in-process bus
      hook: true              # let hooks engine dispatch
    overrides:
      tag.added:   { log: false }
      tag.removed: { log: false }
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `default_recent_limit` | int | `> 0` | Fallback row count applied when the caller passes `<=0`. The query is indexed on `(event_type, created_at, id)` so larger values are cheap. |
| `defaults.log` | bool | required | Persists matching events to SQLite when true. Comments are user data and still write even when the log gate is false. |
| `defaults.broadcast` | bool | required | Publishes matching events to in-process subscribers when true. |
| `defaults.hook` | bool | required | Lets the hooks engine consider matching events when true. |
| `overrides` | map | optional; keys must be known event types | Per-event channel overrides. Omitted channel fields inherit from `defaults`; unknown event names fail validation. |

Domain event names live in `internal/domain/events.go::KnownEventTypes` — that file is the source of truth for what's emittable. For action contracts that consume events, see [hooks.md](hooks.md).

## `config.hooks`

Declares automation that reacts to domain events. The active profile owns only the subscription shape; executable action contracts live in [hooks.md](hooks.md), notification card definitions live in [notifications.md](notifications.md).

```yaml
config:
  hooks:
    - on: guard.violated
      when: { operation: task.delete }
      do: exec
      args:
        argv: ["/home/me/scripts/log-blocked-delete.sh"]
        timeout_ms: 3000
    - on: bundle.swapped
      notification: kitten_orphan_migration
```

| Field | Type | Notes |
|---|---|---|
| `on` | event type | Required; must be in `domain.KnownEventTypes`. |
| `when` | map | Optional top-level payload equality filters; all entries must match. |
| `do` / `args` | action shape | Runs an action such as `exec` or `noop`. Mutually exclusive with `notification`. |
| `notification` | slug | Sends a TUI card using `notifications/<slug>.yaml`. Mutually exclusive with `do`. |
| `message`, `message_field`, `detail_message`, `detail_message_field` | strings | Optional hook-level fallbacks for notification text. |

## `config.search`

Tunes text-similarity heuristics shared across agent-side ranking (similar-task hints in `tasks.create_intent`, query overlap scoring). **Required block** — multilingual users add Portuguese/Spanish/etc. words here without a code change.

```yaml
config:
  search:
    stopwords: [and, are, for, from, into, the, this, that, with]
```

| Field | Type | Constraint | What it does |
|---|---|---|---|
| `stopwords` | list of strings | non-empty, lowercase, unique | Tokens dropped before computing overlap scores. Validator rejects empties, duplicates, and uppercase entries (must match the tokenizer's lowercased output). |

## `config.tag_synonyms`

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
| Unknown guard type / bad guard payload | See [guards.md § Validation rules](guards.md#validation-rules). |
| Reference to a non-existent skill/law/persona/template | `<section>: ref "<slug>" has no matching file` |
| Law in two scopes | `laws.<slug> declared in multiple scopes (<a> and <b>)` |
| Bad template `default` | `templates.<slug>: default "<kind>" is not in config.template_defaults` |
| Two templates claiming the same `(default, project)` | `templates.<a> and templates.<b> both declare default="<kind>" (<scope>)` |
| Bad view sort/filter | `config.views.<view>.* "<v>" is not one of [...]` |
| Missing/zero `config.views.logs.window_days` | `config.views.logs.window_days: must be > 0 (see defaults/omakiten.yaml)` |
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
  # template_defaults declares the kinds the user can assign in the TUI
  # picker. Each loaded template can claim at most one (kind, project) pair
  # via its frontmatter (`default: <kind>`, optional `project: <slug>`).
  # Defaults below match the canonical Omakiten kit; tweak as needed.
  template_defaults: [task, pr, comment-resume, comment-selfbranch, comment-documentation]
  views:
    board: { sort: { field: created_at, order: desc }, filter: { priority: [high, normal] } }
    table: { sort: { field: title,      order: asc  } }
    logs:  { limit: 100, window_days: 30 }
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
  - slug: builder
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

## Update when

- A new `config.*` field lands in `internal/config/loader.go` / `internal/config/validator.go`.
- An existing field's validation rule, default value, or allowed range changes.
- The MCP / TUI / SQLite engine contract changes (new PRAGMA, retention rule, response shape).

For entity wiring schemas, see [entities.md § update when](entities.md#update-when). For path / layout changes, see [path-resolution.md](path-resolution.md).
