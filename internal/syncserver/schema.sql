CREATE TABLE IF NOT EXISTS projects (
	uid         TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	slug        TEXT NOT NULL,
	description TEXT NOT NULL,
	kit_key     TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rows (
	project_uid TEXT NOT NULL REFERENCES projects(uid),
	tbl         TEXT NOT NULL,
	row_uid     TEXT NOT NULL,
	op          TEXT NOT NULL,
	hlc         TEXT NOT NULL,
	device      TEXT NOT NULL,
	data        TEXT NOT NULL,
	seq         INTEGER NOT NULL UNIQUE,
	PRIMARY KEY (project_uid, tbl, row_uid)
);
CREATE INDEX IF NOT EXISTS rows_project_seq ON rows(project_uid, seq);
