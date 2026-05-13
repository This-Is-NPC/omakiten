---
name: Coverage gate
severity: error
---
Test coverage must not drop. New behavioral code targets ≥80% line coverage on the affected packages. Exact numbers (delta + absolute) appear in the `#tests-passing` comment. Exemptions require a documented rationale signed by a reviewer.

Bad: shipped a 500-LOC feature that dropped package coverage from 78% to 71% with no comment about it.
Good: `#tests-passing` reports `cmd/foo coverage: 84.2% (+1.1%); internal/foo coverage: 91.5% (-0.0%)` with the `go test -coverprofile` command quoted.
