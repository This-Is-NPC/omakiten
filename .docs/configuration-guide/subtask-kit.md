# Sub-task kit cascade

A second kit file can be wired beside the root kit so sub-tasks run a lighter (or heavier) workflow than their parents. The root kit owns the parent task lane; the sub-kit owns the per-bucket grid the detail view paints under the parent. Guards, hooks, and the detail-view board all resolve by task depth.

This guide covers the cascade scope, the YAML shape, the validator rules, the migration and transparency-notice behavior, the dedicated `task.bucket_orphaned` event schema, and the depth-aware hook subject metadata.

## Cascade scope

Per-depth resolution applies to **task shape only**:

- **In** — `workflows[]` (buckets / transitions / guards), `config.hooks`, and the detail-view sub-tasks board render.
- **Out** — `mcp_commands`, persona, laws, templates, skills. Every protocol concern always resolves at the project root kit, regardless of the task's depth.

The boundary keeps MCP prompt assembly identical for root and sub-tasks: `tasks.continue <sub_id>` returns the same persona / laws / templates that `tasks.continue <root_id>` would. Only the workflow surface (which bucket the task can move into, which guard fires, which hook gets dispatched) shifts with depth.

## YAML shape

The root kit gains an optional top-level key. The path is **relative to the root kit file's own directory** — no absolute paths, no `..` escapes.

```yaml
# omakiten.yaml — root kit
version: 1
kit:
  id: 1
  key: omakase
  name: Omakase
subtask_kit: izakaya.yaml   # or: custom/izakaya.yaml
config:
  workflow:
    active: omakase
workflows:
  - id: 1
    key: omakase
    buckets: [...]
    transitions: [...]
```

The referenced file is a **full kit file** — its own `kit:` identity block, its own `config:`, its own `workflows:`. Partial files (missing any of these blocks) are rejected.

```yaml
# izakaya.yaml — sub-kit
version: 1
kit:
  id: 2
  key: izakaya
  name: Izakaya
config:
  workflow:
    active: izakaya
workflows:
  - id: 2
    key: izakaya
    buckets:
      - { id: 10, key: backlog, name: Backlog, position: 1 }
      - { id: 11, key: dev,     name: Dev,     position: 2 }
      - { id: 12, key: done,    name: Done,    position: 3 }
    transitions:
      - { from: 10, to: 11 }
      - { from: 11, to: 12 }
```

## Validator rules

The loader enforces the cascade shape before any active snapshot is published:

| Rule | Behavior |
|---|---|
| `subtask_kit:` set, file missing or unreadable | Fail-fast load error quoting the path + reason. |
| Sub-kit file missing `kit:` / `config:` / `workflows:` | Rejected. Partial files cannot be promoted to a sub-kit. |
| Sub-kit file declares its own `subtask_kit:` | Rejected. One cascade level only. |
| `subtask_kit:` absolute path or contains `..` | Rejected. Relative-to-kit-dir only. |
| Sub-kit declares `mcp_commands:` | **Warning** (non-fatal): `mcp_commands: ignored at depth >=1; MCP always resolves at project root`. The block is loaded but never consumed. |

Failed validation produces **no partial snapshot swap, no migration trigger, and no transparency notice**. The runtime keeps the prior snapshot until a valid pair lands.

## Migration

Enable, disable, or swap of `subtask_kit:` reuses the existing project-config-swap migration handler. The order is locked:

1. Load and validate the full root + sub-kit graph. Any failure aborts here — no observable effect on the runtime.
2. Swap the active snapshot.
3. Run migration / orphan detection against the incoming resolved kit.

The migration splits the rebind by depth so each task is diffed against **its own resolved kit**:

| Transition | Root tasks | Sub-tasks |
|---|---|---|
| Enable (no sub-kit → sub-kit configured) | Diffed against the unchanged root kit (no orphans expected). | Diffed against the new sub-kit. |
| Disable (sub-kit → none) | Diffed against the unchanged root kit. | Diffed against the root kit (sub-tasks collapse back). |
| Swap (sub-kit A → sub-kit B) | Diffed against the unchanged root kit. | Diffed against sub-kit B. |

Projects without a `subtask_kit:` configured keep the pre-cascade behavior byte-for-byte — the legacy "all tasks" migration path stays the entry point.

## Orphan event schema

Sub-tasks whose bucket key is absent from the incoming resolved kit emit a dedicated event — one per affected sub-task:

```json
{
  "task_id":       <int64>,
  "parent_id":     <int64>,
  "depth":         <int, >=1>,
  "old_bucket":    "<bucket key on the task before migration>",
  "from_kit":      "<previous resolved kit identity>",
  "to_kit":        "<incoming resolved kit identity>",
  "resolved_kit":  "<same as to_kit>",
  "reason":        "bucket_missing_in_resolved_kit"
}
```

- `event_type`: `task.bucket_orphaned`
- `entity_type`: `task`
- `entity_id`: the affected sub-task id

Bucket validity is **key-based**: if the bucket key exists in the incoming resolved kit, no orphan event is emitted even when the bucket id, label, or position changed. Root tasks continue to emit the legacy `task.migrated` event with `reason: workflow_swap` so audit consumers can keep matching the historical payload.

## Hook subject metadata

Task-scoped event payloads carry the resolving subject identity so the dispatcher can route to the correct kit's hooks:

```json
{
  "subject_task_id":   <int64>,
  "subject_parent_id": <int64 | null>,
  "subject_depth":     <int, 0 for root>,
  "resolved_kit":      "<root or sub-kit identity>",
  ...other event fields
}
```

Dispatch rules:

- Root kit hooks (declared in the project root kit's `config.hooks`) fire only when `subject_depth == 0`.
- Sub-kit hooks (declared in the sub-kit's `config.hooks`) fire only when `subject_depth >= 1`.
- The subject is **always** the directly affected task. `AddSub` uses the new child as subject; moving / editing a sub-task uses the sub-task; root-task creation and moves use the root task. Child changes never trigger parent hooks unless the parent itself is touched by the same action.
- When `subtask_kit:` is absent, every hook fires from the root kit regardless of subject depth — matches pre-#281 behaviour. The depth predicate only activates once a sub-kit is wired.

When no `subtask_kit:` is configured, every event resolves through the root kit regardless of depth — matches the pre-cascade behavior.

## Transparency notice

The first time a project enables `subtask_kit:` (transition from no sub-kit to a configured path), the runtime emits a one-shot `subtask_kit.notice_emitted` system event with the i18n key `notice.subtask_kit.enabled.mcp_resolves_at_root`. The TUI surfaces it once so the operator understands the protocol boundary: `mcp_commands` still resolves at the project root regardless of sub-kit contents.

The notice fires **only** on the no-sub-kit → some-path transition. It does **not** fire on:

- Same-path reloads (hot-reload of an unchanged `subtask_kit:` line).
- Sub-kit swaps (path A → path B).
- Disable (sub-kit → no sub-kit).

## Editing flow

Two paths land you at a configured sub-kit:

1. **Settings → General → `s`** opens the sub-task kit picker. It lists every kit file in the active `config/` directory plus a `none (inherit root)` sentinel. Selecting an entry writes (or clears) `subtask_kit:` through the same atomic writer the root-kit picker uses and triggers the hot-reload path — migration + transparency notice fire automatically.
2. **Edit `omakiten.yaml` in `$EDITOR`** and let the file watcher rebuild the snapshot. Same validator + migration + notice fires.

The picker is read-only with respect to the sub-kit file's *contents*; selecting a file does not open it for editing. Authoring stays in `$EDITOR` against the kit YAML directly.
