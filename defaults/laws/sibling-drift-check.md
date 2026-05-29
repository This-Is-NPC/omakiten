---
name: Sibling drift check
severity: warning
---
When a change edits one instance of a pattern that recurs across sibling files or call sites, check the siblings for the same issue before handing back. Fixing one of N parallel cases and leaving the rest is drift: the codebase now contradicts itself, and the next reader cannot tell which form is intended. Either apply the change to every sibling or record why the others are intentionally left — silence reads as an oversight.

Bad: a nil-guard added to one of six handlers that share the same request-decode shape; the other five kept panicking, and the "fix" made the inconsistency look deliberate.
Good: the same guard applied to all six handlers via a shared helper, or applied to one with a note that the others take a different code path that cannot hit nil.

Relates to Fowler, *Refactoring* (2nd ed.) §3 on Shotgun Surgery and Divergent Change — a change that should touch N sites but touches one is the smell surfacing.
