---
name: Note capture
description: Validate user-stated note inputs, resolve scope from cwd or explicit flag, and persist a free-form note via notes_create.
---
Capture is a one-shot write. The skill does not interpret or rephrase the body — it normalises inputs, validates them, and commits the note exactly as stated.

## Inputs

Accept from invocation args or a follow-up prompt:

- **body** — required, non-empty after trimming. The captured content verbatim.
- **title** — required, non-empty after trimming. Short noun phrase, no terminal period.
- **kind** — optional. Defaults to `free`. Other valid kinds (`handoff`, `standup-digest`, `recap`) are owned by their dedicated skills; reject them here so capture stays single-purpose.
- **scope** — optional. `project` or `global`. Resolution rules below.

## Scope resolution

1. If `--scope global`, scope is `global` regardless of cwd.
2. Otherwise resolve the active project from cwd. If a project resolves, scope is `project`.
3. If no project resolves and no explicit scope was given, abort with a validation error asking the user to pass `--scope global` or run from inside a registered project. Never silently fall back.

## Validation

- Trim leading/trailing whitespace on title and body before length checks.
- Reject empty title, empty body, or `kind` outside `{free}`.
- Reject mixed scope/project hints (e.g. `--scope global` while resolving a project) — the explicit flag wins; warn the user once if cwd implied a different scope.

## Persist

- Call `notes_create` with the resolved `scope`, `kind=free` (or whatever the user passed once allow-listed), `title`, and `body`.
- Surface the new note id to the user. Do not echo the full body back; the user already has it.

## Boundaries

- No template rendering. Capture writes raw content.
- No follow-up edits, tagging, or linking. Subsequent operations are separate intents.
- One write per invocation. If the user supplies a list, prompt them to pick one or split the call.
