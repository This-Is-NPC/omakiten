ALTER TABLE workflow_transitions ADD COLUMN guards_json TEXT NOT NULL DEFAULT '[]';
