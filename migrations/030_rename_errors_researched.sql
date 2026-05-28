-- Phase 4 of event registry refactor (task #356).
-- Rename historical 'error.searched' rows to the new event_type
-- 'errors.researched' and shift their entity_type from 'error' to
-- 'search' to match the semantic ("agent researched existing errors
-- before recording a new one"). Idempotent: a second run finds no
-- rows because the new event_type does not match the WHERE clause.

UPDATE events
SET event_type = 'errors.researched', entity_type = 'search'
WHERE event_type = 'error.searched';
