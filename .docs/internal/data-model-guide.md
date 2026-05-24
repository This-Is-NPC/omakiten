# Data Model Guide

Omakiten persists state in a single SQLite file (default `~/.local/share/omakiten/omakiten.db`, pure-Go driver `modernc.org/sqlite`). The schema is owned by the migration files under `migrations/` and applied transactionally on every connect (`internal/sqlite/store.go:Open`).

> **CQRS-like split — post Phase 2-bis.** YAML files (the active profile yaml plus per-entity markdown) are the **only** source of truth for config. SQLite is **operational data only** — tasks, comments, dependencies, context entries, tags, errors, solutions, plans, plan_waves, task assignment, and the unified events log. Migration 020 dropped every config table from the database; the runtime now resolves workflows, buckets, personas, skills, laws, and templates from an in-memory `config.Snapshot` rebuilt on every `ConfigService.Import` (see `.docs/configuration-guide.md` § In-memory providers).

## Migrations

Schema versions are tracked in `schema_migrations(version)`. Each numbered file under `migrations/` is applied once, in order:

| File | What it adds |
|---|---|
| `001_initial.sql` | Core tables: `projects`, `config_bundles`, `settings`, `skills`, `personas`, `persona_skills`, `laws`, `workflows`, `workflow_buckets`, `workflow_transitions`, `tasks`, `comments`, `task_dependencies`, `context_entries`. |
| `002_entities.sql` | Adds `description` / `body` / `source_path` to entity tables; introduces law `scope` (`global`/`project`/`persona`) with `project_id` / `persona_id` references; adds `tasks.workdir` / `tasks.branch` (both later removed in 017). |
| `003_activity_logs.sql` | `activity_logs` table for operational telemetry (since absorbed into `events`). |
| `004_tags.sql` | `tags`, `task_tags`, `project_tags` join tables. |
| `005_transition_guards.sql` | `workflow_transitions.guards_json` column (later dropped with the table in 020). |
| `006_comment_tags.sql` | `comment_tags` join table (later absorbed into `event_tags`). |
| `007_errors.sql` | `errors`, `solutions`, `error_tags`. Errors and solutions are intentionally **cross-project**. |
| `008_solution_likes.sql` | `solutions.likes` counter, incremented by `solutions.confirm(success=true)`. |
| `009_events.sql` | Unified `events` table; migrates `comments` and `activity_logs` into it; rekeys `comment_tags` as `event_tags`; **drops** `comments`, `comment_tags`, `activity_logs`. |
| `010_agent_attribution.sql` | Adds `agent_model` (NOT NULL DEFAULT '') and nullable `agent_session_id` to `events`, `errors`, and `solutions`; adds `source` / `entrypoint` to `errors` / `solutions`; creates `idx_events_agent_type(agent_model, event_type, created_at)` for the per-model benchmark queries. Existing rows are not backfilled — the domain-event timeline starts at this migration. |
| `011_purge_tui_summary_pollution.sql` | One-shot delete of legacy `operation` rows written by the TUI Stats tick before `activity.WithoutTracking` wrapped the refresh context. Bounded the activity log so legitimate CLI / MCP entries could no longer be evicted by the per-second poll. |
| `012_task_state.sql` | Adds `tasks.state TEXT NOT NULL DEFAULT 'active' CHECK ('active','archived')` and `idx_tasks_project_state(project_id, state)`. Archive bypasses bucket policy / transition guards but still respects `operations.archive.guards`. |
| `013_bucket_permissions_operations.sql` | Adds `workflow_buckets.permissions_json` (per-bucket CRUD policy for tasks and comments) and `workflows.operations_json` (per-workflow archive/delete/unarchive guards). Both later dropped with the tables in 020 — policy now lives in the YAML and the in-memory Snapshot. |
| `014_workflow_defaults.sql` | Adds `workflows.defaults_json` for task/comment edit/delete defaults at the workflow level. Also dropped with the table in 020. |
| `015_priority_id.sql` | Converts `tasks.priority` (TEXT enum `low`/`normal`/`high`) into `tasks.priority_id` (INTEGER referencing the in-bundle `config.priorities` table). Backfill encodes the canonical `1=low`, `2=normal`, `3=high` mapping; subsequent renames in YAML never rewrite the integer ids. |
| `016_severity_id.sql` | Same shape as 015 for `laws.severity` → `laws.severity_id` (`1=info`, `2=warning`, `3=error`). The `laws` table itself was dropped in 020; the severity id is now read from frontmatter at bundle import time. |
| `017_drop_priority_severity_defaults.sql` | Rebuilds `tasks` and `laws` to drop the `DEFAULT 2` clauses on `priority_id` / `severity_id` (the SQL default obscured the principle that the canonical default lives in `defaults/omakiten.yaml`). The `tasks` rebuild also drops the unused `workdir` / `branch` columns added in 002. |
| `018_drop_legacy_event_payloads.sql` | Deletes pre-refactor rows for `task.created`, `task.edited`, `task.removed`, `task.archived`, `task.unarchived`. Their payloads embedded label strings via a process-global registry, but the new wire shape emits the raw int id — purging avoids forcing every reader to accept both shapes. |
| `019_unify_tool_call_events.sql` | Renames every `event_type='operation'` row to `cli.tool_call` / `mcp.tool_call` / `tui.tool_call` based on `source`. Enriches `payload` with `{tool_name, source, entrypoint, status, duration_ms, error_message, args}` so hooks can match without reading SQL columns. The legacy operation columns stay populated so `metrics.summary` keeps using its column-backed index. |
| `020_drop_config_tables.sql` | **The Phase 2-bis breaking migration.** Drops `config_bundles`, `settings`, `skills`, `personas`, `persona_skills`, `laws`, `workflows`, `workflow_buckets`, `workflow_transitions`. Before the drop, rewrites `tasks.bucket_id` from the SQL-era `workflow_buckets.id` (autoincrement PK) to `workflow_buckets.local_id` (the YAML-declared bucket id the post-migration `Snapshot` indexes by) so existing tasks still resolve to a real bucket. Rebuilds `tasks` to drop the FK pointing at `workflow_buckets`; `bucket_id` is now a plain `INTEGER`. |
| `021_rebind_orphan_buckets.sql` | Pure-SQL recovery for databases that applied an earlier version of 020 missing the bucket rebind. Walks `events` for each task's latest `task.moved` (fallback `task.created`) payload, extracts the bucket key, and maps onto the canonical preset bucket id via a `CASE` covering every shipped preset key. Tasks with no recoverable event land in bucket id 1. Idempotent on already-rebound databases. |
| `022_search_index.sql` | Creates the unified FTS5 virtual table `search_index(content, entity_type UNINDEXED, entity_id UNINDEXED, project_id UNINDEXED)` with tokenizer `porter unicode61` and seeds it from the live rows: tasks (title + description), comments (the `events` rows where `event_type='comment'`), errors (description + context), solutions (description + steps), and context entries. Triggers keep the index in sync on insert / update / delete; `solutions` rows derive `project_id` via `errors.error_id`. Backs the unified `search` MCP tool that replaced the legacy `errors.search` path. |
| `023_plans.sql` | Adds the WBS-style plan catalog. Creates `plans` (id, project_id FK, slug, name, goal_body markdown, status `active`/`done`/`abandoned`, completed_at, `UNIQUE(project_id, slug)`) and `plan_waves` (id, plan_id FK ON DELETE CASCADE, name, position, `UNIQUE(plan_id, position)`). Adds three nullable columns on `tasks`: `plan_id REFERENCES plans(id) ON DELETE SET NULL`, `wave_id REFERENCES plan_waves(id) ON DELETE SET NULL`, and `assigned_to TEXT`. Creates `idx_tasks_plan_wave(plan_id, wave_id)`. Tasks survive plan deletion as standalone work items; deleting a plan cascades its waves but only nulls the task pointers. |
| `024_search_index_plans.sql` | Extends the FTS5 `search_index` (migration 022) with a sixth content type: `plan` rows indexed as `name + ' ' + goal_body`. Backfills from `plans` and installs three triggers (`search_index_plans_ai`/`au`/`ad`) so the cross-project `search` MCP tool finds plans by name or any phrase in the markdown goal body. |

After 009, three tables that older code referenced (`comments`, `comment_tags`, `activity_logs`) **no longer exist** — every reader/writer goes through `events` (see "The unified events table" below). After 020, nine more tables (`config_bundles`, `settings`, `skills`, `personas`, `persona_skills`, `laws`, `workflows`, `workflow_buckets`, `workflow_transitions`) are also gone — config is YAML-only. Migration 022 adds the FTS5 virtual table `search_index`, which is not a base table — it does not show up in the table count below, but every row inserted into `tasks`, `events` (comments), `errors`, `solutions`, `context_entries`, or `plans` is mirrored into it by trigger. Migration 023 adds `plans` and `plan_waves` (plus three nullable columns on `tasks`); 024 extends the FTS5 index to plan rows.

## Current schema (post-024)

The live schema contains fourteen base tables of operational state plus `schema_migrations` and the `search_index` FTS5 virtual table:

```
schema_migrations      tags
projects               task_tags
tasks                  project_tags
task_dependencies      error_tags
context_entries        event_tags
errors                 events
solutions              plans
plan_waves             search_index (FTS5 virtual)
```

The diagram below reflects that shape. Crow's-foot reads as: `||--o{` is one-to-many; pure-junction tables (`*_tags`, `task_dependencies`) sit between the two entities they link.

```mermaid
erDiagram
    PROJECTS {
        int  id PK
        text slug "UNIQUE"
        text root_path "UNIQUE"
        text name
    }
    TASKS {
        int  id PK
        int  project_id FK
        int  bucket_id "nullable, resolved via Snapshot"
        text title
        int  priority_id "config.priorities id"
        text state "active|archived"
        int  plan_id FK "nullable, ON DELETE SET NULL"
        int  wave_id FK "nullable, ON DELETE SET NULL"
        text assigned_to "nullable, free-text"
        text completed_at "nullable, terminal-bucket stamp"
    }
    PLANS {
        int  id PK
        int  project_id FK
        text slug "UNIQUE per project"
        text name
        text goal_body "markdown"
        text status "active|done|abandoned"
        text completed_at
    }
    PLAN_WAVES {
        int  id PK
        int  plan_id FK "ON DELETE CASCADE"
        text name
        int  position "UNIQUE per plan"
    }
    TASK_DEPENDENCIES {
        int project_id FK
        int task_id FK
        int depends_on_task_id FK
    }
    CONTEXT_ENTRIES {
        int  id PK
        int  project_id FK
        int  token_estimate
    }
    TAGS {
        int  id PK
        text name "UNIQUE, kebab-case"
        text label
    }
    TASK_TAGS {
        int project_id FK
        int task_id FK
        int tag_id FK
    }
    PROJECT_TAGS {
        int project_id FK
        int tag_id FK
    }
    ERRORS {
        int  id PK
        int  project_id FK "nullable"
        text description
        text agent_model
    }
    SOLUTIONS {
        int  id PK
        int  error_id FK
        int  success "NULL|0|1"
        int  likes
        text agent_model
    }
    ERROR_TAGS {
        int error_id FK
        int tag_id FK
    }
    EVENTS {
        int  id PK
        text entity_type "task|system|project|error|solution"
        int  entity_id "nullable"
        int  project_id FK "nullable"
        text event_type
        text payload "JSON"
        text agent_model
    }
    EVENT_TAGS {
        int event_id FK
        int tag_id FK
    }

    PROJECTS ||--o{ TASKS           : owns
    PROJECTS ||--o{ CONTEXT_ENTRIES : owns
    PROJECTS ||--o{ ERRORS          : "optional scope"
    PROJECTS ||--o{ PROJECT_TAGS    : "tagged via"
    PROJECTS ||--o{ EVENTS          : "optional scope"
    PROJECTS ||--o{ PLANS           : owns

    PLANS ||--o{ PLAN_WAVES : "ordered phases"
    PLANS ||--o{ TASKS      : "groups (nullable)"
    PLAN_WAVES ||--o{ TASKS : "wave member (nullable)"

    TASKS ||--o{ TASK_DEPENDENCIES : "blocked by"
    TASKS ||--o{ TASK_DEPENDENCIES : "blocker of"
    TASKS ||--o{ TASK_TAGS         : "tagged via"
    TASKS ||--o{ EVENTS            : "entity_type=task"

    TAGS ||--o{ TASK_TAGS    : tags
    TAGS ||--o{ PROJECT_TAGS : tags
    TAGS ||--o{ ERROR_TAGS   : tags
    TAGS ||--o{ EVENT_TAGS   : tags

    ERRORS ||--o{ SOLUTIONS  : "candidate fixes"
    ERRORS ||--o{ ERROR_TAGS : "tagged via"

    EVENTS ||--o{ EVENT_TAGS : "tagged via"
```

A few invariants the diagram cannot express compactly:

- **Project-scope invariant** for tasks: `tasks(project_id, id)` is a composite unique key, and `task_dependencies` uses dual composite FKs into it — this is what guarantees a dependency can never cross projects.
- **Cycle prevention** for `task_dependencies` is enforced in software (`internal/graph/dependency.go:HasCycle`), not by the schema.
- **`tasks.bucket_id`** is an unconstrained `INTEGER` post-020 — there is no FK to a buckets table because no buckets table exists. The application resolves it against the per-project `config.Snapshot.BucketByID` built from YAML on every bundle import. An id Snapshot cannot resolve marks the row as an **orphan** and surfaces through `app.OrphanRepository.PreviewOrphanedTasks` / `RebindOrphanedTasks` (see `.docs/configuration-guide.md` § Orphan-task migration).
- **`tasks.priority_id`** is similarly unconstrained at the SQL layer. Validation is the bundle validator's job: every `priority_id` written must match an entry in `config.priorities`. Renaming a priority label is a YAML edit; the integer id stored on tasks does not change.
- **`events` is a discriminated log**: `(entity_type, event_type)` selects the row's role (see "The unified events table" below). `entity_id` is the task / error / solution id when the entity type names a row, and is `NULL` for `entity_type='system'`.
- **`solutions.success`** is a tri-state (`NULL` = untried, `0` = known-bad, `1` = known-good); `1` is the only state that increments `likes`.

## Tables

### `projects`

`id INT PK`, `name`, `slug UNIQUE`, `root_path UNIQUE`, `created_at`, `updated_at`, `archived_at?`.

The active project is resolved by id, slug, or by matching `root_path` to the current working directory (`internal/project/resolver.go`). When a project's `root_path` carries a `.omakiten/` directory, the runtime uses that repo-local SQLite file in place of the global one (`internal/agentruntime/runtime.go`).

### `tasks`

Post-023 column shape:

```
id           INTEGER PRIMARY KEY AUTOINCREMENT
project_id   INTEGER NOT NULL REFERENCES projects(id)
bucket_id    INTEGER                     -- no FK; resolved via Snapshot
title        TEXT    NOT NULL
description  TEXT    NOT NULL DEFAULT ''
priority_id  INTEGER NOT NULL            -- references config.priorities[*].id
state        TEXT    NOT NULL DEFAULT 'active'
                     CHECK (state IN ('active','archived'))
created_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
updated_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
completed_at TEXT                        -- stamped on transition INTO terminal bucket;
                                          -- cleared on transition OUT
plan_id      INTEGER REFERENCES plans(id) ON DELETE SET NULL
wave_id      INTEGER REFERENCES plan_waves(id) ON DELETE SET NULL
assigned_to  TEXT                        -- free-text claimant; populated by
                                          -- plans.claim_next or okt assign
UNIQUE(project_id, id)
```

The `UNIQUE(project_id, id)` shape is what lets `task_dependencies` use a composite foreign key to enforce that **dependencies cannot cross projects**.

`completed_at` is populated by `WorkflowService.MoveTask` whenever the destination is the workflow's final bucket and cleared when a task leaves the terminal bucket. Existing historical `done` rows are backfilled to `updated_at` once per `BuildProjectRuntime` via `Store.BackfillTaskCompletedAt` (`internal/sqlite/tasks_lifecycle.go`); the backfill is idempotent (zero rows after the first run) and errors are swallowed so a transient SQLite hiccup cannot block runtime composition. Tasks that bounced in/out of `done` lose the original completion moment — best-effort by design.

`plan_id` / `wave_id` / `assigned_to` are NULL for every task created before migration 023 and for any task not attached to a plan. Behavior is identical to pre-023 across CLI, TUI, MCP, guards, and metrics for those rows; the `wave_gate` guard returns `0` (no-op pass) when `wave_id IS NULL`.

Indexes:

- `idx_tasks_project_bucket(project_id, bucket_id)` — feeds bucket-filtered list views (board, table).
- `idx_tasks_project_state(project_id, state)` — feeds the archived-tasks toggle (`TaskFilter.IncludeArchived`).
- `idx_tasks_plan_wave(plan_id, wave_id)` — feeds plan/wave projections (`PlanService.Show`, network diagram).

### `plans`

```
id           INTEGER PRIMARY KEY AUTOINCREMENT
project_id   INTEGER NOT NULL REFERENCES projects(id)
slug         TEXT    NOT NULL
name         TEXT    NOT NULL
goal_body    TEXT    NOT NULL DEFAULT ''  -- markdown goal + acceptance criteria
status       TEXT    NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active','done','abandoned'))
created_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
updated_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
completed_at TEXT
UNIQUE(project_id, slug)
```

A plan groups child tasks into ordered waves. v1 is **single-project** by design (`project_id NOT NULL`); cross-project plans are a deliberate follow-up (`plan_projects(plan_id, project_id)` junction would replace the direct FK). Plan status auto-transitions to `done` when the last child task closes — there is no separate `requirements` entity; optional human-authored acceptance criteria live in `goal_body`.

`plans.goal_body` is mirrored into the FTS5 `search_index` virtual table (entity_type `plan`, content = `name + ' ' + goal_body`) via migration 024 so cross-project `search` finds plan goals.

### `plan_waves`

```
id       INTEGER PRIMARY KEY AUTOINCREMENT
plan_id  INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE
name     TEXT    NOT NULL
position INTEGER NOT NULL
UNIQUE(plan_id, position)
```

Waves are ordered phases inside a plan. Tasks within a wave run in parallel; wave `N+1` is gated on wave `N` being fully closed via the `wave_gate` guard (see `.docs/guards-guide.md`) — gating is **not** modelled as auto-wired dependency edges so the network diagram stays clean and the edge count stays linear instead of N×M.

`ON DELETE CASCADE` on `plan_id` ensures waves disappear with their plan; child tasks keep `state='active'` because `tasks.plan_id` / `tasks.wave_id` are `ON DELETE SET NULL` (deleting a plan never deletes its work, only detaches it).

### `task_dependencies`

`(project_id, task_id, depends_on_task_id)` PK, with `CHECK (task_id != depends_on_task_id)` and dual FKs into `tasks(project_id, id)`. The `app.DependencyService` adds cycle detection in software (`internal/graph/dependency.go:HasCycle`) since SQLite cannot enforce DAG-ness.

### `context_entries`

`id`, `project_id`, `body`, `token_estimate`, `created_at`. Project-scoped handoff notes consumed by `context.dump` (`internal/app/context_service.go`).

### `tags`

`id`, `name UNIQUE` (kebab-case-normalized via `app.NormalizeTagName`), `label`, `created_at`.

Four join tables attach tags:

| Join | Reference | Scope |
|---|---|---|
| `task_tags` | `(project_id, task_id, tag_id)` | task ↔ tag |
| `project_tags` | `(project_id, tag_id)` | project ↔ tag |
| `error_tags` | `(error_id, tag_id)` | error ↔ tag (cross-project) |
| `event_tags` | `(event_id, tag_id)` | event ↔ tag — used to tag comments (which are events) |

`tags.merge` reassigns rows from one tag id to another and deletes the source. Orphan tag cleanup is exposed via `TagRepository.DeleteOrphanTags`.

### `errors`, `solutions`, `error_tags`

```
errors:    id, description, context, project_id?, created_at,
           source, entrypoint, agent_model, agent_session_id?
solutions: id, error_id (FK), description, steps,
           success NULL|0|1, task_id?, tried_at?, created_at, likes,
           source, entrypoint, agent_model, agent_session_id?
error_tags: error_id, tag_id          -- cascades on errors delete
```

**Cross-project by design.** Errors carry an optional `project_id` (so you can filter), but the unified `search` tool (with `entity_types=["error"]`) and `solutions.list_top` are global so prior fixes are reusable across projects (see `.docs/mcp-guide.md` § Tools).

`solutions.success` is a tri-state:

- `NULL` — recorded but never tried.
- `0` — known-bad (the agent should not retry without new context).
- `1` — known-good — increments `solutions.likes` (`migration 008`).

The `source` / `entrypoint` / `agent_model` / `agent_session_id` columns (`migration 010`) denormalize the calling agent's identity from the `internal/activity` context at write time. They feed `metrics.summary` directly without a join. `agent_session_id` is nullable so absent sessions don't distort `GROUP BY` queries; `agent_model=""` marks non-agent traffic (TUI human, system internals) and is filtered out of per-model benchmarks.

Indexes: `idx_errors_project`, `idx_errors_created_at(DESC)`, `idx_solutions_error`, `idx_solutions_likes(DESC)`.

## The unified `events` table

After migration 009, **comments**, **task lifecycle events**, **operational telemetry**, and (after migration 010) **domain events** all live in one append-only log. The discriminators are `entity_type` and `event_type`:

| `entity_type` | `event_type` | `entity_id` | Use |
|---|---|---|---|
| `task` | `comment` | task id | Comment authored by a human or agent. `body` carries the comment text; `author_type` is `'human'`/`'agent'`. |
| `task` | `comment.edited` | task id | Comment body or tag set updated. `payload={comment_id, body:{from,to}}`. |
| `task` | `comment.removed` | task id | Comment hard-deleted. `payload={comment_id, author_type, body}`. |
| `task` | `task.created` | task id | Emitted in the same transaction as the `tasks` insert. `payload={bucket}`. |
| `task` | `task.moved` | task id | Emitted by the task repo when `bucket_id` changes via a transition. `payload={from, to}`. |
| `task` | `task.migrated` | task id | Emitted when a task is rebound by a workflow swap (preset change, bucket removed/renamed). Distinct from `task.moved` because transition guards are bypassed. `payload={from, to, reason:"workflow_swap"}`. |
| `task` | `task.completed` | task id | Emitted by `app.WorkflowService.MoveTask` when the destination bucket is the workflow's final bucket. `payload={bucket}`. |
| `task` | `task.edited` | task id | Mutable fields changed. `payload={title?:{from,to}, description?:{from,to}, priority?:{from,to}}` — only keys for fields that actually changed are present; `priority.from`/`priority.to` are the integer priority ids. |
| `system` | `task.removed` | task id | Task hard-deleted. Emitted on the surviving `system` entity row (the task row is gone by then). `payload={task_id, title, description, priority, bucket_key, state}`. |
| `task` | `task.archived` | task id | Task moved to `state='archived'`. `payload={from_bucket, to_bucket, from_state, to_state}` — bucket transitions to the workflow's final bucket atomically with the state flip. |
| `task` | `task.unarchived` | task id | Previously-archived task restored to `active`. `payload={from_bucket, to_bucket, from_state, to_state}` — bucket stays where the task currently sits. |
| `task`/`project`/`error` | `tag.added` / `tag.removed` | entity id | Tag attached/detached. `payload={entity_type, entity_id, tag_id, tag_name}`. |
| `task` | `dependency.added` / `dependency.removed` | dependent task id | Dependency edge insert/delete. `payload={depends_on_task_id}`. |
| `task`/`comment` | `guard.violated` | task/comment id per `payload.target` | Any operation rejected by a configured guard. `payload={operation, rule, hint, target, attempted_by}` — `operation` and `rule` are free-form strings supplied by the call site. |
| `system` | `cli.tool_call` / `mcp.tool_call` / `tui.tool_call` | (null) | Per-call activity log entry written by `activity.Track`. `payload={tool_name, source, entrypoint, status, duration_ms, error_message, args}` mirrors the operation columns so hooks can filter without reading SQL columns. Source-discriminated since migration 019; the legacy `operation` event_type is deprecated and no longer emitted. |
| `system` | `hook.executed` | (null) | Hook action finished (success or failure). `payload={hook_index, action, event_type, target_event_id, success, error, duration_ms}`. |
| `system` | `bundle.swapped` | (null) | Active config bundle replaced via the TUI hot-reload path. `payload={from_workflow, to_workflow, orphan_count, groups}`. |
| `system` | `bundle.imported` | (null) | A fresh bundle reached the runtime (source-of-truth flipped). `payload={path, hash, workflow_key, workflow_count, persona_count, skill_count, law_count, template_count}`. |
| `system` | `confirmation.granted` | (null) | TUI dispatched a non-empty `NotificationAction.Command` in response to a user keystroke. `payload={notification_slug, action_id, command}`. |
| `error` | `error.recorded` | error id | `app.ErrorService.Record` persisted a new error row. `payload={tags, has_context}`. |
| `error` | `error.searched` | (null) | `app.SearchService.Search` ran (unified FTS5 across tasks / comments / errors / solutions / context entries). `payload={query, entity_types, result_count, unified}`. |
| `solution` | `solution.added` | solution id | `app.ErrorService.AddSolution` persisted a candidate. `payload={error_id}`. |
| `solution` | `solution.confirmed` | solution id | `ConfirmSolution` ran (regardless of outcome). Co-emits with `solution.liked` or `solution.failed`. `payload={error_id, success, likes}`. |
| `solution` | `solution.liked` | solution id | `ConfirmSolution(success=true)`. `payload={error_id, likes}`. |
| `solution` | `solution.failed` | solution id | `ConfirmSolution(success=false)`. `payload={error_id, likes}`. |
| `solution` | `solution.viewed_top` | (null) | `ListTopSolutions` ran. `payload={limit, returned_count}`. |

The canonical event-type vocabulary is the `EventType*` constants in `internal/domain/event.go`; the closed set lives in `domain.KnownEventTypes` (consumed by config validation to reject hook overrides referencing typos). `agent_model` and `agent_session_id` are populated from the request context on every domain event (and on every `*.tool_call` row). `metrics.summary` aggregates these rows by `agent_model` to benchmark agent behaviour.

The `events` row carries every column it might need; unused columns are nullable. Three indexes:

- `idx_events_entity(entity_type, entity_id, created_at)` — feeds the per-task activity feed (`task_activity.list` MCP tool, the TUI's activity column, `internal/sqlite/events.go:ListTaskActivity`).
- `idx_events_type_started(event_type, created_at)` — feeds the operational logs view (`internal/sqlite/activity_logs.go:ListActivityLogs`) and the synchronous pruner.
- `idx_events_agent_type(agent_model, event_type, created_at)` — feeds the `metrics.summary` aggregation queries (`internal/sqlite/metrics.go:AgentMetricsSummary`).

### Pruning policy (`*.tool_call` only)

After every successful `*.tool_call` insert, those rows are pruned synchronously by `internal/sqlite/activity_logs.go:PruneActivityLogs`. Both knobs come from the user's bundle (`config.activity_log.max_rows`, `config.activity_log.max_age_days`) and are wired into the `Store` at composition-root time via `Store.SetActivityLogRetention`. The kit canonical (`defaults/config/omakase.yaml`) ships:

- **Max age**: 7 days (`config.activity_log.max_age_days`).
- **Max rows**: 500 most-recent (`config.activity_log.max_rows`).

Both fields are validator-required (> 0) — disabling retention is not a supported mode.

**Comments, task lifecycle, domain, and system events are not pruned** — they are durable history. Pruning is scoped to the tool-call entries.

### Reader / writer cheat-sheet

| Surface | What it sees | File |
|---|---|---|
| `comments.list` (CLI/MCP/TUI) | `entity_type='task' AND event_type='comment'` ordered ascending by id | `internal/sqlite/comments.go:ListComments` |
| `task_activity.list` MCP tool, TUI activity column | `entity_type='task'` ordered chronologically (asc default, desc optional) | `internal/sqlite/events.go:ListTaskActivity` |
| Logs view (TUI), `okt mcp call` history | `event_type IN ('cli.tool_call','mcp.tool_call','tui.tool_call')` ordered desc; legacy `operation` rows still surface for back-compat | `internal/sqlite/activity_logs.go:ListActivityLogs` |
| Guard `comments_min` | `count(*) WHERE entity_type='task' AND event_type='comment' AND entity_id=?` | `internal/sqlite/guards.go:CountTaskComments` |
| Guard `comments_tagged` | join `events` ⨝ `event_tags` ⨝ `tags` filtered by tag name | `internal/sqlite/guards.go:CountTaskCommentsTagged` |
| `metrics.summary` | `events` grouped by `agent_model`, `event_type`, filtered on the agent-type index | `internal/sqlite/metrics.go:AgentMetricsSummary` |

## Bucket and priority resolution

`tasks.bucket_id` and `tasks.priority_id` are integer references into per-project YAML data, not SQL FKs.

- **Bucket id → bucket key**: `config.Snapshot.BucketByID(id)` (`internal/config/snapshot.go`). The Snapshot is rebuilt from `config.Bundle.Workflows[*].Buckets[*]` on every `ConfigService.Import` (or hot-reload via `Repositories.Cache.Reload`). The bucket's stable id is the YAML-declared `local_id` (the canonical ids `1=backlog`, `2=dev`, `3=review`, `4=done` for `omakase`; other presets carry their own).
- **Priority id → priority label**: resolved through the `*domain.EnumRegistry` returned by `ConfigService.Import` and injected into every service that needs labels (`TaskService`, `WorkflowService`, `TUIQueryService`, `ContextService`, agent `Service`). The on-the-wire JSON shape is the raw int id — label projection happens at the DTO boundary so different surfaces (TUI, CLI table, MCP DTO) can resolve to different shapes if they want.

A task pointing at a `bucket_id` the active Snapshot cannot resolve is an **orphan**. Orphans surface through `app.OrphanRepository`:

- `PreviewOrphanedTasks` — read-only count + per-task preview (used by the TUI hot-reload prompt).
- `RebindOrphanedTasks` — in-tx rebind to the same key in the new workflow (preserved) or to the first active bucket (removed), emitting `task.migrated` per task.

The same rebind primitive is reached from the CLI (`okt workflow orphans --confirm`) and the MCP (`orphans.migrate` tool, two-phase confirmation) — `internal/sqlite/orphans.go`, `internal/app/orphan_service.go`, `internal/cli/workflow.go`, `internal/agent/service_orphan.go`.

## Project-scope invariant

Every operational query filters by `project_id` at the SQL layer. This is the canonical enforcement point of NFR-007 ("operational data is strictly project-scoped"); the agent layer adds defense-in-depth on top by always materializing a single `ProjectContext` at intent entry (`internal/agent/service.go`).

The cross-project exceptions (errors, solutions, global tag list, template catalog) are explicitly scoped that way in their service methods — they never touch the project filter.

## Connection settings

`internal/sqlite/store.go:Open` configures every connection with:

- `PRAGMA foreign_keys = ON;` — required for the dependency / events / tags FK cascades to fire.
- A busy-timeout to ride through brief contention.

The driver is pure Go (`modernc.org/sqlite`), so the binary builds without CGo.

## Schema auto-migration (config side)

`MigrateLayout` (in `internal/config/migration.go`) runs every time
the CLI / TUI boots through the materialize path and is responsible
for two distinct migration surfaces:

1. **Directory shape** (v0 / v1 / v2 layouts). Documented inline at
   the function's godoc; the v2 shape is the current source of truth.
2. **YAML schema** (`migrateSchemaDefaults` in
   `internal/config/migration_schema.go`). Backfills required keys
   that were added in later releases so user bundles authored before
   those releases survive the next launch without manual edits. The
   helper walks every `<root>/config/*.yaml`, parses to a
   `*yaml.Node` (so user comments + key order survive), and only
   rewrites a profile when at least one of the required keys is
   missing. Identical inputs read through to a byte-identical write
   that is skipped — running it twice is a no-op.

Currently backfilled:

- `config.sqlite.cache_size_kb` (kit canonical: 1024) — required since
  W7 #225.
- `config.sqlite.mmap_size_bytes` (kit canonical: 0) — required since
  W7 #225.

When adding a new required key to the wiring schema, extend
`migrateSchemaDefaultsInFile` with a `mapValueNode(... ) == nil`
check + `appendMapEntry(...)` call rather than letting the validator
break user bundles on upgrade. The pattern keeps every prior version
of the wiring loadable as long as the kit canonical for the new key
is sensible as a backfill default.

## Where to learn more

- Migration sources: `migrations/001_initial.sql` … `migrations/024_search_index_plans.sql`.
- Domain types behind every row: `internal/domain/` (`task.go`, `event.go`, `tag.go`, `error_record.go`, `context.go`, `priority_test.go`, `severity_test.go`).
- Adapter implementations: `internal/sqlite/` (one file per concern — `tasks.go`, `tasks_lifecycle.go` (archive/unarchive/remove), `comments.go`, `dependencies.go`, `events.go`, `tags.go`, `errors.go`, `metrics.go`, `bucket_resolver.go`, `activity_logs.go`, `guards.go`, `contexts.go`, `orphans.go`, `projects.go`, `store.go`).
- App-level ports the adapter satisfies: `internal/app/ports.go`.
- The in-memory side: `.docs/configuration-guide.md` § How config reads work at runtime; `internal/config/snapshot.go`, `internal/agentruntime/cache.go`.
