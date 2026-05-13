---
name: Test evidence
severity: error
---
Behavioral changes ship with reproducible test evidence: a failing-then-passing test added in the same diff (TDD), or a `#tests-passing` comment with the test command, an output snippet, and a duration. "I tested locally" without an artifact the reviewer can rerun is not evidence.

Bad: opened a PR fixing a race condition with no test; review notes say "trust me, I confirmed it manually."
Good: PR adds `TestFooRace` (initially red) and the fix that makes it green; `#tests-passing` quotes the `go test -race` output.
