---
name: Acceptance criteria writing
description: Testable acceptance shapes (Given/When/Then or alternatives); criteria the requester and reviewer can verify.
schema_version: 2
role_affinity:
  - Owner
  - Tester
  - Reviewer
---
Acceptance criteria are the contract that decides whether work is done. Each criterion must be verifiable by someone other than its author; if the requester and the reviewer cannot independently agree on pass or fail, the criterion is not yet written.

## Given/When/Then

The behavioural default is the Given/When/Then form (from Behaviour-Driven Development; North, 2006):

- **Given** the precondition or system state,
- **When** the action or event occurs,
- **Then** the observable, checkable outcome.

Keep each scenario to a single behaviour. A criterion with two *Then* clauses joined by "and" is usually two criteria.

## Alternative shapes

Not every criterion is behavioural. Use a checklist form for completeness criteria ("the README documents the new flag"), a threshold form for quality attributes ("p95 latency under 200ms"), and a rule form for invariants ("balance never goes negative"). Match the shape to what is being asserted rather than forcing everything into Given/When/Then.

## Tests of a good criterion

- **Observable** — states an outcome that can be seen, measured, or queried, not an internal intention.
- **Bounded** — names the specific case, not "works correctly".
- **Falsifiable** — a concrete input could make it fail.
- **Owned** — tied to a story or task so its scope is clear.

## Boundaries

Criteria define *what* done means, not *how* to build it. Avoid prescribing implementation in a criterion. Hand the criteria to the tester to encode and to the reviewer to check; they are the shared acceptance contract across both.
