-- 031_notes.sql — notes entity (#360, part of umbrella #359).
--
-- Notes are project-or-global SQLite rows, comment-like but project-level
-- (never task-scoped). Scope is encoded in project_id: NULL = global,
-- non-NULL = project-scoped. The kind column is a free string (convention:
-- handoff, decision, architecture, requirements, runbook, gotcha,
-- retrospective, glossary, free) so users can introduce new note kinds
-- without a schema migration.
--
-- Indexes:
--   * (project_id, kind)         — list filters by scope + kind
--   * partial (project_id, pinned) WHERE pinned = 1
--                                 — fast "pinned cover sheet" reads stay
--                                 small even as the table grows
--
-- Tags reuse the global `tags` table via the notes_tags join.
--
-- FTS5 sync: notes_fts is a content-table-backed virtual table that
-- mirrors title+body for SQLite's native MATCH queries. notes also lands
-- in the unified search_index (migration 022) under entity_type='note'
-- so the cross-entity `search` MCP tool surfaces them with the existing
-- BM25 / snippet pipeline. Two indexes — by design — because the
-- per-entity MATCH path is reserved for future "search within notes"
-- surfaces (cover-sheet rendering / notes_list FTS option) while the
-- unified index keeps cross-entity discoverability working without
-- bespoke wiring.

CREATE TABLE IF NOT EXISTS notes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id   INTEGER NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL,
  title        TEXT NOT NULL,
  body         TEXT NOT NULL,
  pinned       INTEGER NOT NULL DEFAULT 0,
  author_model TEXT,
  created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notes_project_kind ON notes(project_id, kind);
CREATE INDEX IF NOT EXISTS idx_notes_pinned ON notes(project_id, pinned) WHERE pinned = 1;

CREATE TABLE IF NOT EXISTS notes_tags (
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (note_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_notes_tags_tag ON notes_tags(tag_id);

-- ----- notes_fts virtual table + sync triggers -----
-- content=notes uses the external-content optimization: notes_fts stores
-- only the tokenized form and points back into notes for the raw text.
-- The triggers replicate the insert/update/delete shape FTS5 needs to
-- keep the index consistent with the source rows.

CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
  title,
  body,
  content='notes',
  content_rowid='id',
  tokenize = "porter unicode61"
);

CREATE TRIGGER IF NOT EXISTS notes_fts_ai
AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, body)
  VALUES (NEW.id, NEW.title, NEW.body);
END;

CREATE TRIGGER IF NOT EXISTS notes_fts_ad
AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body)
  VALUES ('delete', OLD.id, OLD.title, OLD.body);
END;

CREATE TRIGGER IF NOT EXISTS notes_fts_au
AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body)
  VALUES ('delete', OLD.id, OLD.title, OLD.body);
  INSERT INTO notes_fts(rowid, title, body)
  VALUES (NEW.id, NEW.title, NEW.body);
END;

-- ----- unified search_index sync triggers (entity_type='note') -----
-- Mirrors the per-entity triggers migration 022 installs for tasks/
-- comments/errors/solutions/context_entries and migration 024 added for
-- plans. project_id 0 means "global note" so the cross-project filter
-- in search.go's `si.project_id = ?` path keeps global notes out of a
-- project-scoped search — matching the errors/solutions convention.

CREATE TRIGGER IF NOT EXISTS search_index_notes_ai
AFTER INSERT ON notes BEGIN
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.body, ''),
    'note',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_notes_au
AFTER UPDATE ON notes BEGIN
  DELETE FROM search_index WHERE entity_type = 'note' AND entity_id = OLD.id;
  INSERT INTO search_index(content, entity_type, entity_id, project_id)
  VALUES (
    COALESCE(NEW.title, '') || ' ' || COALESCE(NEW.body, ''),
    'note',
    NEW.id,
    COALESCE(NEW.project_id, 0)
  );
END;

CREATE TRIGGER IF NOT EXISTS search_index_notes_ad
AFTER DELETE ON notes BEGIN
  DELETE FROM search_index WHERE entity_type = 'note' AND entity_id = OLD.id;
END;
