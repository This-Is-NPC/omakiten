# Workflow Guards

Guards are policy rules attached to a single workflow **transition**. They run in `app.WorkflowService.MoveTask` after the transition is confirmed allowed (`workflow_invalid_transition` is checked first); the first failing guard short-circuits the move with a coded `guard_violation` error.

Guards live next to transitions in the active profile yaml, are persisted as JSON on the `workflow_transitions` row (`migrations/005_transition_guards.sql`), and are evaluated by `internal/app/workflow_service.go:evaluateGuards`. Validation runs at `okt config validate` time via `internal/config/validator.go:validateWorkflows`.

The same guard shapes also drive **operation policies** (`operations.{archive,delete,unarchive}.guards`) — see [Operation guards](#operation-guards) — and the bucket-level CRUD policy lives under a sibling block ([Bucket permissions](#bucket-permissions)).

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

## Agent guardrails: laws bound to commands and entities

Guards above run on **workflow transitions**. A second class of guardrails runs on **MCP prompt resolution** — when the agent asks for `okt-implement`, the server resolves the bound persona, skills, laws, and templates and ships them in a single PromptMessage. This is where the `template-fidelity` law lives, for example: bound globally to every command so the agent does not invent fields when filling a PR template.

Resolution happens in `agent.Service.ResolveCommand` (`internal/agent/service_command.go`). The MCP adapter (`internal/mcp/adapter.go`) calls it from `prompts/get`.

### What gets composed

For each `okt-*` prompt, the resolver assembles:

| Slot | Source |
|---|---|
| Persona | `mcp_commands.<name>.persona` slug |
| Skills | The persona's `skills:` (wiring) — bodies pulled from `skills/<slug>.md` |
| Laws | Union of `mcp_commands.global.laws` ∪ `personas.<slug>.laws` ∪ `mcp_commands.<name>.laws` ∪ `templates.<bound>.laws`, deduped, **minus** `mcp_commands.<name>.laws_disabled` |
| Templates | `mcp_commands.<name>.templates` slugs — bodies pulled from `templates/<slug>.md` |
| Action | The canonical instruction text for the prompt name |

The merged response is rendered as one markdown body (Persona → Skills → Laws → Templates → Action) so the agent can scan it top-down without paying for multiple prompt messages.

### Wiring location

```yaml
# active profile yaml
mcp_commands:
  global:
    laws:
      - template-fidelity        # applies to every okt-* command
  okt-create:
    persona: engineer
    templates: [user-story]
  okt-implement:
    persona: engineer
    templates: [pull-request]
  okt-imagine:
    persona: engineer
    laws_disabled:               # opt out of the global law for discovery
      - template-fidelity
```

Per-entity bindings travel with the file:

```yaml
# templates/pull-request.md
---
name: Pull Request
default: pr
laws:
  - template-fidelity
---
```

```yaml
# personas/engineer.md
---
name: Backend Agent
laws:
  - project-scope-only
---
```

Persona laws declared in frontmatter merge with persona laws declared in the active profile yaml's `personas:` block (union, dedup, frontmatter-first). Template frontmatter laws have no wiring counterpart — they live only in the `.md` file.

### Validation rules (parse-time)

| Rule | Error message shape |
|---|---|
| Persona slug on a command does not resolve | `mcp_commands.<name> persona: ref "<slug>" has no matching persona file` |
| Law slug under `laws:` or `laws_disabled:` does not resolve | `mcp_commands.<name>.laws: ref "<slug>" has no matching law file` |
| Same slug appears in both `laws:` and `laws_disabled:` on a command | `mcp_commands.<name>: law "<slug>" is in both laws and laws_disabled` |
| Template slug on a command does not resolve | `mcp_commands.<name>.templates: ref "<slug>" has no matching template file` |
| Duplicate slug in any list | `mcp_commands.<name>.<list>: duplicate "<slug>"` |
| Template frontmatter law does not resolve | `templates.<slug> laws: ref "<slug>" has no matching law file` |

The reserved `global` key is treated as a laws-only slot — its persona and templates fields are tolerated but unused.

### The `template-fidelity` default law

`defaults/laws/template-fidelity.md` ships in the bundled kit and is bound to `mcp_commands.global.laws` by default. Its body forbids the agent from inventing fields, links, or claims not present in the template body or the working context — directly addresses the original symptom (agent appending `Closes #40` that the template did not declare). Severity is `warning` because the law is guidance, not server-enforced rejection: the MCP layer ships it inline with the prompt, and an opt-out command (`okt-imagine`) is provided for discovery flows where free sketching is intentional.

### Auto-applying templates on `tasks.create` / `comments.add`

Both tools accept an optional `template_slug` argument. When set, the server resolves the slug against the loaded template catalog and merges the body into the description/comment:

- empty user body ⇒ description = template body verbatim;
- non-empty user body ⇒ user content first, blank line, template body appended.

Unknown slugs surface as a validation error (no silent fallback). Dynamic placeholders are out of scope for this iteration — the materialized body is the literal template text.

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

Evaluation lives in `internal/app/task_service.go` (`Archive`, `Delete`, `Unarchive`) — each operation pulls its policy from `domain.Workflow.Operations` and runs `evaluateGuards` with the same first-fail short-circuit.

**Important policy notes:**

- `Archive` flips `state` to `archived` and atomically moves the task into the workflow's **final bucket** (highest `position`). It bypasses bucket `permissions` and transition `guards` — only `operations.archive.guards` apply. Use it to ship rather than to delete.
- `Delete` is a hard cascade delete (comments, tags, dependencies, events). Bucket `permissions.task.delete` AND `operations.delete.guards` both apply.
- `Unarchive` flips `state` back to `active` while leaving the bucket untouched. Only `operations.unarchive.guards` apply.

## Bucket permissions

Per-bucket CRUD policy lives under `workflows[].buckets[].permissions` with a workflow-level fallback in `workflows[].defaults`. Resolution walks:

1. `bucket.permissions.<task|comment>.<edit|delete>` — per-bucket override.
2. `workflows[].defaults.<task|comment>.<edit|delete>` — workflow-level fallback.
3. Implicit `true` — no rule declared anywhere = allowed.

`comment` inherits from `task` field-by-field at every layer: declaring `task.edit: false` denies edit on **both** task and comments unless `comment.edit` is set explicitly at the same or a deeper layer.

```yaml
defaults:
  task:    { edit: false, delete: false }    # workflow-level: deny by default
  comment: { edit: false, delete: false }
buckets:
  - id: 1
    key: backlog
    permissions:
      task:    { edit: true, delete: true }   # backlog: opt in
      comment: { edit: true, delete: true }
  - id: 3
    key: review
    permissions:
      comment: { edit: true }                 # review: only comment edit (delete inherits false)
```

Violations surface as `guard_violation` with `rule: permissions` and a hint quoting the resolved policy. The active operation (`task.edit`, `task.delete`, `comment.edit`, `comment.delete`) lands in the event payload — consumers filter on `(operation, rule)` to distinguish a transition denial from a permission denial.

## Adding a new guard type

1. Add the case to the `evaluateGuards` switch in `internal/app/workflow_service.go`.
2. Add the corresponding count/list method to `app.GuardEvaluationRepository` (`internal/app/ports.go`) and implement it in `internal/sqlite/guards.go`.
3. Extend `validateWorkflows` (`internal/config/validator.go`) so unknown payloads are rejected at validation time.
4. Add tests in `internal/app/workflow_service_test.go` covering pass, fail, and hint passthrough.
5. Document the new type here.
