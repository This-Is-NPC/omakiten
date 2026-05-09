-- Convert tasks.priority from a hardcoded TEXT enum to a configurable
-- INTEGER id. The enum lookup table now lives in config.priorities (see
-- internal/config/bundle.go:PriorityDefinition); the storage layer keeps
-- only the opaque id, so renaming a label or adding a new priority is a
-- YAML edit instead of a code+migration cycle.
--
-- Backfill maps the canonical 1/2/3 → low/normal/high values that
-- shipped before this migration. The mapping is encoded here (rather
-- than read from config) on purpose: at the moment this migration ran,
-- the system literally used those three values. Encoding them in the
-- migration freezes that truth — later edits to config.priorities can
-- rename the labels but the integer ids on the existing rows continue
-- to mean what they meant at write time.
--
-- The CHECK constraint on the original column also forced exactly those
-- three values, so any pre-migration row matches one of the three
-- branches; the ELSE 2 ("normal") clause is defensive only.
ALTER TABLE tasks ADD COLUMN priority_id INTEGER NOT NULL DEFAULT 2;

UPDATE tasks SET priority_id = CASE priority
    WHEN 'low'    THEN 1
    WHEN 'normal' THEN 2
    WHEN 'high'   THEN 3
    ELSE 2
END;

ALTER TABLE tasks DROP COLUMN priority;
