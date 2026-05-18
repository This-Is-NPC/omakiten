-- 024_search_index_plans.sql — extend the unified FTS5 search index
-- (migration 022) with a sixth content type: plans.
--
-- Indexed content is `name + ' ' + goal_body` so cross-project `search`
-- finds a plan by its human-readable name OR by any phrase in its
-- markdown goal body. Plans are always scoped to a project, so
-- project_id flows through verbatim.
--
-- Triggers mirror the existing task / context_entries pattern: insert
-- rebuilds the indexed row, update deletes+reinserts, delete drops it.

INSERT INTO search_index(content, entity_type, entity_id, project_id)
SELECT COALESCE(name, '') || ' ' || COALESCE(goal_body, ''), 'plan', id, project_id
FROM plans;

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
