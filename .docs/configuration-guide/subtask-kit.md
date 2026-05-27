# Sub-task kit cascade

A second kit file can be wired beside the root kit so sub-tasks run a lighter (or heavier) workflow than their parents. The root kit owns the parent task lane; the sub-kit owns the per-bucket grid the detail view paints under the parent. Guards, hooks, and the detail-view board all resolve by task depth.

This guide covers the cascade scope, the YAML shape, the validator rules, the migration and transparency-notice behavior, the dedicated `task.bucket_orphaned` event schema, and the depth-aware hook subject metadata.

## Cascade scope

Per-depth resolution applies to **task shape only**:

- **In** — `workflows[]` (buckets / transitions / guards), `config.hooks`, and the detail-view sub-tasks board render.
- **Out** — `mcp_commands`, persona, laws, templates, skills. Every protocol concern always resolves at the project root kit, regardless of the task's depth.

The boundary keeps MCP prompt assembly identical for root and sub-tasks: `tasks.continue <sub_id>` returns the same persona / laws / templates that `tasks.continue <root_id>` would. Only the workflow surface (which bucket the task can move into, which guard fires, which hook gets dispatched) shifts with depth.

## Sub-task creation

A new sub-task always lands in the **first bucket of the kit that resolves for it**: the sub-kit's `workflows[0].buckets[0]` when `subtask_kit:` is configured, the root kit's `workflows[0].buckets[0]` otherwise. The pre-cascade behaviour of inheriting the parent task's current bucket is gone — a fresh sub-task is new work and belongs at the workflow's start, not at the parent's current step.

Operators wiring guards on the **outbound** edges of the first bucket (e.g. `backlog → dev`) must confirm the guard set is appropriate for inception — newly created sub-tasks will hit those guards on their first move. If the first bucket should remain a friction-free intake lane, leave the `backlog → dev` transition guard-free.

## Reparent across kits

Editing a task's parent (`tasks.edit task_id change_parent=true new_parent_id=<id|null>` or the form's Parent field in the TUI) can shift the row's resolved kit — promoting a root task to a sub-task crosses into the sub-kit, clearing the parent collapses a sub-task back to the root kit. When the task's current bucket key does **not** exist in the new resolved kit, the reparent helper force-rebinds the row to the new kit's first bucket — same policy `AddSub` applies to fresh sub-tasks. The reparent never rejects on bucket incompatibility; the row is never stuck pointing at a bucket the new kit does not know.

`TaskService.Edit` re-reads the task after a parent change so the returned `Depth`, `ParentID`, and resolved `BucketKey` reflect the post-write state — combined field-edit + reparent calls return one consistent row, not a half-stale snapshot. The recovery path emits `task.moved` for the forced bucket shift so the audit log carries both the parent change and the bucket rebind.

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

### Atomic root + sub rebind

Cascade migrations run inside a **single repository transaction**. The root-tree rebind and the sub-task rebind execute back-to-back in one `BeginTx`, and the joint `Commit` is the only point where rows become visible to readers. A sub-task failure mid-pass rolls the root-pass writes back too — partial migrations cannot happen.

Events follow the same gate: every `task.migrated` (root pass) and `task.bucket_orphaned` (sub-task pass) is buffered, then published only after the joint commit succeeds. A rollback discards the buffered events too, so subscribers never see a half-finished migration.

App callers go through `app.OrphanRepository.RebindOrphanedCascade(ctx, projectID, plan)` — a single call with a `domain.OrphanCascadePlan` carrying both resolver pairs and the kit identities. `PreviewOrphanedCascade(ctx, projectID, plan)` is the read-only twin; the TUI bundle-swap prompt and the confirmed migrate run it through the same plan so the preview row set matches the rows the migrate will rewrite. The `domain.OrphanCascadePlan` struct lives in `internal/domain/orphan_cascade.go` and is the contract callers compose against.

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

The three event-payload shapes (`task.bucket_orphaned`, `task.migrated`, `subtask_kit.notice_emitted`) live in `internal/domain/orphan.go` as exported Go structs (`TaskBucketOrphanedPayload`, `TaskMigratedPayload`, `SubtaskKitNoticePayload`). Audit consumers can import the domain package and unmarshal payloads against the typed schema directly instead of working off raw JSON.

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

`subject_depth` is the materialised distance from the nearest root ancestor — 0 for root rows, 1 for direct children, 2 for grandchildren, and so on. The value comes from the persisted `tasks.depth` column (migration 028) so payloads carry the real depth without a recursive parent-walk at emission time; audit consumers and depth-aware hook filters can key off the exact value rather than the legacy `0/1` binary marker. The single payload helper lives at `internal/domain/event_payloads.go::NewTaskSubjectPayload` so the JSON shape stays in one place across the storage and app layers.

Dispatch rules:

- Root kit hooks (declared in the project root kit's `config.hooks`) fire only when `subject_depth == 0`.
- Sub-kit hooks (declared in the sub-kit's `config.hooks`) fire only when `subject_depth >= 1`.
- The subject is **always** the directly affected task. `AddSub` uses the new child as subject; moving / editing a sub-task uses the sub-task; root-task creation and moves use the root task. Child changes never trigger parent hooks unless the parent itself is touched by the same action.
- When `subtask_kit:` is absent, every hook fires from the root kit regardless of subject depth — matches pre-#281 behaviour. The depth predicate only activates once a sub-kit is wired.

When no `subtask_kit:` is configured, every event resolves through the root kit regardless of depth — matches the pre-cascade behavior.

### `guard.violated` carries the same subject metadata

Task-scoped guard violations (`guard.violated` with `entity_type: task`) emit through `guards.Evaluator.EmitViolatedForTask`, which stamps the payload with the same `subject_task_id` / `subject_parent_id` / `subject_depth` / `resolved_kit` block every other task event already carries. A sub-task guard rejection therefore matches `SubjectDepth: subtask` hooks only — root-kit hooks scoped to `SubjectDepth: root` no longer absorb sub-task violations the way the pre-fix code did.

Production callsites are migrated: bucket-policy denials in `TaskService.Edit` / `Delete` / `Archive` / `Unarchive`, transition rejections in `WorkflowService.MoveTask`, comment-edit / -delete policy denials in `CommentService`, and every built-in guard check (`blockers_in`, `comments_min`, `comments_tagged`, `wave_gate`, `subtasks_complete`) all flow through the task-aware helper. Non-task entity callsites (none in production today) can keep using the bare `EmitViolated` path; the typed `task` overload is the depth-aware one.

## Notifications per kit

When a sub-kit ships its own `notifications/<slug>.yaml`, the slug resolves through the sub-kit catalog at runtime — not just at validation time. The hooks engine threads the resolved kit identity through the action dispatch (`_notification_resolved_kit` arg), and the notification action looks the slug up in `NotificationBundleSnapshot.NotificationsByKit[<resolved_kit>][<slug>]` first, falling back to the legacy global `Notifications` map when the per-kit catalog has no entry for the slug.

The practical effect: a sub-kit hook entry like

```yaml
config:
  hooks:
    - on: task.bucket_orphaned
      notification: subtask-orphaned-warning   # declared in sub-kit notifications/
```

now dispatches against `notifications/subtask-orphaned-warning.yaml` loaded from the sub-kit, even if the root kit never references that slug. Collisions resolve per-kit: a root and sub-kit can both define `notifications/<same-slug>.yaml` with different chrome / copy and each kit's hooks see its own version.

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

### Picker identity is the full relative path

Default and custom kits with the same basename are **distinct identities** in the picker. A stock `config/izakaya.yaml` and a `config/custom/izakaya.yaml` show up as two rows; the active dot lands on whichever row's `RelativePath` matches the `subtask_kit:` value written in `omakiten.yaml`. Selecting the custom row writes `subtask_kit: custom/izakaya.yaml` so the loader resolves the user override; selecting the default writes `subtask_kit: izakaya.yaml`. The `subtask_kit:` value round-trips through the YAML byte-for-byte so the resolver never collapses the two onto the same kit.

### Rollback restores disk AND runtime

If the candidate sub-kit fails to load (validator rejection, nested cascade, YAML parse error), the picker's rollback path rewrites the prior `subtask_kit:` value AND re-runs the bundle reload against the restored file so the cache rotates back to the previous snapshot. The user sees the error inline; the runtime ends up driving the same configuration the YAML names. The pre-fix code rewrote only the YAML and left the cache rotated to the bad bundle — the new transactional helper closes that window.
