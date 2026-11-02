PRAGMA user_version = 4;

CREATE TABLE projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  archived_at TEXT,
  description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE tasks (
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
  parent_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
  depth INTEGER NOT NULL DEFAULT 0,
  UNIQUE(project_id, id)
);

CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  entity_type TEXT NOT NULL,
  entity_id INTEGER,
  project_id INTEGER,
  project_slug TEXT,
  event_type TEXT NOT NULL,
  body TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  author_type TEXT CHECK (author_type IS NULL OR author_type IN ('human', 'agent')),
  source TEXT,
  entrypoint TEXT,
  operation TEXT,
  status TEXT,
  duration_ms INTEGER,
  error_message TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT,
  agent_model TEXT NOT NULL DEFAULT '',
  agent_session_id TEXT,
  kind TEXT,
  title TEXT,
  pinned INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT
);

CREATE TABLE tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  label TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE task_tags (
  project_id INTEGER NOT NULL,
  task_id INTEGER NOT NULL,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (project_id, task_id, tag_id),
  FOREIGN KEY (project_id, task_id) REFERENCES tasks(project_id, id) ON DELETE CASCADE
);

CREATE TABLE project_tags (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (project_id, tag_id)
);

CREATE TABLE errors (
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

CREATE TABLE solutions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  error_id INTEGER NOT NULL REFERENCES errors(id) ON DELETE CASCADE,
  description TEXT NOT NULL,
  steps TEXT NOT NULL DEFAULT '',
  success INTEGER CHECK (success IS NULL OR success IN (0, 1)),
  task_id INTEGER,
  tried_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  likes INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT '',
  entrypoint TEXT NOT NULL DEFAULT '',
  agent_model TEXT NOT NULL DEFAULT '',
  agent_session_id TEXT
);

CREATE TABLE error_tags (
  error_id INTEGER NOT NULL REFERENCES errors(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (error_id, tag_id)
);

CREATE TABLE event_tags (
  event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (event_id, tag_id)
);

CREATE TABLE task_dependencies (
  project_id INTEGER NOT NULL,
  task_id INTEGER NOT NULL,
  depends_on_task_id INTEGER NOT NULL,
  PRIMARY KEY (project_id, task_id, depends_on_task_id),
  FOREIGN KEY (project_id, task_id) REFERENCES tasks(project_id, id) ON DELETE CASCADE,
  FOREIGN KEY (project_id, depends_on_task_id) REFERENCES tasks(project_id, id) ON DELETE CASCADE,
  CHECK (task_id != depends_on_task_id)
);

CREATE TABLE plans (
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

CREATE TABLE plan_waves (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id INTEGER NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  position INTEGER NOT NULL,
  UNIQUE(plan_id, position)
);

CREATE INDEX idx_tasks_project_bucket ON tasks(project_id, bucket_id);
CREATE INDEX idx_tasks_plan_wave ON tasks(plan_id, wave_id);
CREATE INDEX idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX idx_tasks_depth ON tasks(depth);
CREATE INDEX idx_errors_project ON errors(project_id);
CREATE INDEX idx_errors_created_at ON errors(created_at DESC);
CREATE INDEX idx_solutions_error ON solutions(error_id);
CREATE INDEX idx_solutions_likes ON solutions(likes DESC);
CREATE INDEX idx_event_tags_tag ON event_tags(tag_id);
CREATE INDEX idx_error_tags_tag ON error_tags(tag_id);
CREATE INDEX idx_task_tags_tag ON task_tags(tag_id);
CREATE INDEX idx_project_tags_tag ON project_tags(tag_id);
CREATE INDEX idx_task_deps_depends_on ON task_dependencies(project_id, depends_on_task_id);
CREATE INDEX idx_events_entity ON events(entity_type, entity_id, created_at);
CREATE INDEX idx_events_type_started ON events(event_type, created_at);
CREATE INDEX idx_events_agent_type ON events(agent_model, event_type, created_at);
CREATE INDEX idx_events_project_created ON events(project_id, created_at, id);
CREATE INDEX idx_events_project_type_created ON events(project_id, event_type, created_at, id);

CREATE VIRTUAL TABLE search_index USING fts5(
  content,
  entity_type UNINDEXED,
  entity_id UNINDEXED,
  project_id UNINDEXED,
  tokenize = "porter unicode61"
);

CREATE TRIGGER search_index_tasks_ai
AFTER INSERT ON tasks BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.description, ''),
    'task',
    NEW.id,
    NEW.project_id
  );
END;

CREATE TRIGGER search_index_tasks_au
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

CREATE TRIGGER search_index_tasks_ad
AFTER DELETE ON tasks BEGIN
  DELETE FROM search_index WHERE entity_type = 'task' AND entity_id = OLD.id;
END;

CREATE TRIGGER search_index_comments_ai
AFTER INSERT ON events
WHEN NEW.event_type = 'comment' BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.body, '') || ' ' || COALESCE(NEW.title, ''),
    'comment',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER search_index_comments_au
AFTER UPDATE ON events
WHEN NEW.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.body, '') || ' ' || COALESCE(NEW.title, ''),
    'comment',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER search_index_comments_ad
AFTER DELETE ON events
WHEN OLD.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END;

CREATE TRIGGER search_index_comments_au_demote
AFTER UPDATE ON events
WHEN OLD.event_type = 'comment' AND NEW.event_type != 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END;

CREATE TRIGGER search_index_errors_ai
AFTER INSERT ON errors BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.context, ''),
    'error',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER search_index_errors_au
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

CREATE TRIGGER search_index_errors_ad
AFTER DELETE ON errors BEGIN
  DELETE FROM search_index WHERE entity_type = 'error' AND entity_id = OLD.id;
END;

CREATE TRIGGER search_index_solutions_ai
AFTER INSERT ON solutions BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.description, '') || ' ' || COALESCE(NEW.steps, ''),
    'solution',
    NEW.id,
    COALESCE((SELECT project_id FROM errors WHERE id = NEW.error_id), 0)
  );
END;

CREATE TRIGGER search_index_solutions_au
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

CREATE TRIGGER search_index_solutions_ad
AFTER DELETE ON solutions BEGIN
  DELETE FROM search_index WHERE entity_type = 'solution' AND entity_id = OLD.id;
END;

CREATE TRIGGER search_index_plans_ai
AFTER INSERT ON plans BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.name, '') || ' ' || COALESCE(NEW.goal_body, ''),
    'plan',
    NEW.id,
    NEW.project_id
  );
END;

CREATE TRIGGER search_index_plans_au
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

CREATE TRIGGER search_index_plans_ad
AFTER DELETE ON plans BEGIN
  DELETE FROM search_index WHERE entity_type = 'plan' AND entity_id = OLD.id;
END;

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

CREATE TRIGGER tasks_depth_autocompute
AFTER INSERT ON tasks
FOR EACH ROW
WHEN NEW.parent_id IS NOT NULL AND NEW.depth = 0
BEGIN
    UPDATE tasks
       SET depth = COALESCE((SELECT depth + 1 FROM tasks WHERE id = NEW.parent_id), 0)
     WHERE id = NEW.id;
END;

CREATE TABLE document_metadata (
  id INTEGER PRIMARY KEY,
  task_id INTEGER UNIQUE REFERENCES tasks(id) ON DELETE CASCADE,
  plan_id INTEGER UNIQUE REFERENCES plans(id) ON DELETE CASCADE,
  metadata TEXT NOT NULL,
  CHECK ((task_id IS NULL) != (plan_id IS NULL))
);

CREATE TABLE agent_teams (
  id TEXT PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  task_id INTEGER,
  plan_id INTEGER REFERENCES plans(id) ON DELETE CASCADE,
  bucket_key TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('active', 'failed', 'closed')),
  opened_after INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ((task_id IS NULL) != (plan_id IS NULL)),
  FOREIGN KEY (project_id, task_id) REFERENCES tasks(project_id, id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_agent_teams_active ON agent_teams(project_id, task_id, bucket_key) WHERE status = 'active' AND task_id IS NOT NULL;
CREATE UNIQUE INDEX idx_agent_teams_plan_active ON agent_teams(plan_id) WHERE status = 'active' AND plan_id IS NOT NULL;

CREATE TABLE agent_sessions (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
  member TEXT NOT NULL,
  command TEXT NOT NULL,
  persona TEXT NOT NULL DEFAULT '',
  harness TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  pid INTEGER NOT NULL DEFAULT 0,
  transcript TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('running', 'failed', 'done', 'stopped', 'replaced')),
  exit_code INTEGER,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);

CREATE UNIQUE INDEX idx_agent_sessions_seat ON agent_sessions(team_id, member) WHERE status != 'replaced';

CREATE INDEX idx_events_agent_session ON events(agent_session_id, id) WHERE agent_session_id IS NOT NULL;

CREATE TABLE sync_identity (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  device_id TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  hlc TEXT NOT NULL DEFAULT '',
  remote_url TEXT NOT NULL DEFAULT '',
  applying INTEGER NOT NULL DEFAULT 0
);

ALTER TABLE projects ADD COLUMN uid TEXT;
ALTER TABLE tasks ADD COLUMN uid TEXT;
ALTER TABLE events ADD COLUMN uid TEXT;
ALTER TABLE errors ADD COLUMN uid TEXT;
ALTER TABLE solutions ADD COLUMN uid TEXT;
ALTER TABLE plans ADD COLUMN uid TEXT;
ALTER TABLE plan_waves ADD COLUMN uid TEXT;

CREATE UNIQUE INDEX idx_projects_uid ON projects(uid);
CREATE UNIQUE INDEX idx_tasks_uid ON tasks(uid);
CREATE UNIQUE INDEX idx_events_uid ON events(uid);
CREATE UNIQUE INDEX idx_errors_uid ON errors(uid);
CREATE UNIQUE INDEX idx_solutions_uid ON solutions(uid);
CREATE UNIQUE INDEX idx_plans_uid ON plans(uid);
CREATE UNIQUE INDEX idx_plan_waves_uid ON plan_waves(uid);

ALTER TABLE events ADD COLUMN hlc TEXT;
ALTER TABLE events ADD COLUMN device_id TEXT;
ALTER TABLE events ADD COLUMN actor_id TEXT;

ALTER TABLE projects ADD COLUMN sync_scope TEXT NOT NULL DEFAULT 'local' CHECK (sync_scope IN ('local', 'synced'));

CREATE TABLE sync_rows (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  tbl TEXT NOT NULL,
  row_uid TEXT NOT NULL,
  hlc TEXT,
  deleted INTEGER NOT NULL DEFAULT 0,
  local INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (project_id, tbl, row_uid)
);

CREATE INDEX idx_sync_rows_pending ON sync_rows(project_id, local, hlc);

CREATE TABLE sync_projects (
  project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  pushed_hlc TEXT NOT NULL DEFAULT '',
  pull_cursor INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sync_inbox (
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  tbl TEXT NOT NULL,
  row_uid TEXT NOT NULL,
  hlc TEXT NOT NULL,
  change TEXT NOT NULL,
  missing_tbl TEXT NOT NULL,
  missing_uid TEXT NOT NULL,
  PRIMARY KEY (project_id, tbl, row_uid)
);

CREATE INDEX idx_sync_inbox_missing ON sync_inbox(project_id, missing_tbl, missing_uid);

CREATE VIEW sync_capture_projects AS
SELECT id FROM projects
WHERE sync_scope = 'synced' AND (SELECT applying FROM sync_identity WHERE id = 1) = 0;

-- The activity-log event types: device-local records that are never captured nor pushed.
CREATE VIEW sync_activity_event_types AS
SELECT 'cli.tool_call' AS event_type UNION ALL SELECT 'tui.tool_call';

-- An insert replaces the row of the same (project_id, tbl, row_uid). A conflict clause here would be
-- overridden by the statement that fired the trigger, so the replace is a trigger.
CREATE TRIGGER sync_rows_replace BEFORE INSERT ON sync_rows
BEGIN
  DELETE FROM sync_rows WHERE project_id = NEW.project_id AND tbl = NEW.tbl AND row_uid = NEW.row_uid;
END;

CREATE TRIGGER sync_rows_stamp AFTER INSERT ON sync_rows WHEN NEW.local = 1
BEGIN
  UPDATE sync_identity SET hlc = okt_hlc_tick(hlc, device_id) WHERE id = 1;
  UPDATE sync_rows SET hlc = (SELECT hlc FROM sync_identity WHERE id = 1)
  WHERE project_id = NEW.project_id AND tbl = NEW.tbl AND row_uid = NEW.row_uid;
END;

CREATE TRIGGER sync_capture_project_update AFTER UPDATE OF name, description ON projects
WHEN NEW.name IS NOT OLD.name OR NEW.description IS NOT OLD.description
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'projects', u, 0 FROM (SELECT NEW.id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_plans_insert AFTER INSERT ON plans
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'plans', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_plans_update AFTER UPDATE OF slug, name, goal_body, status, created_at, updated_at, completed_at ON plans
WHEN NEW.slug IS NOT OLD.slug OR NEW.name IS NOT OLD.name OR NEW.goal_body IS NOT OLD.goal_body OR NEW.status IS NOT OLD.status OR NEW.created_at IS NOT OLD.created_at OR NEW.updated_at IS NOT OLD.updated_at OR NEW.completed_at IS NOT OLD.completed_at
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'plans', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_plans_delete AFTER DELETE ON plans
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'plans', u, 1 FROM (SELECT OLD.project_id AS p, OLD.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_plan_waves_insert AFTER INSERT ON plan_waves
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'plan_waves', u, 0 FROM (SELECT (SELECT project_id FROM plans WHERE id = NEW.plan_id) AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_plan_waves_update AFTER UPDATE OF plan_id, name, position ON plan_waves
WHEN NEW.plan_id IS NOT OLD.plan_id OR NEW.name IS NOT OLD.name OR NEW.position IS NOT OLD.position
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'plan_waves', u, 0 FROM (SELECT (SELECT project_id FROM plans WHERE id = NEW.plan_id) AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_plan_waves_delete AFTER DELETE ON plan_waves
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'plan_waves', u, 1 FROM (SELECT (SELECT project_id FROM plans WHERE id = OLD.plan_id) AS p, OLD.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_tasks_insert AFTER INSERT ON tasks
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'tasks', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_tasks_update AFTER UPDATE OF bucket_id, title, description, priority_id, state, created_at, updated_at, completed_at, plan_id, wave_id, assigned_to, parent_id, depth ON tasks
WHEN NEW.bucket_id IS NOT OLD.bucket_id OR NEW.title IS NOT OLD.title OR NEW.description IS NOT OLD.description OR NEW.priority_id IS NOT OLD.priority_id OR NEW.state IS NOT OLD.state OR NEW.created_at IS NOT OLD.created_at OR NEW.updated_at IS NOT OLD.updated_at OR NEW.completed_at IS NOT OLD.completed_at OR NEW.plan_id IS NOT OLD.plan_id OR NEW.wave_id IS NOT OLD.wave_id OR NEW.assigned_to IS NOT OLD.assigned_to OR NEW.parent_id IS NOT OLD.parent_id OR NEW.depth IS NOT OLD.depth
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'tasks', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_tasks_delete AFTER DELETE ON tasks
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'tasks', u, 1 FROM (SELECT OLD.project_id AS p, OLD.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_errors_insert AFTER INSERT ON errors
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'errors', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_errors_update AFTER UPDATE OF description, context, created_at, source, entrypoint, agent_model, agent_session_id ON errors
WHEN NEW.description IS NOT OLD.description OR NEW.context IS NOT OLD.context OR NEW.created_at IS NOT OLD.created_at OR NEW.source IS NOT OLD.source OR NEW.entrypoint IS NOT OLD.entrypoint OR NEW.agent_model IS NOT OLD.agent_model OR NEW.agent_session_id IS NOT OLD.agent_session_id
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'errors', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_errors_delete AFTER DELETE ON errors
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'errors', u, 1 FROM (SELECT OLD.project_id AS p, OLD.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_solutions_insert AFTER INSERT ON solutions
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'solutions', u, 0 FROM (SELECT (SELECT project_id FROM errors WHERE id = NEW.error_id) AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_solutions_update AFTER UPDATE OF error_id, description, steps, success, task_id, tried_at, created_at, likes, source, entrypoint, agent_model, agent_session_id ON solutions
WHEN NEW.error_id IS NOT OLD.error_id OR NEW.description IS NOT OLD.description OR NEW.steps IS NOT OLD.steps OR NEW.success IS NOT OLD.success OR NEW.task_id IS NOT OLD.task_id OR NEW.tried_at IS NOT OLD.tried_at OR NEW.created_at IS NOT OLD.created_at OR NEW.likes IS NOT OLD.likes OR NEW.source IS NOT OLD.source OR NEW.entrypoint IS NOT OLD.entrypoint OR NEW.agent_model IS NOT OLD.agent_model OR NEW.agent_session_id IS NOT OLD.agent_session_id
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'solutions', u, 0 FROM (SELECT (SELECT project_id FROM errors WHERE id = NEW.error_id) AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_solutions_delete AFTER DELETE ON solutions
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'solutions', u, 1 FROM (SELECT (SELECT project_id FROM errors WHERE id = OLD.error_id) AS p, OLD.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_events_insert AFTER INSERT ON events
WHEN NEW.event_type NOT IN (SELECT event_type FROM sync_activity_event_types)
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'events', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_events_update AFTER UPDATE OF entity_type, entity_id, project_slug, event_type, body, payload, author_type, source, entrypoint, operation, status, duration_ms, error_message, created_at, finished_at, agent_model, agent_session_id, kind, title, pinned, updated_at, hlc, device_id, actor_id ON events
WHEN NEW.event_type NOT IN (SELECT event_type FROM sync_activity_event_types) AND (NEW.entity_type IS NOT OLD.entity_type OR NEW.entity_id IS NOT OLD.entity_id OR NEW.project_slug IS NOT OLD.project_slug OR NEW.event_type IS NOT OLD.event_type OR NEW.body IS NOT OLD.body OR NEW.payload IS NOT OLD.payload OR NEW.author_type IS NOT OLD.author_type OR NEW.source IS NOT OLD.source OR NEW.entrypoint IS NOT OLD.entrypoint OR NEW.operation IS NOT OLD.operation OR NEW.status IS NOT OLD.status OR NEW.duration_ms IS NOT OLD.duration_ms OR NEW.error_message IS NOT OLD.error_message OR NEW.created_at IS NOT OLD.created_at OR NEW.finished_at IS NOT OLD.finished_at OR NEW.agent_model IS NOT OLD.agent_model OR NEW.agent_session_id IS NOT OLD.agent_session_id OR NEW.kind IS NOT OLD.kind OR NEW.title IS NOT OLD.title OR NEW.pinned IS NOT OLD.pinned OR NEW.updated_at IS NOT OLD.updated_at OR NEW.hlc IS NOT OLD.hlc OR NEW.device_id IS NOT OLD.device_id OR NEW.actor_id IS NOT OLD.actor_id)
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'events', u, 0 FROM (SELECT NEW.project_id AS p, NEW.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_events_delete AFTER DELETE ON events
WHEN OLD.event_type NOT IN (SELECT event_type FROM sync_activity_event_types)
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'events', u, 1 FROM (SELECT OLD.project_id AS p, OLD.uid AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_task_dependencies_insert AFTER INSERT ON task_dependencies
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'task_dependencies', u, 0 FROM (SELECT NEW.project_id AS p, (SELECT uid FROM tasks WHERE id = NEW.task_id) || '|' || (SELECT uid FROM tasks WHERE id = NEW.depends_on_task_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_task_dependencies_delete AFTER DELETE ON task_dependencies
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'task_dependencies', u, 1 FROM (SELECT OLD.project_id AS p, (SELECT uid FROM tasks WHERE id = OLD.task_id) || '|' || (SELECT uid FROM tasks WHERE id = OLD.depends_on_task_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_task_tags_insert AFTER INSERT ON task_tags
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'task_tags', u, 0 FROM (SELECT NEW.project_id AS p, (SELECT uid FROM tasks WHERE id = NEW.task_id) || '|' || (SELECT name FROM tags WHERE id = NEW.tag_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_task_tags_delete AFTER DELETE ON task_tags
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'task_tags', u, 1 FROM (SELECT OLD.project_id AS p, (SELECT uid FROM tasks WHERE id = OLD.task_id) || '|' || (SELECT name FROM tags WHERE id = OLD.tag_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_error_tags_insert AFTER INSERT ON error_tags
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'error_tags', u, 0 FROM (SELECT (SELECT project_id FROM errors WHERE id = NEW.error_id) AS p, (SELECT uid FROM errors WHERE id = NEW.error_id) || '|' || (SELECT name FROM tags WHERE id = NEW.tag_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_error_tags_delete AFTER DELETE ON error_tags
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'error_tags', u, 1 FROM (SELECT (SELECT project_id FROM errors WHERE id = OLD.error_id) AS p, (SELECT uid FROM errors WHERE id = OLD.error_id) || '|' || (SELECT name FROM tags WHERE id = OLD.tag_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_event_tags_insert AFTER INSERT ON event_tags
WHEN (SELECT event_type FROM events WHERE id = NEW.event_id) NOT IN (SELECT event_type FROM sync_activity_event_types)
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'event_tags', u, 0 FROM (SELECT (SELECT project_id FROM events WHERE id = NEW.event_id) AS p, (SELECT uid FROM events WHERE id = NEW.event_id) || '|' || (SELECT name FROM tags WHERE id = NEW.tag_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;

CREATE TRIGGER sync_capture_event_tags_delete AFTER DELETE ON event_tags
WHEN (SELECT event_type FROM events WHERE id = OLD.event_id) NOT IN (SELECT event_type FROM sync_activity_event_types)
BEGIN
  INSERT INTO sync_rows(project_id, tbl, row_uid, deleted)
  SELECT p, 'event_tags', u, 1 FROM (SELECT (SELECT project_id FROM events WHERE id = OLD.event_id) AS p, (SELECT uid FROM events WHERE id = OLD.event_id) || '|' || (SELECT name FROM tags WHERE id = OLD.tag_id) AS u)
  WHERE u IS NOT NULL AND p IN (SELECT id FROM sync_capture_projects);
END;
