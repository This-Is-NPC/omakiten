CREATE TABLE IF NOT EXISTS comment_tags (
  comment_id INTEGER NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (comment_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_comment_tags_tag ON comment_tags(tag_id);
