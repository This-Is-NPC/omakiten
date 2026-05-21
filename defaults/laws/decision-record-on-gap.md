---
name: Decision record on gap
severity: warning
---
When a check surfaces a known, accepted gap (uncovered branch, waived AC, deferred hardening), it is recorded — once — as a decision record (`docs/decisions/<NNNN>-<title>.md` or the repo's equivalent) before promotion. The check report then links the record instead of restating the rationale each run. Without the record, the gap drifts from "intentional" to "forgotten" the next time anyone re-runs the check.

Bad: coverage report shows `internal/audit: 0%`; PR description says "audit path is integration-tested in staging" and the next reviewer has nothing to cross-check.
Good: same gap, but `docs/decisions/0087-audit-coverage-in-staging.md` is filed and `#check-report` quotes the link.
