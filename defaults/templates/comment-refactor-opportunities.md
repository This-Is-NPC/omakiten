---
name: Comment — Refactor opportunities
description: "Fowler-named refactor opportunities surfaced by okt-review; each row cites the methodology and the target."
entity: comment
laws:
  - template-fidelity
  - findings-actionable
---
**Base** — `<base ref>`
**Head** — `<head ref>`

| path:line | refactoring | rationale | effort |
| --- | --- | --- | --- |
| `internal/x/y.go:42-88` | Extract Function — Fowler | <smell + methodology citation> | <S / M / L> |

**Notes** — defer / pursue rationale per opportunity when effort ≠ S.

**Next** — surface the S-effort items to `okt-implement` as boy-scout drive-bys; spin M/L items into their own `okt-create` tasks.
