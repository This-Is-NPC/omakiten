# Workflow Guards

Guards are policy rules attached to a single workflow **transition**. They run in `app.WorkflowService.MoveTask` after the transition is confirmed allowed (`workflow_invalid_transition` is checked first); the first failing guard short-circuits the move with a coded `guard_violation` error.

Guards live next to transitions in the active profile yaml and are evaluated by `internal/app/guards/evaluator.go` (`Evaluator.EvaluateTransition` / `EvaluateOperation`, dispatched per-guard-type via `runGuards`) against the in-memory `*config.Snapshot` rebuilt on every bundle import. Migration 005 originally persisted the JSON on the `workflow_transitions` row; migration 020 dropped that table along with every other config table, so guards are now read directly from YAML via the Snapshot — there is no SQL mirror. Validation runs at `okt config validate` time via `internal/config/validator.go:validateWorkflows`.

The same guard shapes also drive **operation policies** (`operations.{archive,delete,unarchive}.guards`) — see [Operation guards](#operation-guards). Bucket/comment CRUD policy is configured under workflow permissions; this page only documents how permission denials surface as guard failures.

## Contents

- [Where they sit in the move pipeline](#where-they-sit-in-the-move-pipeline)
- [Guard types](#guard-types)
- [Multiple guards on the same transition](#multiple-guards-on-the-same-transition)
- [The `hint` field](#the-hint-field)
- [Validation rules (parse-time)](#validation-rules-parse-time)
- [Worked example (from `defaults/config/omakase.yaml`)](#worked-example-from-defaultsconfigomakaseyaml)
- [Failure shape](#failure-shape)
- [Operation guards](#operation-guards)
- [Permission policy failures](#permission-policy-failures)
- [Adding a new guard type](#adding-a-new-guard-type)
- [See also](#see-also)

## Where they sit in the move pipeline

`app.WorkflowService.MoveTask` runs in this order (`internal/app/workflow_service.go:179`):

1. Validate input (`task_id > 0`, target bucket non-empty).
2. Load `tasks.state`; reject archived tasks with `validation_error "task is archived; unarchive before moving"` (the `tasks.unarchive` MCP tool, `okt unarchive <id>` CLI, or TUI un-archive lift the gate). Archived tasks never reach the workflow flow — `Archive`/`Unarchive` live on `task_service.go` and have their own guard slot.
3. Resolve current bucket via `WorkflowRepository.CurrentTaskBucket`.
4. Resolve target bucket via the captured per-project Snapshot (`s.snap.BucketByKey`) — workflow shape lives in memory post-020, no repository round-trip.
5. If `current != target`:
   1. **Transition allowed?** → `Snapshot.TransitionAllowed`. Fails with `workflow_invalid_transition`.
   2. **Guards** → `guards.Evaluator.EvaluateTransition` (this doc; implementation in `internal/app/guards/evaluator.go`). First failure returns `guard_violation` and the move never persists.
6. Persist via `TaskRepository.MoveTask` (records `task.moved`).
7. If the destination is the workflow's final bucket, additionally emit `task.completed`.

Self-moves (current == target) skip both the transition check and guard evaluation. Same-bucket "moves" are no-ops by design.

## Guard types

Five types are supported. Anything else is rejected at validation time with `unknown guard type`.

### `blockers_in`

Asserts that every task this task depends on currently sits in one of `buckets`. Useful for "you cannot leave Backlog while any blocker is still in Backlog".

```yaml
- type: blockers_in
  buckets: [done]                # required, non-empty; each key must exist in the same workflow
  hint: "Move blockers to Done first."   # optional, surfaced verbatim in the error
```

**Evaluation** (`internal/app/guards/evaluator.go:checkBlockersIn`):

- Loads dependency rows via `GuardEvaluationRepository.ListTaskBlockerBuckets` (`internal/sqlite/guards.go`).
- Builds the allowed-key set from `buckets`.
- Collects every blocker whose current bucket is **not** in that set.
- If any are pending, returns `guard_violation` with details:
  ```json
  {
    "code": "guard_violation",
    "msg": "blockers_in guard: pending blockers: #12 \"refactor sqlite store\" (in \"dev\")",
    "details": { "pending_blockers": ["#12 \"…\" (in \"dev\")"], "hint": "…" }
  }
  ```

Notes:

- `buckets` is required and non-empty (validator rejects an empty list).
- Every key in `buckets` must be a bucket in the same workflow (`workflows.<key>.buckets[].key`).
- A task with no dependencies always passes.

### `comments_min`

Asserts that the task has at least `count` comments — any author, any tag.

```yaml
- type: comments_min
  count: 1                       # required, must be >= 1
  hint: "Leave a status note before moving forward."  # optional
```

**Evaluation** (`internal/app/guards/evaluator.go:checkCommentsMin`):

- Loads the count via `GuardEvaluationRepository.CountTaskComments` (`internal/sqlite/guards.go`).
- Passes when `count(comments) >= count`.
- Otherwise returns `guard_violation` with `details.count` (current) and `details.required`.

### `comments_tagged`

Asserts that the task has at least `count` comments carrying a specific tag. The repository call uses `DISTINCT comment.id`, so a single comment with the tag attached multiple times still counts as one.

```yaml
- type: comments_tagged
  tag: resume                    # required; tag name must be non-empty
  count: 1                       # required, must be >= 1
  hint: "Add a #resume note summarizing what was implemented."  # optional
```

**Evaluation** (`internal/app/guards/evaluator.go:checkCommentsTagged`):

- Loads the tagged-count via `GuardEvaluationRepository.CountTaskCommentsTagged` (`internal/sqlite/guards.go`).
- Passes when `count(distinct comments tagged tag) >= count`.
- Defaults `count` to 1 at runtime if a stored guard somehow has `count < 1` (validation already prevents this on configured guards).
- Returns `details.tag`, `details.count`, `details.required` on failure.

Tag matching is exact-name and case-sensitive — names are kebab-cased on insert (`internal/app/tag_normalization.go:NormalizeTagName`), so always reference them in kebab-case from the YAML.

### `wave_gate`

Asserts that every task in earlier waves of the same plan currently sits in the workflow's final bucket (terminal). Lets a workflow gate a wave so its tasks cannot enter `dev` while a prior wave is still in flight.

```yaml
- type: wave_gate
  hint: "Wait for the previous wave to finish before starting this one."  # optional
```

No fields beyond `type` and `hint`. The wave order is read from `plan_waves.position`; the pending count is derived per-task by joining the task's `wave_id` against earlier-positioned siblings in the same plan.

**Evaluation** (`internal/app/guards/evaluator.go:checkWaveGate`):

- Resolves the task's plan/wave via `GuardEvaluationRepository.CountPriorWavesPending`.
- Returns `0` (no-op pass) when the task has no `wave_id` — the guard is safe to wire into existing presets without affecting legacy tasks.
- Otherwise counts tasks in waves with `position < currentWave.position` whose `bucket_id` is not the workflow's final bucket.
- Fails with `guard_violation`, `rule="wave_gate"`, and `details.pending = N` when any prior-wave task is still in flight.

The wave-gate edges themselves are not rendered in the network diagram (`internal/tui/render_plan_network.go`) — the guard is invisible by design so the diagram stays focused on intra/cross-wave dependency arrows.

### `subtasks_complete`

Asserts that every direct child task (`tasks.parent_id = current task id`) currently sits in the workflow's final bucket. This lets a preset block promotion of a parent while any child remains open.

```yaml
- type: subtasks_complete
  hint: "Finish all child tasks before promoting the parent."  # optional
```

No fields beyond `type` and `hint` are consumed. The final bucket is resolved from the active workflow snapshot, so renaming `done` still works as long as the workflow's last-position bucket represents completion.

**Evaluation** (`internal/app/guards/evaluator.go:checkSubtasksComplete`):

- Resolves the workflow's final bucket via `Snapshot.Workflow().FinalBucketKey()`.
- Loads the first direct child not in that bucket via `GuardEvaluationRepository.FirstChildNotInBucket`.
- Passes when no direct child is open. Grandchildren are handled by their own parent when that parent is promoted.
- Fails with `guard_violation`, `rule="subtasks_complete"`, and details naming the open child id/title/bucket plus the final bucket.

## Multiple guards on the same transition

Guards are stored and evaluated in declaration order. The **first** violation short-circuits and is the one returned to the user; later guards do not run. Pick the order that gives the most actionable error first.

```yaml
- from: 2
  to: 3
  guards:
    - type: blockers_in
      buckets: [done]
    - type: comments_tagged
      tag: resume
      count: 1
```

If you want both checks to be visible at once, split them across two stricter transitions instead — Omakiten does not aggregate guard failures.

## The `hint` field

`hint` is free-form text. When a guard fails, the hint is appended to the error message after `". Hint: "` and also returned in `details.hint`. It is the canonical place to tell the operator/agent how to remediate — e.g. which tag to apply, where to add the comment, which task to move first.

Empty/missing `hint` is fine; the error stays terse.

### Hint resolution: `${{intl:KEY}}` tokens

Hints support `${{intl:KEY}}` substitution so presets can keep copy in the i18n catalog instead of inlining language per guard. Expansion runs in `internal/app/guards/evaluator.go:212-217` (`resolveHint`) via `Snapshot.ResolveGuardHint` → `Catalog(SurfaceCLI).Resolve` (`internal/config/catalog.go:88`). Lookup hits the active catalog first, then baseline (`internal/config/catalog.go:58`); a missing key returns the key literal verbatim and logs at debug level — guard evaluation never fails on a missing token. Resolution is single-pass (catalog values are not re-scanned), unknown namespaces stay verbatim, and `$${{intl:KEY}}` escapes to a literal `${{intl:KEY}}`. Empty `hint` short-circuits before any catalog call.

## Validation rules (parse-time)

`internal/config/validator.go:validateWorkflows` enforces, per transition, before the bundle is imported:

| Rule | Error message shape |
|---|---|
| Unknown guard `type` | `workflows.<wf>: unknown guard type "<type>"` |
| `blockers_in.buckets` is empty | `workflows.<wf> guard blockers_in: buckets is required` |
| `blockers_in.buckets` contains a key not in this workflow | `workflows.<wf> guard blockers_in: bucket key "<k>" not found in workflow` |
| `comments_min.count < 1` | `workflows.<wf> guard comments_min: count must be >= 1` |
| `comments_tagged.tag` empty | `workflows.<wf> guard comments_tagged: tag is required` |
| `comments_tagged.count < 1` | `workflows.<wf> guard comments_tagged: count must be >= 1` |
| `wave_gate` / `subtasks_complete` | no required fields beyond `type`; `hint` is optional and runtime state is derived |

A failed validation rejects `okt config validate` and any bundle import that would re-materialize the SQLite read model.

## Worked example (from `defaults/config/omakase.yaml`)

```yaml
workflows:
  - id: 1
    key: omakase
    name: Omakase Workflow
    defaults:                        # workflow-level CRUD fallback
      task:    { edit: false, delete: false }
      comment: { edit: false, delete: false }
    buckets:
      - id: 1
        key: backlog
        name: Backlog
        position: 1
        permissions:                 # backlog opts in to full edit/delete
          task:    { edit: true, delete: true }
          comment: { edit: true, delete: true }
      - id: 2
        key: dev
        name: Development
        position: 2
        permissions:                 # dev: comments only
          comment: { edit: true, delete: true }
      - id: 3
        key: review
        name: Review
        position: 3
        permissions:                 # review: comment edit only (no delete)
          comment: { edit: true }
      - id: 4
        key: done
        name: Done
        position: 4
        permissions:                 # done freezes deletion explicitly
          task:    { delete: false }
          comment: { delete: false }
    transitions:
      - from: 1
        to: 2
        guards:
          - type: comments_tagged
            tag: self-branch
            count: 1
            hint: "Before starting, create a dedicated feature branch or git worktree…"
          - type: blockers_in
            buckets: [done]
            hint: "Move blockers to Done first…"
          - type: wave_gate
            hint: "Wait for the previous wave to finish…"
      - from: 2
        to: 3
        guards:
          - type: comments_tagged
            tag: resume
            count: 1
            hint: "Add a comment tagged #resume summarizing what was implemented…"
          - type: comments_tagged
            tag: tests-passing
            count: 1
            hint: "Attach passing test evidence…"
          - type: subtasks_complete
            hint: "Finish every direct child task before review…"
      - from: 3
        to: 4
        guards:
          - type: comments_tagged
            tag: documentation
            count: 1
            hint: "Add a comment tagged #documentation summarizing: commits merged…"
      # Regression transitions — no guards; reopening is a deliberate corrective action.
      - { from: 2, to: 1 }    # dev    → backlog
      - { from: 3, to: 1 }    # review → backlog
      - { from: 3, to: 2 }    # review → dev
      - { from: 4, to: 3 }    # done   → review
      - { from: 4, to: 2 }    # done   → dev
      - { from: 4, to: 1 }    # done   → backlog
```

The pattern: forward transitions carry an evidence-gathering guard (a tag-anchored comment); regression transitions are intentionally guard-free so reviewers can return work without ceremony. Bucket permissions tighten as the task advances — backlog is freely reshaped, dev allows comment maintenance, review only allows comment edits to refine the handoff, done blocks destructive operations completely.

## Failure shape

All guard violations use the `guard_violation` coded error (`internal/domain/errors.go:23`). Consumers:

- **CLI**: returns the failure JSON envelope (`internal/output/json.go`) with `code: "guard_violation"` and exit code `1`.
- **MCP / agent**: `internal/agent/errors.go` wraps it with next-step guidance via `guidanceForCode`.
- **TUI**: surfaces the message + hint inline in the move flow (`internal/tui/render_task.go`).

Because the move never persists when a guard fails, no `task.moved` event is recorded. The current bucket is preserved.

Every task-scoped violation also records a `guard.violated` audit event whose payload carries the same subject metadata block every other task event ships:

```json
{
  "operation":         "task.transition",   // or task.archive / task.delete / task.edit / etc.
  "rule":              "subtasks_complete", // guard.Type or "permissions" / "transition_not_allowed"
  "hint":              "subtasks_complete guard: subtask #42 ...",
  "target":            { "task_id": 7, "from_bucket_id": 2, "to_bucket_id": 3, "to_bucket": "review" },
  "attempted_by":      "agent",             // "agent" for MCP source, "user" otherwise
  "subject_task_id":   7,
  "subject_parent_id": 42,                  // null for root tasks
  "subject_depth":     1,                   // 0 for root, 1+ for sub-tasks
  "resolved_kit":      "izakaya"            // sub-kit identity for sub-task subjects; root kit otherwise
}
```

The subject block is the same shape `task.created` / `task.moved` / `task.edited` carry — see [`subtask-kit.md § Hook subject metadata`](subtask-kit.md#hook-subject-metadata). Hooks scoped to `SubjectDepth: subtask` therefore match a sub-task guard violation; hooks scoped to `SubjectDepth: root` only match a root-task violation. The depth filter activates as soon as `subtask_kit:` is wired; before that every hook fires regardless of depth (matches pre-cascade behavior).

## Operation guards

The same three guard shapes (`blockers_in`, `comments_min`, `comments_tagged`) also gate **non-flow operations**: archive, delete, unarchive. They live under `workflows[].operations.<op>.guards[]` and apply globally to the operation regardless of which bucket the task currently sits in.

```yaml
workflows:
  - id: 1
    key: omakase
    operations:
      archive:
        guards:
          - { type: comments_tagged, tag: documentation, count: 1 }
      delete:
        guards:
          - { type: comments_tagged, tag: peer-review, count: 1, hint: "Get a peer-review tagged comment before deleting." }
      unarchive: {}                  # no guards
```

Evaluation entry point lives in `internal/app/task_service.go` (`Archive`, `Delete`, `Unarchive`) — each operation calls `s.workflow.Evaluator().EvaluateOperation(ctx, projectID, taskID, OperationKey, op.Guards)` on the per-project `Evaluator`. The evaluator (`internal/app/guards/evaluator.go::EvaluateOperation`) walks the guards in declaration order and applies the same first-fail short-circuit as transitions.

**Important policy notes:**

- `Archive` flips `state` to `archived` and atomically moves the task into the workflow's **final bucket** (highest `position`). It bypasses bucket `permissions` and transition `guards` — only `operations.archive.guards` apply. Use it to ship rather than to delete.
- `Delete` is a hard cascade delete (comments, tags, dependencies, events). Bucket `permissions.task.delete` AND `operations.delete.guards` both apply.
- `Unarchive` flips `state` back to `active` while leaving the bucket untouched. Only `operations.unarchive.guards` apply.

## Permission policy failures

Task/comment CRUD policy is workflow schema, not a guard type. Configure it under `workflows[].defaults` and `workflows[].buckets[].permissions`; the canonical field reference and resolution order live in [workflows.md](workflows.md#workflow-defaults) and [workflows.md § Comment permissions](workflows.md#comment-permissions).

When a permission denies an operation, the app returns the same error class as transition guards: `guard_violation` with `rule: permissions`. The operation (`task.edit`, `task.delete`, `comment.create`, `comment.edit`, or `comment.delete`) lands in the event payload so consumers can filter on `(operation, rule)`.

Comment operation values can be a bare bool or a rule object. A bare bool `b` is equivalent to `{ allow: b }`. The rule object carries a base verdict plus tag predicates (`internal/domain/comment_policy.go`, `CommentOpPolicy`):

```yaml
comment:
  edit:
    allow: true
    require_tags: [reviewed]
    deny_tags: [locked]
    require_any_tag: true
```

The decoder accepts only `{allow, require_tags, deny_tags, require_any_tag}`. Unknown keys are rejected at load with `unknown comment permission rule key`, and empty tag names are rejected by `validateCommentTagNames`.

`CommentOpPolicy.Evaluate` runs in this order: base `allow` first; `false` short-circuits. If the base allows, predicates apply as `require_any_tag` -> `require_tags` -> `deny_tags`. For `create`, the evaluated tags are the request payload tags; for `edit` and `delete`, they are the target comment's stored tags.

Failure hints name the resolved policy or predicate (`internal/app/workflow_service.go`):

```text
policy: comment.create is not permitted in bucket "done". Move the task to one of: backlog, dev - then retry.
policy: comment.create is not permitted in bucket "done" (no bucket allows it; declare workflows[].buckets[].permissions.comment.create)
policy: task comment edit is denied for tag "locked"
policy: project comment create requires tag "x"
policy: universal comment create requires at least one tag
```

## Adding a new guard type

1. Add the case to the `runGuards` switch in `internal/app/guards/evaluator.go`.
2. Add the corresponding count/list method to `app.GuardEvaluationRepository` (`internal/app/ports.go`) and implement it in `internal/sqlite/guards.go`.
3. Extend `validateWorkflows` (`internal/config/validator.go`) so unknown payloads are rejected at validation time.
4. Add tests in `internal/app/workflow_service_test.go` covering pass, fail, and hint passthrough.
5. Document the new type here.

## Update when

- A new guard type lands in `internal/app/guards/evaluator.go` — add it to [Guard types](#guard-types) with its YAML shape and failure mode.
- The comment-permission failure shape, `CommentOpPolicy` rule shape, or its per-operation tag source changes (`internal/domain/comment_policy.go`, `internal/domain/workflow.go`, `internal/config/bundle.go`) — update [Permission policy failures](#permission-policy-failures).
- Validator rules change in `internal/config/validator.go::validateWorkflows`.
- `app.WorkflowService.MoveTask` pipeline reorders or adds a step.
- `guard.violated` event payload gains/drops a field (source: `internal/domain/event.go`).

## See also

- [workflow.md](../workflow.md) — guards per preset; preset-level conceptual flow.
- [workflows.md](workflows.md) — full schema for `workflows[]`, including the guard slot wiring.
- `internal/domain/event.go::KnownEventTypes` — source-of-truth for the `guard.violated` payload.
