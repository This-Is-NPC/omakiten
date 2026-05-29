---
name: Comment — Brief
description: "Compact assignment brief handed to an implementer or subagent before work starts: scope, constraints, definition of done."
entity: comment
laws:
  - template-fidelity
  - scope-from-paths
---
**Task** — <id + one-line objective>
**Scope** — <files / packages / surfaces in play; name what is explicitly out of scope>

**Constraints**
- <constraint 1, e.g. "config/entity files only — no Go source">
- <constraint 2, e.g. "English, conventional commits, no AI-attribution trailer">

**Definition of done**
- <observable outcome 1, e.g. "entity loader picks up new files">
- <observable outcome 2, e.g. "`mise run check` green">

**Hand-back** — <what to report when finished: commits, test evidence, deviations>
