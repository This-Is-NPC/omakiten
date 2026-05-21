---
name: Regression required
severity: error
---
Every bugfix ships with a regression test that pins the failure mode. The test fails before the fix and passes after; the diff makes both points reviewable in one read. Without the test, the same bug can come back silently — and reviewers have no anchor for "is this really fixed?".

Bad: panic on `nil` user object patched in 1 line, no test added; six weeks later a sibling code path regresses the same way.
Good: same fix paired with `TestHandlerHandlesNilUser` that reproduced the panic; `#tests-passing` shows the test went red → green.
