---
name: Regression test for bug fix
severity: error
---
Every bug fix ships with a test that reproduces the bug. The test fails on the pre-fix code and passes on the fixed code, so the diff makes both states reviewable in one read. The test pins the specific failure mode — input, state, and expected behaviour — not a vague "it works now". Without it, the same defect can return silently and reviewers have no anchor for "is this actually fixed?".

Bad: an off-by-one in pagination patched in one line, no test; the next refactor reintroduced it and shipped to users.
Good: the fix is paired with `TestPaginationIncludesLastPage`, which reproduced the missing row before the change and passes after — the regression is now permanently guarded.

See Beck, *Test-Driven Development: By Example*, on writing the failing test first; and Fowler, *Refactoring* (2nd ed.) §5 on tests as the safety net for change.
