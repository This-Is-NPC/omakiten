-- 027_tasks_parent_project_fk.sql — defence-in-depth guard for
-- tasks.parent_id scoping.
--
-- Migration 026 introduced tasks.parent_id with a single-column FK
-- (parent_id → tasks(id)). That guarantees the parent row exists but
-- does NOT guarantee the parent shares the child's project_id —
-- nothing at the SQL layer rejected an INSERT or UPDATE that
-- attached a child in project A to a parent in project B. The
-- app-layer guard in internal/sqlite/tasks_parent.go closed the hole
-- for the documented call paths; this migration adds the matching
-- DB-side trigger so any future code path (direct SQL, ad-hoc
-- migration, restored backup) cannot reopen it.
--
-- SQLite does not support multi-column FKs declared post-hoc via
-- ALTER TABLE (and rewriting the tasks table to add one would
-- cascade through every index + view that references the column),
-- so we use a BEFORE INSERT / BEFORE UPDATE trigger pair instead.
-- Both raise via RAISE(ABORT, ...) when the parent row's project_id
-- differs from NEW.project_id; nullable parent_id passes straight
-- through (the WHERE NEW.parent_id IS NOT NULL guard short-circuits
-- root inserts so the per-row cost is a single null check).

DROP TRIGGER IF EXISTS tasks_parent_project_insert_guard;
DROP TRIGGER IF EXISTS tasks_parent_project_update_guard;

CREATE TRIGGER tasks_parent_project_insert_guard
BEFORE INSERT ON tasks
FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN (SELECT project_id FROM tasks WHERE id = NEW.parent_id) IS NULL
            THEN RAISE(ABORT, 'tasks.parent_id references missing task')
        WHEN (SELECT project_id FROM tasks WHERE id = NEW.parent_id) != NEW.project_id
            THEN RAISE(ABORT, 'tasks.parent_id must point to a task in the same project')
    END;
END;

CREATE TRIGGER tasks_parent_project_update_guard
BEFORE UPDATE OF parent_id, project_id ON tasks
FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL
BEGIN
    SELECT CASE
        WHEN (SELECT project_id FROM tasks WHERE id = NEW.parent_id) IS NULL
            THEN RAISE(ABORT, 'tasks.parent_id references missing task')
        WHEN (SELECT project_id FROM tasks WHERE id = NEW.parent_id) != NEW.project_id
            THEN RAISE(ABORT, 'tasks.parent_id must point to a task in the same project')
    END;
END;
