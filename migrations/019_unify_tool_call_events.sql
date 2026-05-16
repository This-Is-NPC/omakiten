-- Split the catch-all `operation` event_type into source-discriminated
-- `<source>.tool_call` values so hooks can subscribe with `on:
-- mcp.tool_call` (or cli/tui) and filter by payload fields without
-- needing schema migrations every time a new field becomes interesting.
--
-- Migration 009 already folded `activity_logs` into `events` under
-- event_type='operation'. Phase 1 of the in-memory config refactor (task
-- #109) finishes that work: the operational rows now share the same
-- canonical event_type vocabulary as the rest of the unified audit log.
--
-- Payload becomes the source of truth for hook customization. The
-- legacy `events.source`, `events.operation`, `events.entrypoint`,
-- `events.status`, `events.duration_ms`, and `events.error_message`
-- columns remain populated for index-backed metrics queries — they are
-- a performance mirror, not a separate fact. Hooks read payload;
-- metrics.summary keeps reading columns.

-- 1. Rename event_type by source. Rows with an unknown source stay on
--    the legacy value so a follow-up audit can hand-fix them; the
--    activity_logs.go readers continue to accept the legacy event_type
--    for backwards compatibility, but new writes emit only the new
--    values.
UPDATE events
SET event_type = CASE source
    WHEN 'cli' THEN 'cli.tool_call'
    WHEN 'mcp' THEN 'mcp.tool_call'
    WHEN 'tui' THEN 'tui.tool_call'
    ELSE event_type
END
WHERE event_type = 'operation';

-- 2. Enrich payload so hooks can match on tool_name/source/status
--    without reading SQL columns. The pre-019 payload column held the
--    raw arguments JSON (often an object, sometimes empty string).
--    We wrap any prior content under `$.args` and add the mirror
--    fields next to it. Rows whose payload was empty or NULL collapse
--    to `args: {}`.
UPDATE events
SET payload = json_object(
    'tool_name', COALESCE(operation, ''),
    'source', COALESCE(source, ''),
    'entrypoint', COALESCE(entrypoint, ''),
    'status', COALESCE(status, ''),
    'duration_ms', COALESCE(duration_ms, 0),
    'error_message', COALESCE(error_message, ''),
    'args', CASE
        WHEN payload IS NULL OR payload = '' OR payload = '{}' THEN json('{}')
        WHEN json_valid(payload) THEN json(payload)
        ELSE json_object('raw', payload)
    END
)
WHERE event_type IN ('cli.tool_call', 'mcp.tool_call', 'tui.tool_call');
