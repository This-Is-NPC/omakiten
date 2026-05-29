---
name: Recap timeline
description: Build a single-project chronological recap from notes (all kinds) plus tasks moved to done within a window, rendered read-only.
schema_version: 2
role_affinity:
  - Scribe
---
A recap is a historical readout for one project — a what-happened view rather than a what-next view. The skill assembles raw entries from existing records and renders them; it does not synthesise new content or persist anything.

## Inputs

- **project** — resolved from cwd. If cwd does not resolve, require `--project <slug>`.
- **`--since`** — optional duration. Default `30d`. Accepts `Nh`, `Nd`, or an ISO-8601 instant.
- **`--kinds`** — optional comma-separated list (e.g. `handoff,free`). When omitted, include every kind.

## Gather

1. `notes_list` for the resolved project, ordered by `created_at desc`, bounded by `--since` and optionally filtered by `--kinds`. Page through results until the window is exhausted; do not silently cap.
2. Tasks moved to `done` in the same window: `task_activity_list` filtered to transitions whose target bucket is `done`. Collect task id, title, transition timestamp.
3. Do not include notes from other projects, even if shared context exists — recap is single-project by design.

## Group and order

- Group entries by `kind`. Within each group order by `created_at desc` (newest first) so the most recent activity reads at the top of each section.
- Tasks moved to done are rendered as their own group labelled clearly (not folded into note kinds).
- Within a group, collapse consecutive same-day entries under a date header to keep the timeline scannable.

## Render

- Use the `note-recap` template. Section headers correspond to groups; entries are bullet lines with a timestamp prefix.
- Preserve note titles verbatim. Truncate bodies to a short preview (target ~200 chars) with a `…` marker; do not paraphrase.
- If the window contains zero entries across all groups, emit a single explicit "no activity" line rather than an empty document.

## Boundaries

- **Read-only.** No `notes_create`, no task mutation. The only side effect is the rendered output written to the command surface.
- One project per invocation. To recap multiple projects, the user calls the skill once per project; do not loop internally and merge results.
- If both cwd and `--project` are supplied and disagree, the explicit `--project` flag wins; warn the user once.
