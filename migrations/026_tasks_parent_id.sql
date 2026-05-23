-- 026_tasks_parent_id.sql — sub-task self-FK on tasks.
--
-- Adds tasks.parent_id so a task can be modelled as a sub-task of
-- another row in the same project. The board hides non-roots
-- (parent_id IS NOT NULL); detail view renders the child panel by
-- filtering on this column.
--
-- ON DELETE policies:
--   tasks.parent_id → CASCADE  (deleting a parent removes its whole
--                               subtree in one DB operation; events
--                               that absorbed comments+activity_logs
--                               in migration 009 hold no FK to tasks
--                               so the audit log survives untouched,
--                               matching today's delete semantics for
--                               root tasks)
--
-- The ALTER TABLE ADD COLUMN call keeps the column nullable so every
-- existing row remains a root task (no backfill) and SQLite's "added
-- FK column must default to NULL" restriction is honoured.
ALTER TABLE tasks ADD COLUMN parent_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_tasks_parent_id ON tasks(parent_id);
