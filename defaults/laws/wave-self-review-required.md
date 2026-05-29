---
name: Wave self-review required
severity: error
---
Before a wave of work is handed back, the author reviews their own diff against the brief — scope, definition of done, and the guards the wave declared. Self-review is a distinct pass, not a side effect of writing the code: read the change as a reviewer would, top to bottom, and confirm each acceptance item is observably met. The hand-back states that the self-review happened and what it found.

Bad: a wave returned "done" with three acceptance items, one of which was never wired; the gap surfaced two waves later when a dependent task could not build.
Good: the hand-back lists each acceptance item with its evidence, and flags one item as deferred with a follow-up task id — the deferral was a decision, not an omission.
