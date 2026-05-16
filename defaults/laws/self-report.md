---
name: Self-report
severity: error
---
Record any error that needed more than one fix attempt — the second attempt is the trigger. Call `errors.record` (one-line description, context, specific tags) and `solutions.add` against the returned id with the resolution that worked. Use `solutions.confirm` when applying a previously recorded solution found via `search(query, entity_types=["error"])`.

Bad: attempt 1 failed; attempt 2 worked — moved on without recording.
Good: before attempt 2, ran `search(query, entity_types=["error"])`; attempt 2 worked — `errors.record` with symptom and tags, then `solutions.add` with the resolution.

Single-attempt fixes don't require recording — keeps the log signal-rich.
