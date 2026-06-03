# Domain events

Omakiten records every comment, task-lifecycle change, operational
tool call, and domain audit event in one append-only `events` table.
The canonical vocabulary is the `EventType*` constants in
[`internal/domain/event.go`](../internal/domain/event.go); the closed
set lives in `domain.KnownEventTypes` (hydrated from the YAML event
registry at boot) and is mirrored, per kit, under
`events.definitions` in the shipped config. The full row-shape table —
which `entity_type` / `event_type` / `entity_id` combination each event
uses — is in [`.docs/internal/data-model.md`](internal/data-model.md).

This page documents the project-scoped events.

## `project.updated`

Fires when a project's mutable metadata is rewritten through the
canonical service layer (`agent.Service.EditProject`). Today the only
mutable field is the `description` column, whose write path was
restored here after living schema-only since migration 002.

| Field | Value |
|---|---|
| `entity_type` | `project` |
| `entity_id` | the project id |
| `project_id` | the project id |
| `event_type` | `project.updated` |
| `payload` | `{"description": {"from": <old>, "to": <new>}}` |

Emitted only when the description actually changes — editing a project
to its current description is a no-op and writes no event. The audit
row carries the calling agent's `agent_model` / `agent_session_id`
(populated from the request context) so `metrics.summary` and the Logs
inspector can attribute the edit.

## `project.removed`

Fires when `app.ProjectService.Delete` hard-deletes a project (and
cascades its dependent rows) after writing a recovery snapshot.

| Field | Value |
|---|---|
| `entity_type` | `project` |
| `event_type` | `project.removed` |
| `payload` | `{"slug", "name", "counters": {...}, "backup_path"}` |

The `backup_path` is the snapshot written before the destructive
transaction so audit consumers can correlate the event with the
recovery artefact.
