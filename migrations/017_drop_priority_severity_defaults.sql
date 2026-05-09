-- Rebuild tasks and laws to drop the SQL-side `DEFAULT` on
-- priority_id / severity_id. Migrations 015 and 016 set those defaults
-- (DEFAULT 2) as a safety net during the TEXT→INTEGER conversion. Now
-- that the app layer always passes an explicit id (resolved via
-- domain.DefaultPriority / DefaultSeverity from the user's config),
-- the SQL defaults are dead weight that obscures the principle: the
-- canonical default lives in `defaults/omakiten.yaml`, NOT in the
-- schema.
--
-- SQLite has no `ALTER COLUMN ... DROP DEFAULT`, so we use the
-- standard "create new table, copy data, drop old, rename new" dance
-- documented at https://www.sqlite.org/lang_altertable.html#otheralter.
-- All within a transaction; foreign keys are checked at COMMIT via
-- defer_foreign_keys = ON for the duration.

PRAGMA defer_foreign_keys = ON;

-- ----- tasks rebuild -----
CREATE TABLE tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    bucket_id INTEGER REFERENCES workflow_buckets(id),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    priority_id INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived')),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT,
    UNIQUE(project_id, id)
);

INSERT INTO tasks_new (id, project_id, bucket_id, title, description, priority_id, state, created_at, updated_at, completed_at)
SELECT id, project_id, bucket_id, title, description, priority_id, state, created_at, updated_at, completed_at FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_new RENAME TO tasks;
CREATE INDEX IF NOT EXISTS idx_tasks_project_bucket ON tasks(project_id, bucket_id);

-- ----- laws rebuild -----
CREATE TABLE laws_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bundle_id INTEGER NOT NULL REFERENCES config_bundles(id),
    local_id INTEGER NOT NULL,
    key TEXT NOT NULL,
    severity_id INTEGER NOT NULL,
    body TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'global',
    project_id INTEGER REFERENCES projects(id),
    persona_id INTEGER REFERENCES personas(id),
    active INTEGER NOT NULL DEFAULT 1,
    UNIQUE(bundle_id, local_id)
);

INSERT INTO laws_new (id, bundle_id, local_id, key, severity_id, body, scope, project_id, persona_id, active)
SELECT id, bundle_id, local_id, key, severity_id, body, scope, project_id, persona_id, active FROM laws;

DROP TABLE laws;
ALTER TABLE laws_new RENAME TO laws;

PRAGMA defer_foreign_keys = OFF;
