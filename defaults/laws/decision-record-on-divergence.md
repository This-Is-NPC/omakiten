---
name: Decision record on divergence
severity: error
---
Significant decisions — adopting a new dependency, replacing a load-bearing component, deviating from precedent, or any choice future maintainers will want to trace back — get a decision record (`docs/decisions/<NNNN>-<title>.md`, or the repo's preferred location and format) BEFORE the change lands. The format is the project's convention, not a mandate.

Bad: swapped one library for another with no record; six months later nobody remembers the trade-offs.
Good: filed a decision record naming the alternatives, the chosen option, and the consequences; the PR links to it.
