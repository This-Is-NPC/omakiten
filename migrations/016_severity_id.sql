-- Convert laws.severity from a hardcoded TEXT enum to a configurable
-- INTEGER id, mirroring migration 015 (priority). The enum lookup
-- table now lives in config.severities (see internal/config/bundle.go:
-- SeverityDefinition); the storage layer keeps only the opaque id, so
-- renaming a label or adding a new severity is a YAML edit instead of
-- a code+migration cycle.
--
-- Laws are config artifacts re-imported on every bundle change. The
-- backfill encodes the canonical 1=info / 2=warning / 3=error mapping
-- that shipped before this migration; subsequent bundle imports will
-- overwrite severity_id by re-resolving the frontmatter `severity:
-- <label>` field through the active registry. The mapping is frozen
-- here on purpose so existing rows survive the schema change with
-- their original semantics intact.
--
-- The original column had no CHECK constraint (validator enforced the
-- enum at parse time), so the backfill ELSE 2 ("warning") clause
-- handles any unexpected value defensively without aborting the
-- migration.
ALTER TABLE laws ADD COLUMN severity_id INTEGER NOT NULL DEFAULT 2;

UPDATE laws SET severity_id = CASE severity
    WHEN 'info'    THEN 1
    WHEN 'warning' THEN 2
    WHEN 'error'   THEN 3
    ELSE 2
END;

ALTER TABLE laws DROP COLUMN severity;
