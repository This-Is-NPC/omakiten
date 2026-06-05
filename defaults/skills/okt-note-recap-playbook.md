---
name: okt-note-recap playbook
description: Recap timeline of recent notes; wide window folds in the cross-project handoff digest.
schema_version: 2
role_affinity:
  - Scribe
  - Concierge
---
Render a recap timeline of recent activity, scoped by the window argument. The recap reads through `comments.*`; it is the artifact and is never persisted.

## Single-project window

With a single-project window (the default — project from `--project <slug>` or cwd) it groups recent notes chronologically: resolve the window from `[janela]` / `--since` (default `7d`) and the kind filter from `--kinds <comma-list>` (default all), call `comments.list` with `scope=project` for the project filtered by `since` (the window) and `kind`, then `templates.show note-recap` to fetch the scaffold. Group entries by kind and order them chronologically with a timestamp prefix per bullet.

## Wide window folds in the digest

With a wide window (`okt-note-recap day`, or any cross-project invocation where `--project` is omitted) it folds in the former standup digest: enumerate every project the user owns, call `comments.list` with `scope=project` per project filtered by `kind=handoff` and `since` (the window; per-project limit from `--limit`, default `5`), then `templates.show note-standup-digest` and fill one section per project ordered by most recent handoff first, silent projects last under a clear header. When more than 50 projects resolve, paginate or require `--project`.

## Read-only, always

Read-only either way — never persist; the recap is the artifact. When zero notes/handoffs match the window, surface "nothing in window" (or "no handoffs — run okt-pause" for the cross-project case).

## Handoff

Next: suggest `okt-task-continue` with a specific task id when the recap reveals an open thread to resume.
