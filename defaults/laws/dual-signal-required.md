---
name: Dual signal required
severity: warning
---
A green test run alone is not a green check — lint must also be clean (and any other configured static-analysis target). High-rigor presets require both signals before promotion past the check phase; one signal hides bugs the other catches (e.g. dead code, ineffective assignments, shadowed variables that tests happen to skip).

Bad: tests pass, lint surfaces three unused variables; promoted on test signal alone; one of the unused vars was a guard that was wired to the wrong branch.
Good: `#check-report` shows `test: pass` and `lint: pass`; both quoted with command + tail.
