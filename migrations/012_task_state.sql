-- Add the active|archived lifecycle column to tasks. Existing rows default to
-- 'active' so the new field is invisible until callers explicitly archive.
-- Archive bypasses bucket policy/transition guards but still respects any
-- operations.archive.guards declared in omakiten.yaml.
ALTER TABLE tasks ADD COLUMN state TEXT NOT NULL DEFAULT 'active'
  CHECK (state IN ('active', 'archived'));

CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project_id, state);
