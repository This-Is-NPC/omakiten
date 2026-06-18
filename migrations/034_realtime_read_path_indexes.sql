-- Index the hot read paths that commit #123's per-tick reload exposed as
-- full scans. Pure schema, no behavior change; each index is reversible
-- via `DROP INDEX` and was justified by a captured EXPLAIN QUERY PLAN
-- before/after diff (see task #1263).

-- `events` is filtered project_id-first by the realtime tick reload and the
-- project-scoped Logs/Stats reads, then ordered by created_at. The existing
-- indexes lead with entity_type (009), event_type (009), or agent_model
-- (010) — none leads with project_id, so the project-filtered path
-- full-scans `events` and sorts via a temp b-tree. Leading with project_id
-- and carrying event_type/entity_type/created_at lets the planner seek the
-- project partition and read the order-by column from the index.
CREATE INDEX IF NOT EXISTS idx_events_project_type
  ON events(project_id, event_type, entity_type, created_at);

-- task_dependencies' PK is (project_id, task_id, depends_on_task_id), so a
-- reverse lookup keyed on depends_on_task_id (the cascade-delete OR branch
-- in DeleteTask, and the plan-network edge JOIN) has no usable PK prefix and
-- full-scans the table. A (project_id, depends_on_task_id) index gives that
-- path a seekable prefix.
CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on
  ON task_dependencies(project_id, depends_on_task_id);
