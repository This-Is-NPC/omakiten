---
name: Dual peer review
severity: error
---
At minimum two independent peer reviewers — neither the task author nor a co-author of the change. Each leaves a `#peer-review` comment with verdict, approval scope, and concerns. A single reviewer is not a peer review; it is a hand-off.

Bad: merged after a single thumbs-up from a pair-programming partner.
Good: two `#peer-review` comments from engineers who did not touch the code; both approved with notes; the merge happened after both verdicts landed.
