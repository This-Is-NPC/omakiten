---
name: Comment — Review findings
description: "Diff-walk findings emitted by okt-review; one row per finding, severity-tagged."
entity: comment
laws:
  - template-fidelity
  - findings-actionable
  - severity-tagged
---
**Base** — `<base ref>` (e.g. `main`)
**Head** — `<head ref>` (e.g. `HEAD`)
**Lens** — `<bugs | security | smells | scalability | mixed>`

| path:line | severity | problem | fix |
| --- | --- | --- | --- |
| `internal/x/y.go:42` | error | <one-line problem, name the methodology when applicable> | <one-line concrete fix, no "could be cleaner"> |

**Summary** — <count by severity, e.g. "2 error, 3 warning, 1 info">

**Next** — <`okt-implement` to apply, `okt-create` to spin a follow-up task, or `okt-review` to re-run after fixes>
