-- Drop historical event payloads that were written before the JSON
-- wire format for priority/severity migrated from label string to raw
-- int id. The pre-refactor MarshalJSON on `domain.Priority` and
-- `domain.Severity` emitted the configured label (e.g. "high", "error")
-- via a process-global registry; new code emits the int id explicitly.
-- Mixed-format payloads in `events.payload` would force every reader
-- to accept both shapes — the alternative is to delete the legacy rows
-- and let new events repopulate the table.
--
-- Scope: only the event types whose payloads embedded Priority/Severity
-- handles. Workflow shape events (transitions, guard violations, etc.)
-- never carried those fields and are preserved.

DELETE FROM events
WHERE event_type IN (
    'task.removed',
    'task.archived',
    'task.unarchived',
    'task.edited',
    'task.created'
);
