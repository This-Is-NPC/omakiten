-- Unify operational telemetry, system events, and comments under a single
-- `events` log keyed by (entity_type, entity_id). The activity feed reads
-- entity_type='task' rows; the logs view reads event_type='operation' rows;
-- comments_add becomes a thin wrapper that writes event_type='comment'.

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  entity_type TEXT NOT NULL,
  entity_id INTEGER,
  project_id INTEGER,
  project_slug TEXT,
  event_type TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  author_type TEXT CHECK (author_type IS NULL OR author_type IN ('human', 'agent')),
  source TEXT,
  entrypoint TEXT,
  operation TEXT,
  status TEXT,
  duration_ms INTEGER,
  error_message TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);

-- Activity feed lookups: (entity_type, entity_id, created_at).
CREATE INDEX IF NOT EXISTS idx_events_entity ON events(entity_type, entity_id, created_at);
-- Logs view + pruning both filter by event_type.
CREATE INDEX IF NOT EXISTS idx_events_type_started ON events(event_type, created_at);

-- Carry comments over with their ids intact so comment_tags can be re-keyed
-- as a straight copy. Comments become entity_type='task' events.
INSERT INTO events(id, entity_type, entity_id, project_id, event_type, body, author_type, created_at)
SELECT id, 'task', task_id, project_id, 'comment', body, author_type, created_at FROM comments;

-- Carry activity_logs over as event_type='operation' events. Their ids are
-- not referenced from any other table, so we let AUTOINCREMENT pick fresh
-- ones beyond the migrated comment range.
INSERT INTO events(entity_type, project_id, project_slug, event_type, payload, source, entrypoint, operation, status, duration_ms, error_message, created_at, finished_at)
SELECT
  'system',
  CASE WHEN project_id > 0 THEN project_id ELSE NULL END,
  CASE WHEN project_slug = '' THEN NULL ELSE project_slug END,
  'operation',
  COALESCE(arguments_json, '{}'),
  source,
  entrypoint,
  operation,
  status,
  CASE WHEN duration_ms = 0 AND finished_at IS NULL THEN NULL ELSE duration_ms END,
  CASE WHEN error_message = '' THEN NULL ELSE error_message END,
  started_at,
  finished_at
FROM activity_logs;

-- Reseed AUTOINCREMENT so future inserts pick up after the highest id we
-- wrote during migration (handles both the comment-id range and any
-- operation rows that landed after).
INSERT OR REPLACE INTO sqlite_sequence(name, seq)
VALUES ('events', COALESCE((SELECT MAX(id) FROM events), 0));

-- Re-key the comment_tags join table onto events. Comment ids were preserved
-- above, so this is a direct copy.
CREATE TABLE IF NOT EXISTS event_tags (
  event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (event_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_event_tags_tag ON event_tags(tag_id);

INSERT INTO event_tags(event_id, tag_id) SELECT comment_id, tag_id FROM comment_tags;

DROP TABLE comment_tags;
DROP TABLE comments;
DROP TABLE activity_logs;
