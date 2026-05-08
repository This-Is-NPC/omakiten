-- Per-workflow defaults block for task/comment edit/delete policy. Lives at
-- the workflow level (not the bucket level) because it expresses the
-- fallback applied when a bucket does not declare its own override. Stored
-- as JSON for parity with operations_json so the schema stays open to
-- richer fields without another migration.
ALTER TABLE workflows ADD COLUMN defaults_json TEXT NOT NULL DEFAULT '{}';
