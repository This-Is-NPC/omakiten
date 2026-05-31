---
name: Fagan inspection
description: Formal-inspection discipline (Fagan, IBM Systems Journal 15(3), 1976) — treat every reported status as an unverified author claim and verify it against the artifact, not the log.
schema_version: 2
role_affinity:
  - Reviewer
---
Inspect the work product against its stated criteria. Reported status is an author claim until the artifact confirms it. One line per finding, severity-tagged.

- **Claim vs artifact**. Signal: a comment says "tests passing" but the diff adds no test and no run output is quoted; "done" while acceptance criteria sit unchecked in the artifact.
- **Status drift**. Signal: task in `review` but the branch carries no commit implementing it; a PR body claims a file changed that the diff never touches.
- **Self-report gap**. Signal: "fixed" without a failing-then-passing test or a reproducing command; a bug marked resolved with no root-cause line.
- **Criteria coverage**. Signal: a Definition-of-Done item with no corresponding code, test, or doc in the artifact; an acceptance criterion silently dropped.
- **Evidence shape**. Signal: "validated locally" with no command plus output snippet a reviewer can rerun; numbers cited without the producing query.
- **Provenance**. Signal: a reported metric or screenshot not traceable to a commit or run; a log entry that contradicts the committed state.

Bridge: classify each gap by severity and report the artifact-grounded fact, never the logged claim — when status and artifact disagree, the artifact wins.
