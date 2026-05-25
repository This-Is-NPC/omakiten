# Surface Policy

Omakiten exposes three primary surfaces — CLI (`okt`), TUI (`okt tui`), and MCP (`okt mcp …`). Most operations land on all three so agents, scripts, and humans share one implementation. A small set of operations live deliberately on only one or two surfaces. This page documents the criteria, the current exceptions, and the convention for new operations.

## Criteria

An operation is restricted from MCP when **any** of the following holds:

- **Destructive and irreversible** — the operation removes data that cannot be reconstructed from the bundle or surrounding events (e.g. removing a project removes its tasks, comments, plans, and errors; no replay can rebuild that history).
- **Requires human-in-the-loop confirmation** — the blast radius is large enough that an automated agent should not be authorised to fire it without a deliberate human step. Confirmations are surfaced as a tty prompt (CLI) or an arm-then-confirm gate (TUI).
- **Sysadmin operation** — the action belongs to the runtime/install layer, not the project layer. It does not produce a domain event the agent would consume; it produces a recovery artefact the operator manages out-of-band.

Operations that fail none of those checks default to the standard CLI + TUI + MCP triplet.

## Current restrictions

| Operation       | TUI | CLI | MCP | Reason                                                          |
| --------------- | :-: | :-: | :-: | --------------------------------------------------------------- |
| `projects.delete` | ✓ | ✓ | ✗ | Irreversible cascade across tasks/plans/errors/activity log; human-in-the-loop required. |
| `db.backup`       | ✗ | ✓ | ✗ | Sysadmin op (writes a `.db` snapshot to `$XDG_STATE_HOME/omakiten/backups/`); no agent workflow consumes the artefact directly. |
| `update`          | ✗ | ✓ | ✗ | Sysadmin op (swaps the on-disk binary); auto-runs `db.backup` before the swap. |
| `uninstall`       | ✗ | ✓ | ✗ | Sysadmin op (removes the binary + optional purge of data/config); irreversible. |
| `setup`           | ✗ | ✓ | ✗ | Bootstrap op (writes user-global default files + shell-rc wrapper); no agent workflow drives a re-setup. |

`✓` = surface owns this operation. `✗` = explicitly out of scope for this surface.

`tasks.delete` and `comments.delete` are exposed on all three surfaces (adapter.go registers them as MCP tools) but are not unconditional: each call passes through bucket-level permissions and operation guards before any rows move.

## Bucket permissions and operation guards

Surface availability is only the outer gate. Mutating tools also flow through two YAML-declared checks evaluated by `internal/app/guards/evaluator.go`:

- **Bucket permissions** — `permissions.task.edit`, `permissions.task.delete`, `permissions.comment.edit`, and `permissions.comment.delete` resolve against the task's current bucket (falling through to `workflow.defaults`, then to an implicit `true`). The default kit allows `tasks.edit` only in the planning bucket; deletes inherit the same shape unless declared separately.
- **Operation guards** — `operations.delete.guards`, `operations.archive.guards`, and `operations.unarchive.guards` run guard specs (`blockers_in`, `comments_min`, `comments_tagged`, `wave_gate`, `subtasks_complete`) before the operation commits. Archive and unarchive bypass bucket permissions and transition guards but still honour these operation guards; deletes pay both tolls. Violations emit a `guard.violated` event tagged `attempted_by=agent` for MCP traffic.

## Backup safety net

Every operation tagged "irreversible" in the table above runs `app.BackupService` before mutating state when the flow owns project data (`projects.delete`; `update` runs the same snapshot routine before swapping the binary). `uninstall` is intentionally opt-in for backups because it removes user-owned state by request; run `okt db backup` first when you want a retained snapshot. The snapshot lands under `$XDG_STATE_HOME/omakiten/backups/<utc-iso>.db` with the retention count from `config.backup.retention_count` (default 5). Backup failure aborts the destructive flow before any rows or files are touched.

## Convention for new operations

When proposing a new operation, ask:

1. Is the change reversible from the bundle + events log? → MCP + CLI + TUI.
2. Is it a sysadmin/install operation? → CLI-only.
3. Is it irreversible with cross-project blast radius? → CLI + TUI, MCP off, BackupService hook required.

Document any deviation in the table above so the policy stays discoverable. Reviewers should reject a new MCP tool that violates these criteria without an explicit waiver in the PR description.

## See also

- [Configuration guide](configuration-guide.md) — `config.backup.retention_count` knob.
- [MCP guide](mcp-guide.md) — agent surface catalogue.
