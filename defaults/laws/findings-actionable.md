---
name: Findings actionable
severity: warning
---
Every review finding ships a concrete fix on the same line — a named refactoring, a code edit, or an explicit "no action; signal noted". Vague observations ("this could be cleaner", "consider simplifying") waste reviewer attention.

Bad: "the function is long".
Good: "`handler.go:42-118`: Long Function — extract the validation block (lines 60-90) as `validateRequest`."
