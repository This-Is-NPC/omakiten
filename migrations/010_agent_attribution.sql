-- Capture which AI model (and optional opaque session id) drove each
-- write. Required so the metrics layer can benchmark agents by behaviour
-- (errors recorded, solutions added, search-before-record ratio, like rate).
-- Coercive: the MCP adapter rejects calls missing _agent_model. CLI reads
-- OMAKITEN_AGENT_MODEL; TUI reports "human". Domain-event timeline starts
-- here — pre-existing rows are not backfilled.

ALTER TABLE events ADD COLUMN agent_model TEXT NOT NULL DEFAULT '';
ALTER TABLE events ADD COLUMN agent_session_id TEXT;

ALTER TABLE errors ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE errors ADD COLUMN entrypoint TEXT NOT NULL DEFAULT '';
ALTER TABLE errors ADD COLUMN agent_model TEXT NOT NULL DEFAULT '';
ALTER TABLE errors ADD COLUMN agent_session_id TEXT;

ALTER TABLE solutions ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE solutions ADD COLUMN entrypoint TEXT NOT NULL DEFAULT '';
ALTER TABLE solutions ADD COLUMN agent_model TEXT NOT NULL DEFAULT '';
ALTER TABLE solutions ADD COLUMN agent_session_id TEXT;

-- Aggregations in metrics.summary group by agent_model + event_type, so a
-- combined index keeps "events recorded by model X over period P" cheap.
CREATE INDEX IF NOT EXISTS idx_events_agent_type ON events(agent_model, event_type, created_at);
