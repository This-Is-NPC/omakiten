CREATE TABLE IF NOT EXISTS errors (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  description TEXT NOT NULL,
  context     TEXT NOT NULL DEFAULT '',
  project_id  INTEGER REFERENCES projects(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_errors_project ON errors(project_id);
CREATE INDEX IF NOT EXISTS idx_errors_created_at ON errors(created_at DESC);

CREATE TABLE IF NOT EXISTS solutions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  error_id    INTEGER NOT NULL REFERENCES errors(id) ON DELETE CASCADE,
  description TEXT NOT NULL,
  steps       TEXT NOT NULL DEFAULT '',
  success     INTEGER CHECK (success IS NULL OR success IN (0, 1)),
  task_id     INTEGER,
  tried_at    TEXT,
  created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_solutions_error ON solutions(error_id);

CREATE TABLE IF NOT EXISTS error_tags (
  error_id INTEGER NOT NULL REFERENCES errors(id) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (error_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_error_tags_tag ON error_tags(tag_id);
