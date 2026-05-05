ALTER TABLE solutions ADD COLUMN likes INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_solutions_likes ON solutions(likes DESC);
