-- Reshape the `events` hot read-path index so it satisfies the Logs query's
-- ORDER BY as well as its filter, eliminating the `USE TEMP B-TREE FOR ORDER
-- BY` step. Pure schema, no behaviour change; reversible via `DROP INDEX`.
-- Decided empirically by EXPLAIN QUERY PLAN on a migrated, ANALYZEd DB (task
-- #1291, okt-audit debrief finding F5).
--
-- The query (internal/sqlite/list_events.go):
--   WHERE project_id=? [AND event_type IN (...)] [AND created_at >= ?]
--   ORDER BY created_at <dir>, id <dir> LIMIT ?
--
-- migration 034's idx_events_project_type = (project_id, event_type,
-- entity_type, created_at) served the FILTER but left a temp b-tree on the
-- project-only and multi-`event_type IN` paths: entity_type sits between
-- event_type and created_at, and id is absent, so neither the project-only
-- nor the multi-value-IN seek can read rows in (created_at, id) order. EXPLAIN
-- confirmed the temp b-tree before; the two indexes below remove it.

-- Project-only / all-events path: leading project_id equality, then the exact
-- ORDER BY columns (created_at, id). Lets the planner seek the project
-- partition and read rows already in sort order — also serves the multi-value
-- `event_type IN (...)` path, where event_type cannot be an index-ordered
-- prefix (one seek per IN value would not produce a globally sorted stream),
-- so the planner uses this index and filters event_type as a residual while
-- keeping the (created_at, id) ordering.
CREATE INDEX IF NOT EXISTS idx_events_project_created
  ON events(project_id, created_at, id);

-- Single-`event_type` + project path: equality prefix (project_id,
-- event_type) followed by the ORDER BY columns (created_at, id). Supersedes
-- the migration-009 idx_events_type_started (event_type, created_at) for the
-- project-scoped single-type read with no temp b-tree, and serves the
-- project-scoped GROUP BY event_type category aggregate as a covering index
-- (replacing idx_events_project_type's coverage of that path with no
-- regression).
CREATE INDEX IF NOT EXISTS idx_events_project_type_created
  ON events(project_id, event_type, created_at, id);

-- idx_events_project_type (project_id, event_type, entity_type, created_at)
-- from migration 034 is now redundant: every path it served is covered by one
-- of the two indexes above, and it never eliminated the temp b-tree on its
-- target ORDER BY paths. Drop the dead index rather than carry it.
DROP INDEX IF EXISTS idx_events_project_type;
