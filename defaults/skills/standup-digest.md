---
name: Standup digest
description: Aggregate recent handoff comments across projects within a time window, compute per-project deltas, and render the standup digest read-only.
schema_version: 2
role_affinity:
  - Scribe
  - Concierge
---
A standup digest is a multi-project snapshot for the start of a working session. The skill reads existing handoffs; it never writes them. If a project has no handoff in the window, the digest reports the gap rather than inventing one.

## Inputs

- **`--project`** — optional. Comma-separated project slugs. Default: every project the current user owns or participates in.
- **`--since`** — optional duration. Default `7d`. Accepts `Nh`, `Nd`, or an ISO-8601 instant.
- **`--limit`** — optional integer. Maximum handoffs to consider per project. Default `5`.

## Gather

For each project in the resolved set:

1. `comments_list` with `kind=handoff`, `scope=project`, `since` mapped from `--since`, ordered by `created_at desc`, capped by `--limit`.
2. If the project produced zero handoffs in the window, record it as a "silent project" — do not skip it.
3. For each handoff found, fetch the most recent handoff prior to the window (one extra `comments_list` with `kind=handoff`, `scope=project`, no `since`, taking the newest row before the window) so deltas can be computed.

## Compute deltas

For each project's newest handoff in the window, derive:

- **Tasks moved** since the prior handoff: query `task_activity_list` for bucket transitions in the interval `[prior_handoff.created_at, newest_handoff.created_at]`. Group by direction (e.g. `dev → review`, `review → done`).
- **New entries** since the prior handoff: `comments_list` with `scope=project` for the same interval across all kinds; report counts by kind.
- **Carry-overs**: tasks named in both handoffs that did not change bucket. Surface as a "still open" list.

## Render

- Use the `note-standup-digest` template. One section per project, ordered by most recent handoff first; silent projects appear at the bottom under a clear header.
- Each project section lists: latest handoff date, focus items, deltas, carry-overs.
- Keep the rendered output below ~200 lines; if it exceeds that, truncate the per-project bullet stacks (not the project list) and append a `(+N more)` marker.

## Boundaries

- **Read-only.** No `comments_add`, no task mutation, no comment writes.
- Output is emitted inline to the command surface. The user can pipe or capture it; the skill does not persist a copy.
- If `--project` names a slug that does not resolve, fail loudly with the list of unresolved slugs rather than silently dropping them.
