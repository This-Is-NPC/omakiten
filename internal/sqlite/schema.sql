PRAGMA user_version = 1;

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
