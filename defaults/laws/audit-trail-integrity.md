---
name: Audit trail integrity
severity: error
---
Comments published in dev or beyond are append-only. No delete. Corrections happen via a new `#scribe-correction` comment that names the assertion being corrected, the corrected text, and the reason. The original stays in place — the trail must survive review.

Bad: deleted a `#peer-review` comment because the reviewer "changed their mind"; the trail no longer shows the original verdict.
Good: filed a new `#scribe-correction` referencing the original; both comments stay; the reviewer's evolution is auditable.
