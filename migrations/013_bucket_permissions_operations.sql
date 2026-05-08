-- Per-bucket CRUD policy for tasks and comments, plus per-workflow operation
-- guards (archive/delete/unarchive). Both columns store JSON so the schema
-- stays open to richer policy without further migrations.
ALTER TABLE workflow_buckets ADD COLUMN permissions_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE workflows        ADD COLUMN operations_json  TEXT NOT NULL DEFAULT '{}';
