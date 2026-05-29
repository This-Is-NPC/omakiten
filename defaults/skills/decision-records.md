---
name: Decision records
description: When to record a decision; concise context / decision / consequences; discoverable filenames and links.
schema_version: 2
role_affinity:
  - Scribe
  - Owner
---
An Architecture Decision Record (ADR; Nygard, *Documenting Architecture Decisions*, 2011) captures a single significant decision and the reasoning behind it, so future readers understand *why* the system is the way it is rather than reverse-engineering intent from the code.

## When to record

Record a decision when it is consequential and not obvious: a choice with real alternatives, a constraint that shaped the design, a trade-off that someone will later question. Do not record trivial or easily-reversed choices — an ADR log diluted with noise stops being read. The test is whether a future maintainer would ask "why was this done this way?"

## Structure

Keep each record short and to a fixed shape (Nygard's form):

- **Context** — the forces in play: the problem, the constraints, what was true at the time.
- **Decision** — the choice made, stated plainly in the active voice.
- **Consequences** — what follows, both the benefits gained and the costs and limitations accepted. The honest cost section is what makes the record trustworthy.

Add a status (proposed / accepted / superseded) so the log shows the lineage when a later decision overrides an earlier one.

## Discoverability

Name files so they sort and search well — a numbered, dated, slug form (`0007-use-sqlite-for-local-state.md`) keeps the log ordered and the title scannable. Link related records and link the ADR from the code or doc it governs, so a reader landing on either finds the other.

## Boundaries

A decision record captures *one* decision's rationale; it is not a design doc (the full approach) nor living architecture documentation (the as-built state). Supersede rather than edit an accepted record when the decision changes, preserving the history of what was once true.
