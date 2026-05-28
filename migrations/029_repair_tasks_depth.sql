-- 029_repair_tasks_depth.sql — repair tasks.depth drift + invariant trigger.
--
-- Migration 028 added tasks.depth + a one-shot backfill, but the
-- invariant (child.depth = parent.depth + 1) lived only in the Go
-- INSERT statement. An older `okt mcp serve` process kept its
-- pre-028-aware INSERT in memory after the binary on disk was
-- upgraded; that statement omitted the depth column, so SQLite
-- wrote rows via the column DEFAULT (0). 028's backfill is
-- one-shot and ran before those rows existed.
--
-- Two-part fix:
--   1. Re-run 028's BACKFILL CTE. Idempotent — already-correct rows
--      land back on the same value. Rows above the 64 cap stay at 0
--      (matches 028's contract; the existing
--      warnTaskDepthBackfillTruncation continues to detect them as
--      legitimate >64 truncations).
--   2. Install AFTER INSERT trigger that auto-computes depth when
--      the caller inserts with parent_id != NULL AND depth = 0.
--      SQLite default `recursive_triggers = off` ensures the inner
--      UPDATE does NOT re-fire the same trigger.
--
-- Hyrum's-Law gap: schema-level enforcement is the canonical defence
-- for invariants otherwise scattered across Go INSERT + Go reparent +
-- migration backfill. Any future caller (script, older binary,
-- ad-hoc SQL session) is now blocked from drifting the invariant.

-- REBACKFILL BEGIN
WITH RECURSIVE depths(id, parent_id, depth) AS (
    SELECT id, parent_id, 0 FROM tasks WHERE parent_id IS NULL
    UNION ALL
    SELECT t.id, t.parent_id, d.depth + 1 FROM tasks t
        INNER JOIN depths d ON t.parent_id = d.id
        WHERE d.depth < 64
)
UPDATE tasks SET depth = (SELECT depth FROM depths WHERE depths.id = tasks.id)
WHERE id IN (SELECT id FROM depths WHERE depth > 0);
-- REBACKFILL END

-- TRIGGER BEGIN
CREATE TRIGGER IF NOT EXISTS tasks_depth_autocompute
AFTER INSERT ON tasks
FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL AND NEW.depth = 0
BEGIN
    UPDATE tasks
       SET depth = COALESCE((SELECT depth + 1 FROM tasks WHERE id = NEW.parent_id), 0)
     WHERE id = NEW.id;
END;
-- TRIGGER END
