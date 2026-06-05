---
name: okt-note-show playbook
description: Read one knowledge note in full by id.
schema_version: 2
role_affinity:
  - Scribe
  - Concierge
---
Read one note in full. The note is fetched from the `comments.*` surface; this command is read-only by design.

## Resolve and render

Resolve the id from `--id` (or the first positional argument), call `comments.list` with `comment_id` set to that id (it returns exactly the one row, any scope), and render the note's title, kind, scope, tags, and body verbatim. Read-only — never mutate here; `comments.edit` / `comments.delete` are MCP-only by design.

## Handoff

Next: suggest `okt-note-list` to scan the surrounding notes, or `okt-task-continue` when the note points at an open task to resume.
