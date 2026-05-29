---
name: Requirements mapping
description: Extracts functional, non-functional, and business rules with source-file references.
schema_version: 2
role_affinity:
  - Scribe
  - Reviewer
---
Requirements mapping reconstructs the implemented requirement set from the code itself, producing a traceable index of functional requirements, non-functional requirements, and business rules. Every entry cites the source file (and ideally the symbol) that implements it, so the map is verifiable rather than asserted.

## What to extract

- **Functional requirements** — observable behaviours the system provides. Trace each to the handler, service, or command that realises it.
- **Non-functional requirements** — quality attributes enforced in code: rate limits, timeouts, retry policy, caching, auth checks. Cite the guard or middleware.
- **Business rules** — domain invariants and constraints (validation thresholds, state-transition guards, pricing logic). These are the rules that are expensive to rediscover; pin each to the function that enforces it.

## Method

Survey by capability, not by file: start from an externally observable behaviour and follow it inward to the code that implements it, recording the path. Group findings by category. Where a requirement is partially implemented or guarded by a TODO, mark it as such rather than reporting it as complete — an honest gap is more useful than an optimistic claim.

## Traceability

Each mapped requirement is a claim about the code; a claim without a file reference is not yet verified. Keep references at the path level at minimum so a reader can open the file and confirm. When behaviour spans several files, list the entry point and the key collaborators.

## Boundaries

Mapping documents what *is* implemented, not what *should* be. It does not judge correctness or completeness against an external spec — that is a review or audit task. Hand the map to the documentation or review phase as the factual baseline.
