-- Phase 2 of the config refactor (task #110): purge every SQL config
-- table. YAML becomes the single source of truth and reads go through
-- in-memory providers seeded by ConfigService.Import (which now writes
-- zero SQL rows beyond the audit event).
--
-- Dropped tables:
--   workflows, workflow_buckets, workflow_transitions,
--   personas, persona_skills, skills, laws,
--   config_bundles, settings.
--
-- Templates and notifications never had SQL tables — they live in the
-- bundle entirely. tasks.bucket_id keeps its INTEGER storage so the
-- task row can still reference a bucket by id, but the FK pointing at
-- workflow_buckets is removed: the provider snapshot resolves the
-- id↔key mapping post-migration. Backfill is intentionally a no-op —
-- the bundle YAML is the canonical source, and users are advised to
-- back up the `.db` before upgrading (documented in `.docs/`).
--
-- The migration touches FK-bearing rows so we defer FK checks for the
-- duration; SQLite enforces them at COMMIT instead of per-statement.

PRAGMA defer_foreign_keys = ON;

-- ----- tasks rebuild: drop FK to workflow_buckets -----
-- The FK is the only blocker preventing workflow_buckets from being
-- dropped. The new schema keeps bucket_id as a plain INTEGER, indexed
-- for the (project_id, bucket_id) filter the list view depends on.
CREATE TABLE tasks_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id),
    bucket_id INTEGER,
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

-- ----- laws drop -----
-- The laws table referenced personas + projects + config_bundles, and
-- the LawService now reads frontmatter directly from the bundle editor.
DROP TABLE laws;

-- ----- workflow_transitions drop -----
-- workflow_transitions FK→workflow_buckets must die before
-- workflow_buckets.
DROP TABLE workflow_transitions;

-- ----- workflow_buckets drop -----
DROP TABLE workflow_buckets;

-- ----- workflows drop -----
DROP TABLE workflows;

-- ----- persona_skills drop -----
DROP TABLE persona_skills;

-- ----- personas drop -----
DROP TABLE personas;

-- ----- skills drop -----
DROP TABLE skills;

-- ----- settings drop -----
DROP TABLE settings;

-- ----- config_bundles drop -----
DROP TABLE config_bundles;

PRAGMA defer_foreign_keys = OFF;
