-- 023_plans.sql — WBS-style orchestration scaffolding.
--
-- Introduces the plans / plan_waves catalog that lets the user author an
-- implementation plan and group its child tasks in ordered waves. Multi-
-- agent execution (atomic claim, wave_gate guard) and TUI surfaces land
-- in later migrations and service-layer slices; this migration is the
-- pure data-shape baseline.
--
-- ON DELETE policies:
--   plan_waves(plan_id)       → CASCADE   (waves have no meaning without a plan)
--   tasks.plan_id / wave_id   → SET NULL  (tasks survive plan deletion as
--                                          standalone work items; see plan #124
--                                          "Delete cascade" decision)
--
-- The ALTER TABLE ADD COLUMN calls keep the columns nullable so historical
-- rows remain valid and SQLite's "added FK column must default to NULL"
-- restriction is honoured.
CREATE TABLE IF NOT EXISTS plans (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id   INTEGER NOT NULL REFERENCES projects(id),
  slug         TEXT NOT NULL,
  name         TEXT NOT NULL,
  goal_body    TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'done', 'abandoned')),
  created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TEXT,
  UNIQUE(project_id, slug)
);

CREATE TABLE IF NOT EXISTS plan_waves (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id  INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  name     TEXT NOT NULL,
  position INTEGER NOT NULL,
  UNIQUE(plan_id, position)
);

ALTER TABLE tasks ADD COLUMN plan_id     INTEGER REFERENCES plans(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN wave_id     INTEGER REFERENCES plan_waves(id) ON DELETE SET NULL;
ALTER TABLE tasks ADD COLUMN assigned_to TEXT;

CREATE INDEX IF NOT EXISTS idx_tasks_plan_wave ON tasks(plan_id, wave_id);
