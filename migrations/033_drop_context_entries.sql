-- 033_drop_context_entries.sql — drop the context_entries table and retire
-- its search path (A4, umbrella context-entry removal).
--
-- The Go service/store/domain layer for context entries was already deleted
-- upstream; the only things still tied to the physical table were its
-- search_index mirror triggers (migration 022, recreated verbatim by the
-- table rebuild in migration 025) and the 'context' rows they keep in the
-- unified FTS index. This migration retires both, then drops the table.
--
-- Hard drop: any existing context_entries rows are discarded (confirmed
-- acceptable — no backfill).
--
-- Order matters: drop the search_index triggers first (they fire on
-- context_entries, so DROP TABLE would otherwise leave them dangling), then
-- purge the 'context' rows already mirrored into search_index, then drop the
-- base table itself.

-- ----- 1. drop the search_index mirror triggers (022 / recreated by 025) -----

DROP TRIGGER IF EXISTS search_index_context_entries_ai;
DROP TRIGGER IF EXISTS search_index_context_entries_au;
DROP TRIGGER IF EXISTS search_index_context_entries_ad;

-- ----- 2. purge mirrored 'context' rows from the unified FTS index -----

DELETE FROM search_index WHERE entity_type = 'context';

-- ----- 3. drop the base table -----

DROP TABLE IF EXISTS context_entries;
