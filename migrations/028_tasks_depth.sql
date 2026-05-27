-- 028_tasks_depth.sql — materialized depth column on tasks.
--
-- Adds tasks.depth so event payloads can report the real distance from
-- root (0 for root rows, 1 for direct children, 2 for grandchildren,
-- and so on) without paying for a recursive parent-walk on every event
-- emission. The previous `subject_depth = 1 for any sub-task` encoding
-- was a binary marker; downstream audit consumers and depth-aware
-- hook filters need the true value — see review finding §B.5 of #297.
--
-- Backfill is a one-shot recursive CTE that walks the parent chain
-- from each root (parent_id IS NULL) down through every descendant.
-- The cap (`d.depth < 64`) matches `orphanDepthLimit` in
-- internal/sqlite/orphans.go so the two ceilings stay consistent.
-- Tasks below the cap are NOT updated — they stay at the column
-- default (0) and the orphan path's slog.Warn detects the truncation
-- via `parent_id != nil AND depth == 0` (already wired in #297 §B.2).
--
-- The column is NOT NULL with DEFAULT 0 so existing root rows (the
-- vast majority) silently land at the correct value during ADD COLUMN.
ALTER TABLE tasks ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;

WITH RECURSIVE depths(id, parent_id, depth) AS (
    SELECT id, parent_id, 0 FROM tasks WHERE parent_id IS NULL
    UNION ALL
    SELECT t.id, t.parent_id, d.depth + 1 FROM tasks t
        INNER JOIN depths d ON t.parent_id = d.id
        WHERE d.depth < 64
)
UPDATE tasks SET depth = (SELECT depth FROM depths WHERE depths.id = tasks.id)
WHERE id IN (SELECT id FROM depths WHERE depth > 0);

CREATE INDEX IF NOT EXISTS idx_tasks_depth ON tasks(depth);
