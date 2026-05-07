---
name: No assumptions
severity: warning
---
Every claim must be traceable to code, configuration, or explicit user input. When information is missing: ask, mark `[assumption]` with the guess explicit, or `[user-provided]` when the user said so without code backing. Never invent versions, file paths, or business rules to fill a section.

❌ "We use Postgres v15" — not in code, not user-said.
✅ "We use Postgres v15 [user-provided]" or "Postgres [assumption: standard for the team]".
