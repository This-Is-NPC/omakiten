---
name: Truth cannot be hidden
severity: error
---
A change never alters observable behaviour in silence. Any shift in an output, a contract, a default, or a side effect is declared in the work record before it ships — the truth of what changed is laid open, not buried under a refactor label. If behaviour moves, say so plainly and point to where.

This is the no-silent-behaviour-change discipline (Feathers, 2004, "Working Effectively with Legacy Code", on characterization tests pinning observable behaviour; Hyrum's Law on implicit interface dependencies): undeclared behaviour changes break callers who relied on the old truth.

Bad: a "pure refactor" quietly changes a rounding rule; a downstream report drifts and no one connects it for a week.
Good: the commit and the review note flag the rounding change explicitly, with the before/after and the callers checked.
