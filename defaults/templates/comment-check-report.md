---
name: Comment — Check report
description: "Tabular pass/fail report emitted by okt-task-check; one row per discovered target."
entity: comment
laws:
  - template-fidelity
  - findings-actionable
---
**Discovery** — `<mise tasks | npm run | make -qp | …>`
**Targets** — `<count>` (`<pass> pass, <fail> fail, <skip> skip, <yellow> yellow>`)

| target | command | status | findings |
| --- | --- | --- | --- |
| `test` | `<go test ./... | npm test | …>` | pass / fail / skip / yellow | <one-line failing tail or "n/a"> |

**Failing tails** (≤10 lines per failed target)
```
<target>: <last 10 lines of stdout/stderr verbatim>
```

**Summary** — <count by status; coverage Δ when measured>

**Next** — <`okt-task-implement` to fix the failing targets, `okt-task-review` to triage smells, or `okt-task-check` to re-run after fixes>
