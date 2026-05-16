-- Unified FTS5 search across the five content entities: tasks, comments,
-- errors, solutions, context entries. Replaces the per-table errors.search
-- path with a single virtual table + triggers; the agent layer exposes the
-- index through a new `search` MCP tool.
--
-- Comments are not a standalone table: migration 009 folded them into
-- `events` rows where event_type='comment' (entity_id stores the parent
-- task id). Triggers and backfill therefore target `events` with that
-- filter rather than a `comments` table that no longer exists.
--
-- Solutions have no project_id column; the trigger derives it from
-- errors(project_id) via solutions.error_id.
--
-- Tokenizer: `porter unicode61` — porter stemming over the unicode61
-- folding tokenizer. Reasonable for English/Portuguese. UNINDEXED columns
-- carry the routing metadata (entity_type, entity_id, project_id) without
-- contributing to the FTS index. project_id 0 means "no project" (errors
-- and solutions may live outside a project).

CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
  content,
  entity_type UNINDEXED,
  entity_id UNINDEXED,
  project_id UNINDEXED,
  tokenize = "porter unicode61"
);

-- ----- backfill: seed existing rows -----

INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT COALESCE(title, '') || ' ' || COALESCE(description, ''), 'task', id, project_id
FROM tasks;

INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT COALESCE(body, ''), 'comment', id, COALESCE(project_id, 0)
FROM events
WHERE entity_type = 'task' AND event_type = 'comment';

INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT COALESCE(description, '') || ' ' || COALESCE(context, ''), 'error', id, COALESCE(project_id, 0)
FROM errors;

INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT
  COALESCE(s.description, '') || ' ' || COALESCE(s.steps, ''),
  'solution',
  s.id,
  COALESCE((SELECT project_id FROM errors WHERE id = s.error_id), 0)
FROM solutions s;

INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT COALESCE(body, ''), 'context', id, project_id
FROM context_entries;

-- ----- triggers: tasks -----
-- Title or description changes mutate the indexed content. The trigger
-- fires on any UPDATE — filtering by columns is not portable in SQLite
-- without OF, and the per-row delete+insert pattern stays cheap.

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

-- ----- triggers: comments (events with event_type='comment') -----
-- A WHEN clause keeps the trigger inert for the other 30+ event types so
-- the hot path of task.moved / operation events stays unaffected.

CREATE TRIGGER IF NOT EXISTS search_index_comments_ai
AFTER INSERT ON events
WHEN NEW.event_type = 'comment' BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.body, ''), 'comment', NEW.id, COALESCE(NEW.project_id, 0));
END;

CREATE TRIGGER IF NOT EXISTS search_index_comments_au
AFTER UPDATE ON events
WHEN NEW.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (COALESCE(NEW.body, ''), 'comment', NEW.id, COALESCE(NEW.project_id, 0));
END;

CREATE TRIGGER IF NOT EXISTS search_index_comments_ad
AFTER DELETE ON events
WHEN OLD.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END;

-- Defensive cleanup: if an event row is mutated from event_type='comment'
-- to another type, the `_au` trigger above (WHEN NEW.event_type='comment')
-- does not fire and the matching search_index row would leak. The current
-- code path never rewrites event_type, but the trigger keeps the index
-- correct if that invariant is ever relaxed.
CREATE TRIGGER IF NOT EXISTS search_index_comments_au_demote
AFTER UPDATE ON events
WHEN OLD.event_type = 'comment' AND NEW.event_type != 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END;

-- ----- triggers: errors -----

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

-- ----- triggers: solutions (project_id derived from errors via error_id) -----

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

-- ----- triggers: context_entries -----

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
