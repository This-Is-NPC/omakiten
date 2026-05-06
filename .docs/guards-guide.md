# Workflow Guards

Guards are policy rules attached to a single workflow **transition**. They run in `app.WorkflowService.MoveTask` after the transition is confirmed allowed (`workflow_invalid_transition` is checked first); the first failing guard short-circuits the move with a coded `guard_violation` error.

Guards live next to transitions in `omakiten.yaml`, are persisted as JSON on the `workflow_transitions` row (`migrations/005_transition_guards.sql`), and are evaluated by `internal/app/workflow_service.go:evaluateGuards`. Validation runs at `okt config validate` time via `internal/config/validator.go:validateWorkflows`.

## Where they sit in the move pipeline

`app.WorkflowService.MoveTask` runs in this order (`internal/app/workflow_service.go:76`):

1. Validate input (`task_id > 0`, target bucket non-empty).
2. Resolve current bucket via `WorkflowRepository.CurrentTaskBucket`.
3. Resolve target bucket via `WorkflowRepository.ResolveActiveBucket`.
4. If `current != target`:
   1. **Transition allowed?** → `WorkflowRepository.TransitionAllowed`. Fails with `workflow_invalid_transition`.
   2. **Guards** → `evaluateGuards` (this doc). First failure returns `guard_violation` and the move never persists.
5. Persist via `TaskRepository.MoveTask` (records `task.moved`).
6. If the destination is the workflow's final bucket, additionally emit `task.completed`.

Self-moves (current == target) skip both the transition check and guard evaluation. Same-bucket "moves" are no-ops by design.

## Guard types

Three types are supported. Anything else is rejected at validation time with `unknown guard type`.

### `blockers_in`

Asserts that every task this task depends on currently sits in one of `buckets`. Useful for "you cannot leave Backlog while any blocker is still in Backlog".

```yaml
- type: blockers_in
  buckets: [done]                # required, non-empty; each key must exist in the same workflow
  hint: "Move blockers to Done first."   # optional, surfaced verbatim in the error
```

**Evaluation** (`workflow_service.go:checkBlockersIn`):

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

**Evaluation** (`workflow_service.go:checkCommentsMin`):

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

**Evaluation** (`workflow_service.go:checkCommentsTagged`):

- Loads the tagged-count via `GuardEvaluationRepository.CountTaskCommentsTagged` (`internal/sqlite/guards.go`).
- Passes when `count(distinct comments tagged tag) >= count`.
- Defaults `count` to 1 at runtime if a stored guard somehow has `count < 1` (validation already prevents this on configured guards).
- Returns `details.tag`, `details.count`, `details.required` on failure.

Tag matching is exact-name and case-sensitive — names are kebab-cased on insert (`internal/app/tag_normalization.go:NormalizeTagName`), so always reference them in kebab-case from the YAML.

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

A failed validation rejects `okt config validate` and any bundle import that would re-materialize the SQLite read model.

## Worked example (from `defaults/omakiten.yaml`)

```yaml
workflows:
  - id: 1
    key: default
    name: Default Workflow
    buckets:
      - { id: 1, key: backlog, name: Backlog,     position: 1 }
      - { id: 2, key: dev,     name: Development, position: 2 }
      - { id: 3, key: review,  name: Review,      position: 3 }
      - { id: 4, key: done,    name: Done,        position: 4 }
    transitions:
      - from: 1
        to: 2
        guards:
          - type: comments_tagged
            tag: self-branch
            count: 1
            hint: "Before starting, create a dedicated feature branch or git worktree…"
      - from: 2
        to: 3
        guards:
          - type: comments_tagged
            tag: resume
            count: 1
            hint: "Add a comment tagged #resume summarizing what was implemented…"
      - from: 3
        to: 4
        guards:
          - type: comments_tagged
            tag: documentation
            count: 1
            hint: "Add a comment tagged #documentation summarizing: commits merged…"
      - from: 3
        to: 2          # review → dev: no guards (kickback path)
      - from: 4
        to: 3          # done   → review: no guards (re-open path)
```

The pattern: forward transitions carry an evidence-gathering guard (a tag-anchored comment); backward kickback transitions are intentionally guard-free so reviewers can return work without ceremony.

## Failure shape

All guard violations use the `guard_violation` coded error (`internal/domain/errors.go:23`). Consumers:

- **CLI**: returns the failure JSON envelope (`internal/output/json.go`) with `code: "guard_violation"` and exit code `1`.
- **MCP / agent**: `internal/agent/errors.go` wraps it with next-step guidance via `guidanceForCode`.
- **TUI**: surfaces the message + hint inline in the move flow (`internal/tui/render_task.go`).

Because the move never persists when a guard fails, no `task.moved` event is recorded. The current bucket is preserved.

## Adding a new guard type

1. Add the case to the `evaluateGuards` switch in `internal/app/workflow_service.go`.
2. Add the corresponding count/list method to `app.GuardEvaluationRepository` (`internal/app/ports.go`) and implement it in `internal/sqlite/guards.go`.
3. Extend `validateWorkflows` (`internal/config/validator.go`) so unknown payloads are rejected at validation time.
4. Add tests in `internal/app/workflow_service_test.go` covering pass, fail, and hint passthrough.
5. Document the new type here.
