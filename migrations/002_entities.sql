ALTER TABLE personas ADD COLUMN description TEXT NOT NULL DEFAULT '';

ALTER TABLE skills ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE skills ADD COLUMN body TEXT NOT NULL DEFAULT '';
ALTER TABLE skills ADD COLUMN source_path TEXT NOT NULL DEFAULT '';

ALTER TABLE laws ADD COLUMN scope TEXT NOT NULL DEFAULT 'global' CHECK (scope IN ('global', 'project', 'persona'));
ALTER TABLE laws ADD COLUMN project_id INTEGER REFERENCES projects(id);
ALTER TABLE laws ADD COLUMN persona_id INTEGER REFERENCES personas(id);

ALTER TABLE tasks ADD COLUMN workdir TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN branch TEXT NOT NULL DEFAULT '';

ALTER TABLE projects ADD COLUMN description TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_laws_scope ON laws(scope, project_id, persona_id);
