-- Recovery for DBs that applied migration 020 before the
-- tasks.bucket_id rebind step was added to it. Those DBs lost
-- workflow_buckets without first rewriting tasks.bucket_id from the
-- SQL-era PK to the bundle-declared local_id, leaving every task
-- pointing at an integer the post-020 Snapshot does not recognise.
-- Tasks vanish from every view because Snapshot.BucketByID returns
-- ok=false for the orphaned id.
--
-- Pure-SQL recovery path (no Go code, no manual fix expected):
--
-- 1. For each task whose bucket_id sits above the canonical YAML
--    range (>1000 is well above every preset's bucket count — the
--    YAML presets use ids 1-6), scan the events table for the last
--    `task.moved` event and fall back to the `task.created` event.
--    Both payloads carry the bucket key the task last lived under.
--
-- 2. Map that key onto the canonical bucket id for the default
--    `omakase` preset (1=backlog, 2=dev, 3=review, 4=done). Other
--    shipped presets share most key names (`requirements`,
--    `planning`, `docs`) so the mapping covers them at the cost of
--    a slight offset for `dev` / `done` in non-omakase workflows;
--    surviving offsets surface through the standard orphan-migration
--    flow on the next workflow rebuild.
--
-- 3. Tasks with no recoverable key (no events) land in bucket id 1
--    (the universal first-bucket convention across every shipped
--    preset). Users reorganise via TUI / CLI after the binary reopens
--    successfully.
--
-- Migration is idempotent. On fresh installs and DBs that ran the
-- rebind-aware shape of migration 020, bucket_id is already in the
-- canonical YAML range; the WHERE clause matches no rows and the
-- migration is a no-op.

CREATE TEMPORARY TABLE _task_recovery_keys AS
SELECT t.id AS task_id,
       COALESCE(
         (SELECT json_extract(e.payload, '$.to')
            FROM events e
           WHERE e.entity_type = 'task'
             AND e.entity_id   = t.id
             AND e.event_type  = 'task.moved'
           ORDER BY e.id DESC
           LIMIT 1),
         (SELECT json_extract(e.payload, '$.bucket')
            FROM events e
           WHERE e.entity_type = 'task'
             AND e.entity_id   = t.id
             AND e.event_type  = 'task.created'
           ORDER BY e.id DESC
           LIMIT 1)
       ) AS bucket_key
  FROM tasks t
 WHERE t.bucket_id > 1000;

UPDATE tasks
   SET bucket_id = CASE (SELECT bucket_key FROM _task_recovery_keys WHERE task_id = tasks.id)
                       WHEN 'backlog'      THEN 1
                       WHEN 'requirements' THEN 1
                       WHEN 'planning'     THEN 2
                       WHEN 'dev'          THEN 2
                       WHEN 'review'       THEN 3
                       WHEN 'docs'         THEN 5
                       WHEN 'done'         THEN 4
                       ELSE 1
                   END
 WHERE bucket_id > 1000;

DROP TABLE _task_recovery_keys;
