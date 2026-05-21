---
name: Design recorded check
severity: warning
---
Reviewer verifies that significant design choices visible in the diff trace back to a recorded design doc / decision record / RFC. Choices made implicitly in code — new dependency, replaced component, deviation from precedent — are findings.

Bad: PR swaps `database/sql` for an ORM with no design doc; reviewer approves; six months later no one remembers why.
Good: "`internal/db/`: swap to `ent` is not in `docs/decisions/`. File a record before merge per `decision-record-on-divergence`."
