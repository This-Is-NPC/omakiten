# Workflow configuration

`workflows:` defines the state machine Omakiten enforces for tasks: buckets, allowed transitions, CRUD permissions, and operation guards. This page owns the YAML shape. Guard payload semantics live in [guards.md](guards.md).

Source files:

- `internal/config/bundle.go` (`Workflow`, `Bucket`, permissions, operations).
- `internal/config/validator.go::validateWorkflows`.
- `internal/domain/workflow.go` (permission resolution and final-bucket helpers).

## Contents

- [`workflows`](#workflows)
- [Workflow defaults](#workflow-defaults)
- [Buckets](#buckets)
- [Transitions](#transitions)
- [Operations](#operations)
- [Comment permissions](#comment-permissions)
- [Importing workflow sections](#importing-workflow-sections)
- [Update when](#update-when)

## `workflows`

```yaml
workflows:
  - id: 1
    key: omakase
    name: Omakase Workflow
    defaults: { ... }
    operations: { ... }
    buckets: [ ... ]
    transitions: [ ... ]
```

| Field | Type | Notes |
|---|---|---|
| `id` | int `> 0`, unique | Workflow identity inside the profile. |
| `key` | string, unique | Referenced by `config.workflow.active`. |
| `name` | string | Human label in CLI/TUI. |
| `defaults` | object, optional | Workflow-level task/comment permission fallback. |
| `operations` | object, optional | Guards for `archive`, `delete`, and `unarchive`. |
| `buckets` | non-empty list | Ordered workflow states. |
| `transitions` | list | Allowed bucket moves. Empty list means no moves. |

`tasks.bucket_id` stores the YAML bucket id, not a SQL FK. Once tasks exist, prefer renaming `key`/`name` over changing bucket ids.

## Workflow defaults

`defaults` is the workflow-level fallback for CRUD policy when a bucket omits a value.

```yaml
workflows:
  - key: omakase
    defaults:
      task:
        edit: false
        delete: false
      comment:
        edit: false
        delete: false
```

Task permissions support `edit` and `delete`. Task-scoped comment permissions support `create`, `edit`, and `delete`; comment `edit`/`delete` inherit from task rules when the comment rule is omitted.

Resolution order for task `edit`/`delete`:

1. `bucket.permissions.task.<op>`.
2. `workflow.defaults.task.<op>`.
3. implicit `true`.

Resolution order for task-scoped comment `edit`/`delete`:

1. `bucket.permissions.comment.<op>`.
2. `bucket.permissions.task.<op>`.
3. `workflow.defaults.comment.task.<op>` or flat `workflow.defaults.comment.<op>`.
4. `workflow.defaults.task.<op>`.
5. implicit `true`.

Pointer booleans distinguish omitted from explicit `false`: omitted falls through, explicit `false` stops the chain.

## Buckets

```yaml
buckets:
  - id: 1
    key: backlog
    name: Backlog
    position: 1
    permissions:
      task:    { edit: true, delete: true }
      comment: { create: true, edit: true, delete: true }
```

| Field | Type | Notes |
|---|---|---|
| `id` | int `> 0`, unique within workflow | Referenced by `transitions[].from` / `to` and stored on tasks. |
| `key` | string, unique within workflow | Stable bucket handle used by CLI/MCP/TUI. |
| `name` | string | Human label. |
| `position` | int `>= 1` | Visual order. Lowest position is the default bucket for new tasks; highest position is the final bucket. |
| `permissions` | object, optional | Per-bucket task/comment CRUD overrides. |

## Transitions

```yaml
transitions:
  - from: 1
    to: 2
    guards:
      - type: comments_tagged
        tag: self-branch
        count: 1
  - { from: 2, to: 1 }
```

| Field | Type | Notes |
|---|---|---|
| `from` | bucket id | Must reference a bucket in this workflow. |
| `to` | bucket id | Must reference a bucket in this workflow. |
| `guards` | list, optional | Transition guards. Payloads are documented in [guards.md](guards.md#guard-types). |

Each `(from, to)` pair must be unique. Same-bucket moves are no-ops and do not need transitions.

## Operations

`operations` gates lifecycle actions that are not workflow transitions.

```yaml
operations:
  archive:
    guards:
      - { type: comments_tagged, tag: documentation, count: 1 }
  delete:
    guards:
      - { type: comments_tagged, tag: peer-review, count: 1 }
  unarchive:
    guards: []
```

| Operation | Behavior |
|---|---|
| `archive` | Sets `state=archived` and moves the task to the final bucket. Bypasses bucket permissions and transition guards; operation guards still apply. |
| `delete` | Hard-deletes task data. Bucket `permissions.task.delete` and `operations.delete.guards` both apply. |
| `unarchive` | Restores `state=active` while leaving the bucket unchanged. Operation guards apply when declared. |

Operation guards reuse the same guard payloads as transitions; see [guards.md](guards.md).

## Comment permissions

Project and universal comments have no task bucket. Their policies resolve from workflow defaults:

```yaml
defaults:
  comment:
    task:      { create: true,  edit: false, delete: false }
    project:   { create: true,  edit: true,  delete: false }
    universal: { create: false, edit: true,  delete: false }
```

The flat shape `comment: {edit, delete}` is equivalent to the task scope for backward compatibility. The `task` / `project` / `universal` sub-blocks are valid only under `workflows[].defaults.comment`.

Comment operation values may be a bare bool or a rule object:

```yaml
comment:
  create:
    allow: true
    require_tags: [reviewed]
    deny_tags: [locked]
    require_any_tag: false
```

Rule-object evaluation and failure hints are documented in [guards.md § Permission policy failures](guards.md#permission-policy-failures).

## Importing workflow sections

Workflows can be split from the active profile with a value-level import:

```yaml
workflows:
  from: ./packs/workflows/omakase-workflow.yaml
```

The imported document replaces the `workflows:` value wholesale. Full import rules live in [path-resolution.md § Modular config imports](path-resolution.md#modular-imports).

## Update when

- A workflow, bucket, transition, operation, or permission field changes.
- Permission resolution order changes in `internal/domain/workflow.go`.
- A lifecycle operation gains or loses workflow-level policy.
- `validateWorkflows` changes validation rules that affect this schema.
