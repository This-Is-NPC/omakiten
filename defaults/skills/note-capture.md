---
name: Note capture
description: Validate user-stated note inputs, resolve scope from cwd or explicit flag, and persist a free-form note via comments_add.
schema_version: 2
role_affinity:
  - Scribe
---
Capture is a one-shot write. The skill does not interpret or rephrase the body — it normalises inputs, validates them, and commits the comment exactly as stated.

## Inputs

Accept from invocation args or a follow-up prompt:

- **body** — required, non-empty after trimming. The captured content verbatim.
- **title** — required, non-empty after trimming. Short noun phrase, no terminal period.
- **kind** — optional. Defaults to `free`. The kind `handoff` is owned by `handoff-synthesis` — reject it here so capture stays single-purpose.
- **scope** — optional. `project` or `global` (persisted as the `universal` comment scope). Resolution rules below.

## Scope resolution

1. If `--scope global`, scope is `global` regardless of cwd.
2. Otherwise resolve the active project from cwd. If a project resolves, scope is `project`.
3. If no project resolves and no explicit scope was given, abort with a validation error asking the user to pass `--scope global` or run from inside a registered project. Never silently fall back.

## Validation

- Trim leading/trailing whitespace on title and body before length checks.
- Reject empty title, empty body, or `kind` outside `{free}`.
- Reject mixed scope/project hints (e.g. `--scope global` while resolving a project) — the explicit flag wins; warn the user once if cwd implied a different scope.

## Persist

- Call `comments_add` with the resolved scope (`project`, or `universal` for a `global` capture), `kind=free` (or whatever the user passed once allow-listed), `title`, and `body`. Omit `task_id` — captures hang on the project or universally, never on a task.
- Surface the new comment id to the user. Do not echo the full body back; the user already has it.

## Boundaries

- No template rendering. Capture writes raw content.
- No follow-up edits, tagging, or linking. Subsequent operations are separate intents.
- One write per invocation. If the user supplies a list, prompt them to pick one or split the call.
