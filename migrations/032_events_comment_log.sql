-- 032_events_comment_log.sql — make `events` the scoped comment/activity log
-- and drop the unreleased `notes` entity (#383, W1 schema migration).
--
-- Comments already live in `events` (event_type='comment'); migration 009
-- folded the old `comments` table into it. This migration extends `events`
-- with the note-like fields comments now need — kind/title/pinned/updated_at —
-- so a comment can carry a heading and be pinned, and recasts the scope model
-- onto the existing (entity_type, entity_id, project_id) columns:
--
--   * entity_type='task'      + entity_id=taskID     — task comment
--   * entity_type='project'   + entity_id=projectID  — project comment
--   * entity_type='universal' + entity_id=NULL       — universal comment
--
-- `events` has no foreign keys, so task-less / project-less comment rows are
-- already legal and no FK work is needed.
--
-- The `notes` entity (migration 031) never shipped — zero rows — so this is a
-- straight DROP with no data migration. Its unified search_index triggers and
-- its dedicated FTS table go with it.

-- ----- 1. extend events with the note-like comment fields -----
-- All nullable / defaulted so existing rows (operation/system/comment) stay
-- valid without a backfill.

ALTER TABLE events ADD COLUMN kind TEXT;
ALTER TABLE events ADD COLUMN title TEXT;
ALTER TABLE events ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE events ADD COLUMN updated_at TEXT;

-- ----- 2. drop the unreleased notes entity -----
-- Order: note search_index triggers (031) → notes_fts + its sync triggers →
-- join table → indexes → base table. DROP TABLE removes a table's own
-- indexes and triggers, but the search_index triggers fire on `notes` and
-- must be dropped explicitly before the table goes away.

DROP TRIGGER IF EXISTS search_index_notes_ai;
DROP TRIGGER IF EXISTS search_index_notes_au;
DROP TRIGGER IF EXISTS search_index_notes_ad;

DROP TRIGGER IF EXISTS notes_fts_ai;
DROP TRIGGER IF EXISTS notes_fts_au;
DROP TRIGGER IF EXISTS notes_fts_ad;

DROP TABLE IF EXISTS notes_fts;

DROP INDEX IF EXISTS idx_notes_tags_tag;
DROP TABLE IF EXISTS notes_tags;

DROP INDEX IF EXISTS idx_notes_project_kind;
DROP INDEX IF EXISTS idx_notes_pinned;
DROP TABLE IF EXISTS notes;

-- No search_index cleanup for 'note' rows is needed: notes never shipped
-- (zero rows), so the unified index can never have held a note row. Skipping
-- the DELETE also keeps this migration applicable to DBs seeded without a
-- materialized search_index (e.g. migration-chain unit fixtures).

-- ----- 3. recast the comment search_index triggers to index title + body -----
-- Replace the migration-022 comment triggers so the indexed content is
-- `body || ' ' || COALESCE(title,'')`, surfacing comment headings in the
-- unified search. project_id may be NULL on universal/project comments where
-- no project applies, so COALESCE(project_id, 0) keeps the cross-project
-- filter (search.go's `si.project_id = ?`) honest — 0 means "no project".

DROP TRIGGER IF EXISTS search_index_comments_ai;
DROP TRIGGER IF EXISTS search_index_comments_au;
DROP TRIGGER IF EXISTS search_index_comments_ad;
DROP TRIGGER IF EXISTS search_index_comments_au_demote;

CREATE TRIGGER IF NOT EXISTS search_index_comments_ai
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

CREATE TRIGGER IF NOT EXISTS search_index_comments_au
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

CREATE TRIGGER IF NOT EXISTS search_index_comments_ad
AFTER DELETE ON events
WHEN OLD.event_type = 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END;

-- Defensive cleanup: if an event row is mutated from event_type='comment' to
-- another type, the _au trigger (WHEN NEW.event_type='comment') does not fire
-- and its search_index row would leak. Preserved from migration 022.
CREATE TRIGGER IF NOT EXISTS search_index_comments_au_demote
AFTER UPDATE ON events
WHEN OLD.event_type = 'comment' AND NEW.event_type != 'comment' BEGIN
  DELETE FROM search_index WHERE entity_type = 'comment' AND entity_id = OLD.id;
END;
