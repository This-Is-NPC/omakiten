-- 025_projects_cascade.sql — rewire every FK referencing projects(id)
-- so that deleting a project row cleans up its dependent data without
-- the service layer running per-table DELETE statements.
--
-- Tables that already cascaded before this migration:
--   project_tags          (CASCADE since 004_tags.sql)
--
-- Tables this migration rebuilds to gain ON DELETE CASCADE:
--   tasks.project_id            (was bare FK; rebuild also keeps the
--                                23 ALTER-added columns plan_id /
--                                wave_id / assigned_to)
--   context_entries.project_id  (was bare FK)
--   errors.project_id           (was ON DELETE SET NULL → now CASCADE;
--                                project-scoped error rows are intentionally
--                                deleted with the project per task #191)
--   plans.project_id            (was bare FK; plan_waves cascade through
--                                plans via its own existing CASCADE)
--   task_dependencies (project_id, task_id) + (project_id, depends_on_task_id)
--                               (composite FKs to tasks were bare; now
--                                cascade so dependency rows die with the
--                                task that anchors them)
--
-- Tables WITHOUT an FK to projects are out of scope for this migration:
--   events       — bare INTEGER project_id; the service does an explicit
--                  DELETE FROM events WHERE project_id = ? in the same
--                  transaction so activity-log / comment rows don't
--                  linger as orphans pointing at a gone project.
--
-- Triggers on every rebuilt table are auto-dropped when the underlying
-- table goes away; the migration recreates them inline so the FTS
-- search_index stays consistent. Indexes are recreated for the same
-- reason. Only the schema_migrations PRAGMA discipline (defer_foreign_keys
-- for the duration) keeps the intermediate state legal.

PRAGMA defer_foreign_keys = ON;

-- ----- tasks rebuild -----
-- Mirror the post-023 column set: 020-shape base + ALTER columns from 023.
CREATE TABLE tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    bucket_id INTEGER,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority_id INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT,
    plan_id INTEGER REFERENCES plans(id) ON DELETE SET NULL,
    wave_id INTEGER REFERENCES plan_waves(id) ON DELETE SET NULL,
    assigned_to TEXT,
    UNIQUE(project_id, id)
);

INSERT INTO tasks_new (id, project_id, bucket_id, title, description, priority_id, state, created_at, updated_at, completed_at, plan_id, wave_id, assigned_to)
SELECT id, project_id, bucket_id, title, description, priority_id, state, created_at, updated_at, completed_at, plan_id, wave_id, assigned_to FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;

CREATE INDEX IF NOT EXISTS idx_tasks_project_bucket ON tasks(project_id, bucket_id);
CREATE INDEX IF NOT EXISTS idx_tasks_plan_wave ON tasks(plan_id, wave_id);

-- ----- triggers: tasks (mirror 022 verbatim) -----
CREATE TRIGGER IF NOT EXISTS search_index_tasks_ai
AFTER INSERT ON tasks BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.description, ''),
    'task',
    NEW.id,
    NEW.project_id
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_tasks_au
AFTER UPDATE ON tasks BEGIN
  DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.description, ''),
    'task',
    NEW.id,
    NEW.project_id
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_tasks_ad
AFTER DELETE ON tasks BEGIN
  DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = OLD.id;
END;

-- ----- context_entries rebuild -----
CREATE TABLE context_entries_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    token_estimate INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO context_entries_new (id, project_id, body, token_estimate, created_at)
SELECT id, project_id, body, token_estimate, created_at FROM context_entries;

DROP TABLE context_entries;
ALTER TABLE context_entries_new RENAME TO context_entries;

-- ----- triggers: context_entries (mirror 022 verbatim) -----
CREATE TRIGGER IF NOT EXISTS search_index_context_entries_ai
AFTER INSERT ON context_entries BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.body, ''), 'context', NEW.id, NEW.project_id);
END;

CREATE TRIGGER IF NOT EXISTS search_index_context_entries_au
AFTER UPDATE ON context_entries BEGIN
  DELETE FROM search_index WHERE entity_type = 'context' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.body, ''), 'context', NEW.id, NEW.project_id);
END;

CREATE TRIGGER IF NOT EXISTS search_index_context_entries_ad
AFTER DELETE ON context_entries BEGIN
  DELETE FROM search_index WHERE entity_type = 'context' AND entity_id = OLD.id;
END;

-- ----- errors rebuild -----
-- Policy swap: ON DELETE SET NULL → ON DELETE CASCADE. The original
-- policy treated errors as cross-project knowledge that survives a
-- project's removal. Task #191 reverses that: errors are
-- project-scoped artefacts and should die with the project so the
-- cross-project search/list views stay free of dangling references.
-- Errors that need to survive must be recorded against project_id=NULL
-- from the start.
--
-- The solutions search triggers (from migration 022) reference errors
-- via a subquery to derive project_id. Dropping errors before that
-- trigger validates fails with "no such table: main.errors" because
-- SQLite re-checks trigger validity at DROP time. Drop the dependent
-- triggers first, then rebuild errors, then recreate the solutions
-- triggers verbatim.
DROP TRIGGER IF EXISTS search_index_solutions_ai;
DROP TRIGGER IF EXISTS search_index_solutions_au;
DROP TRIGGER IF EXISTS search_index_solutions_ad;

CREATE TABLE errors_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    description TEXT NOT NULL,
    context TEXT NOT NULL DEFAULT '',
    project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source TEXT NOT NULL DEFAULT '',
    entrypoint TEXT NOT NULL DEFAULT '',
    agent_model TEXT NOT NULL DEFAULT '',
    agent_session_id TEXT
);

INSERT INTO errors_new (id, description, context, project_id, created_at, source, entrypoint, agent_model, agent_session_id)
SELECT id, description, context, project_id, created_at, source, entrypoint, agent_model, agent_session_id FROM errors;

DROP TABLE errors;
ALTER TABLE errors_new RENAME TO errors;

CREATE INDEX IF NOT EXISTS idx_errors_project ON errors(project_id);
CREATE INDEX IF NOT EXISTS idx_errors_created_at ON errors(created_at DESC);

-- ----- triggers: errors (mirror 022 verbatim) -----
CREATE TRIGGER IF NOT EXISTS search_index_errors_ai
AFTER INSERT ON errors BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.context, ''),
    'error',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_errors_au
AFTER UPDATE ON errors BEGIN
  DELETE FROM search_index WHERE entity_type = 'error' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.context, ''),
    'error',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_errors_ad
AFTER DELETE ON errors BEGIN
  DELETE FROM search_index WHERE entity_type = 'error' AND entity_id = OLD.id;
END;

-- ----- triggers: solutions (recreate after errors rebuild, mirror 022) -----
CREATE TRIGGER IF NOT EXISTS search_index_solutions_ai
AFTER INSERT ON solutions BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.steps, ''),
    'solution',
    NEW.id,
    COALESCE((SELECT project_id FROM errors WHERE id = NEW.error_id), 0)
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_solutions_au
AFTER UPDATE ON solutions BEGIN
  DELETE FROM search_index WHERE entity_type = 'solution' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.steps, ''),
    'solution',
    NEW.id,
    COALESCE((SELECT project_id FROM errors WHERE id = NEW.error_id), 0)
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_solutions_ad
AFTER DELETE ON solutions BEGIN
  DELETE FROM search_index WHERE entity_type = 'solution' AND entity_id = OLD.id;
END;

-- ----- plans rebuild -----
CREATE TABLE plans_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    goal_body TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'done', 'abandoned')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT,
    UNIQUE(project_id, slug)
);

INSERT INTO plans_new (id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at)
SELECT id, project_id, slug, name, goal_body, status, created_at, updated_at, completed_at FROM plans;

DROP TABLE plans;
ALTER TABLE plans_new RENAME TO plans;

-- ----- triggers: plans (mirror 024 verbatim) -----
CREATE TRIGGER IF NOT EXISTS search_index_plans_ai
AFTER INSERT ON plans BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.name, '') || ' ' || COALESCE(NEW.goal_body, ''),
    'plan',
    NEW.id,
    NEW.project_id
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_plans_au
AFTER UPDATE ON plans BEGIN
  DELETE FROM search_index WHERE entity_type = 'plan' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.name, '') || ' ' || COALESCE(NEW.goal_body, ''),
    'plan',
    NEW.id,
    NEW.project_id
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_plans_ad
AFTER DELETE ON plans BEGIN
  DELETE FROM search_index WHERE entity_type = 'plan' AND entity_id = OLD.id;
END;

-- ----- task_dependencies rebuild -----
-- Composite FK to tasks(project_id, id) was bare; cascade dependency
-- rows when either end is deleted (project removal kills tasks via
-- the new cascade, which in turn kills these rows).
CREATE TABLE task_dependencies_new (
    project_id INTEGER NOT NULL,
    task_id INTEGER NOT NULL,
    depends_on_task_id INTEGER NOT NULL,
    PRIMARY KEY (project_id, task_id, depends_on_task_id),
    FOREIGN KEY(project_id, task_id) REFERENCES tasks(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, depends_on_task_id) REFERENCES tasks(project_id, id) ON DELETE CASCADE,
    CHECK (task_id != depends_on_task_id)
);

INSERT INTO task_dependencies_new (project_id, task_id, depends_on_task_id)
SELECT project_id, task_id, depends_on_task_id FROM task_dependencies;

DROP TABLE task_dependencies;
ALTER TABLE task_dependencies_new RENAME TO task_dependencies;

PRAGMA defer_foreign_keys = OFF;
