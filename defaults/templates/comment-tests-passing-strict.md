---
name: Comment — Tests passing (strict)
description: Test evidence with coverage delta + types + perf regression check; satisfies the coverage-gate law.
entity: comment
laws:
  - template-fidelity
  - test-evidence
  - coverage-gate
---
**Command** — `<test command + flags>`

**Output snippet** (≤10 lines)
```
<copy the relevant pass/fail lines>
```

**Coverage**
| Package | Before | After | Delta |
| --- | --- | --- | --- |
| `<pkg>` | <x>% | <y>% | <±z>% |

**Test types** — unit ✓ · integration ✓ · e2e <✓/n/a>

**Perf regression check** — <benchmark run + result + any delta vs baseline>

**CI run id** — <link or build number>
