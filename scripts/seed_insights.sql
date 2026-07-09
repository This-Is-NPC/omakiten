-- scripts/seed_insights.sql — canonical dev fixture for the Insights intelligence layer.
--
-- Self-enforcing abort: `.bail on` (a sqlite3 CLI dot-command) makes the CLI
-- stop on the FIRST failed statement, so the sandbox CHECK guard below aborts
-- the run without relying on a `-bail` invocation flag. It is a no-op in
-- non-sqlite3 runners, which is acceptable — the guard is defence in depth for
-- the interactive path the RUN note documents.
.bail on
--
-- Representative agent-event history that lights up all six "today-computable" insights.
-- Every inserted row is tagged source='seed' so the fixture is fully reversible.
--
-- ── TARGET DB ────────────────────────────────────────────────────────────────
-- Run ONLY against the disposable sandbox at  dev_env/data/omakiten.db
-- NEVER run against the real installed db at  ~/.local/share/omakiten/  (or any
-- $XDG_DATA_HOME/omakiten path). This fixture rewrites tasks 1-5 and 20-24 and is
-- meant for a throwaway dev_env, not your live board.
--
-- ── RUN ──────────────────────────────────────────────────────────────────────
--   sqlite3 dev_env/data/omakiten.db < scripts/seed_insights.sql
-- The `.bail on` directive at the top of this script makes the sandbox CHECK
-- guard self-enforcing, so no `-bail` invocation flag is required.
-- Re-running is safe: the script first deletes any prior seed rows, so it is
-- idempotent (run it as many times as you like; the seed set is identical).
--
-- ── CLEAN ────────────────────────────────────────────────────────────────────
-- Remove every seeded row (leaves your other dev_env data untouched):
--   sqlite3 dev_env/data/omakiten.db "DELETE FROM events    WHERE source='seed';"
--   sqlite3 dev_env/data/omakiten.db "DELETE FROM solutions WHERE source='seed';"
--   sqlite3 dev_env/data/omakiten.db "DELETE FROM errors    WHERE source='seed';"
-- (Note: the UPDATE statements below mutate existing tasks 1-5 / 20-24 in place;
--  those board-state edits are not source-tagged. Reset them by re-seeding a
--  fresh dev_env or by reapplying your own board state.)
--
-- ── CONTENT ──────────────────────────────────────────────────────────────────
-- Lights up all 6 insights: stuck tasks, cycle time + bottleneck/bucket, WIP/bucket,
-- guard hotspot, error loop, and per-model contrast across 3 distinct agent_model
-- values: 'claude-opus-4-8', 'claude-sonnet-4-6', 'openai/gpt-5.5'.
-- buckets: 1=backlog 2=dev 3=review 4=done. project_id=1 slug='omakiten'.
-- tasks.state CHECK allows only ('active','archived'); "done" = bucket_id=4 + completed_at.

BEGIN;

-- ── SANDBOX GUARD ─────────────────────────────────────────────────────────────
-- Abort unless this database is the single-project dev_env sandbox. The real
-- installed db registers many projects; the disposable dev_env registers
-- exactly one. The CHECK constraint fails the INSERT on a multi-project db,
-- and `sqlite3 -bail` (see RUN above) stops the script before any mutation.
CREATE TEMP TABLE seed_guard (ok INTEGER NOT NULL CHECK (ok = 1));
INSERT INTO seed_guard
SELECT CASE WHEN (SELECT COUNT(*) FROM projects) <= 1 THEN 1 ELSE 0 END;
DROP TABLE seed_guard;

-- Idempotent: clear any prior seed run first.
DELETE FROM events    WHERE source='seed';
DELETE FROM solutions WHERE source='seed';
DELETE FROM errors    WHERE source='seed';

-- ── Current board state (drives WIP/bucket + stuck = current bucket) ──────────
-- NOTE: state CHECK allows only ('active','archived'); "done" = bucket_id=4 + completed_at.
UPDATE tasks SET bucket_id=4, completed_at=datetime('now','-23 days') WHERE id=20;
UPDATE tasks SET bucket_id=4, completed_at=datetime('now','-5 days')  WHERE id=21;
UPDATE tasks SET bucket_id=2, state='active' WHERE id IN (22,23,24);
UPDATE tasks SET bucket_id=3, state='active' WHERE id IN (1,2,3,4,5);

-- ── task.moved history (cycle time, bottleneck, stuck, rework) ────────────────
-- helper shape: entity_type, entity_id, project_id, project_slug, event_type, body, payload, author_type, source, created_at, agent_model, agent_session_id
INSERT INTO events (entity_type,entity_id,project_id,project_slug,event_type,body,payload,author_type,source,created_at,agent_model,agent_session_id) VALUES
-- task 20: fast clean cycle (opus) backlog->dev->review->done
('task',20,1,'omakiten','task.moved','','{"from":"backlog","to":"dev"}','agent','seed',datetime('now','-25 days'),'claude-opus-4-8','sess-opus-a'),
('task',20,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-24 days'),'claude-opus-4-8','sess-opus-a'),
('task',20,1,'omakiten','task.moved','','{"from":"review","to":"done"}','agent','seed',datetime('now','-23 days'),'claude-opus-4-8','sess-opus-a'),
-- task 21: review bottleneck (sonnet) 14d dwell in review
('task',21,1,'omakiten','task.moved','','{"from":"backlog","to":"dev"}','agent','seed',datetime('now','-20 days'),'claude-sonnet-4-6','sess-son-a'),
('task',21,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-19 days'),'claude-sonnet-4-6','sess-son-a'),
('task',21,1,'omakiten','task.moved','','{"from":"review","to":"done"}','agent','seed',datetime('now','-5 days'),'claude-sonnet-4-6','sess-son-a'),
-- task 24: rework loop (sonnet) dev->review->dev->(stays dev)
('task',24,1,'omakiten','task.moved','','{"from":"backlog","to":"dev"}','agent','seed',datetime('now','-12 days'),'claude-sonnet-4-6','sess-son-b'),
('task',24,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-10 days'),'claude-sonnet-4-6','sess-son-b'),
('task',24,1,'omakiten','task.moved','','{"from":"review","to":"dev"}','agent','seed',datetime('now','-8 days'),'claude-sonnet-4-6','sess-son-b'),
-- task 23: normal WIP in dev (gpt)
('task',23,1,'omakiten','task.moved','','{"from":"backlog","to":"dev"}','agent','seed',datetime('now','-3 days'),'openai/gpt-5.5','sess-gpt-a'),
-- task 22: STUCK in dev 18d (gpt)
('task',22,1,'omakiten','task.moved','','{"from":"backlog","to":"dev"}','agent','seed',datetime('now','-18 days'),'openai/gpt-5.5','sess-gpt-b'),
-- task 3: STUCK in review 15d (opus)
('task',3,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-15 days'),'claude-opus-4-8','sess-opus-b'),
-- review WIP fillers
('task',1,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-6 days'),'claude-opus-4-8','sess-opus-c'),
('task',2,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-7 days'),'openai/gpt-5.5','sess-gpt-c'),
('task',4,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-4 days'),'claude-opus-4-8','sess-opus-d'),
('task',5,1,'omakiten','task.moved','','{"from":"dev","to":"review"}','agent','seed',datetime('now','-2 days'),'claude-sonnet-4-6','sess-son-c');

-- ── guard.violated (guard hotspot by rule/tag; self-branch = #1) ──────────────
INSERT INTO events (entity_type,entity_id,project_id,project_slug,event_type,body,payload,author_type,source,created_at,agent_model,agent_session_id) VALUES
('task',22,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"self-branch","operation":"task.transition","hint":"task has 0 comment(s) tagged self-branch","target":{"to_bucket":"dev"}}','agent','seed',datetime('now','-18 days'),'openai/gpt-5.5','sess-gpt-b'),
('task',23,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"self-branch","operation":"task.transition","hint":"task has 0 comment(s) tagged self-branch","target":{"to_bucket":"dev"}}','agent','seed',datetime('now','-3 days'),'openai/gpt-5.5','sess-gpt-a'),
('task',24,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"self-branch","operation":"task.transition","hint":"task has 0 comment(s) tagged self-branch","target":{"to_bucket":"dev"}}','agent','seed',datetime('now','-12 days'),'claude-sonnet-4-6','sess-son-b'),
('task',1,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"self-branch","operation":"task.transition","hint":"task has 0 comment(s) tagged self-branch","target":{"to_bucket":"dev"}}','agent','seed',datetime('now','-9 days'),'claude-sonnet-4-6','sess-son-d'),
('task',2,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"self-branch","operation":"task.transition","hint":"task has 0 comment(s) tagged self-branch","target":{"to_bucket":"dev"}}','agent','seed',datetime('now','-7 days'),'openai/gpt-5.5','sess-gpt-c'),
('task',3,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"self-branch","operation":"task.transition","hint":"task has 0 comment(s) tagged self-branch","target":{"to_bucket":"dev"}}','agent','seed',datetime('now','-16 days'),'claude-opus-4-8','sess-opus-b'),
-- documentation tag (rule comments_tagged) x3
('task',20,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"documentation","operation":"task.transition","hint":"task has 0 comment(s) tagged documentation","target":{"to_bucket":"done"}}','agent','seed',datetime('now','-23 days'),'claude-opus-4-8','sess-opus-a'),
('task',21,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"documentation","operation":"task.transition","hint":"task has 0 comment(s) tagged documentation","target":{"to_bucket":"done"}}','agent','seed',datetime('now','-5 days'),'claude-sonnet-4-6','sess-son-a'),
('task',4,1,'omakiten','guard.violated','','{"rule":"comments_tagged","tag":"documentation","operation":"task.transition","hint":"task has 0 comment(s) tagged documentation","target":{"to_bucket":"done"}}','agent','seed',datetime('now','-4 days'),'claude-opus-4-8','sess-opus-d'),
-- workflow_transition rule x2
('task',5,1,'omakiten','guard.violated','','{"rule":"workflow_transition","tag":"invalid-move","operation":"task.transition","hint":"transition review->backlog not allowed","target":{"to_bucket":"backlog"}}','agent','seed',datetime('now','-2 days'),'claude-sonnet-4-6','sess-son-c'),
('task',23,1,'omakiten','guard.violated','','{"rule":"workflow_transition","tag":"invalid-move","operation":"task.transition","hint":"transition dev->done not allowed","target":{"to_bucket":"done"}}','agent','seed',datetime('now','-3 days'),'openai/gpt-5.5','sess-gpt-a');

-- ── errors + solutions (error loop: 2 of 5 resolved) ─────────────────────────
INSERT INTO errors (description,context,project_id,created_at,source,entrypoint,agent_model,agent_session_id) VALUES
('guard self-branch repeatedly blocks dev transition','task.transition',1,datetime('now','-16 days'),'seed','mcp','claude-opus-4-8','sess-opus-b'),
('realtime-tick test flaky under load','test',1,datetime('now','-11 days'),'seed','cli','claude-sonnet-4-6','sess-son-b'),
('i18n parity test fails: missing key in 3 locale packs','test',1,datetime('now','-9 days'),'seed','cli','openai/gpt-5.5','sess-gpt-c'),
('sqlite database is locked during nested tx','runtime',1,datetime('now','-6 days'),'seed','mcp','claude-opus-4-8','sess-opus-c'),
('MCP tool input schema mismatch on insights.summary draft','runtime',1,datetime('now','-2 days'),'seed','mcp','claude-sonnet-4-6','sess-son-c');

-- solutions: confirm 2 (success + likes), leave 3 unresolved
INSERT INTO solutions (error_id,description,steps,success,task_id,tried_at,created_at,likes,source,entrypoint,agent_model,agent_session_id)
SELECT id,'create dedicated branch + tag comment #self-branch before move','git switch -c feat/x; comment tag self-branch',1,22,datetime('now','-15 days'),datetime('now','-15 days'),2,'seed','mcp','claude-opus-4-8','sess-opus-b' FROM errors WHERE source='seed' AND description LIKE 'guard self-branch%';
INSERT INTO solutions (error_id,description,steps,success,task_id,tried_at,created_at,likes,source,entrypoint,agent_model,agent_session_id)
SELECT id,'add missing key to all 21 packs; parity test green','populate en + 20 locales',1,9,datetime('now','-8 days'),datetime('now','-8 days'),1,'seed','cli','openai/gpt-5.5','sess-gpt-c' FROM errors WHERE source='seed' AND description LIKE 'i18n parity%';
INSERT INTO solutions (error_id,description,steps,success,task_id,tried_at,created_at,likes,source,entrypoint,agent_model,agent_session_id)
SELECT id,'increase tick debounce — did not hold','bump debounce 50ms',0,NULL,datetime('now','-10 days'),datetime('now','-10 days'),0,'seed','cli','claude-sonnet-4-6','sess-son-b' FROM errors WHERE source='seed' AND description LIKE 'realtime-tick%';

-- a few error/solution lifecycle events too (event-shaped readers)
INSERT INTO events (entity_type,entity_id,project_id,project_slug,event_type,body,payload,author_type,source,created_at,agent_model,agent_session_id) VALUES
('error',0,1,'omakiten','error.recorded','guard self-branch repeatedly blocks dev transition','{"context":"task.transition"}','agent','seed',datetime('now','-16 days'),'claude-opus-4-8','sess-opus-b'),
('error',0,1,'omakiten','solution.confirmed','create dedicated branch before move','{"success":true}','agent','seed',datetime('now','-15 days'),'claude-opus-4-8','sess-opus-b'),
('error',0,1,'omakiten','error.recorded','i18n parity test fails','{"context":"test"}','agent','seed',datetime('now','-9 days'),'openai/gpt-5.5','sess-gpt-c'),
('error',0,1,'omakiten','solution.confirmed','add missing key to all packs','{"success":true}','agent','seed',datetime('now','-8 days'),'openai/gpt-5.5','sess-gpt-c');

COMMIT;
