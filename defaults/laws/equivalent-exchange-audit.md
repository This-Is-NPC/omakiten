---
name: Equivalent exchange audit
severity: warning
---
Every claim of completion must be paid for with evidence of equal weight — nothing is gained without giving something of equal value in return. A "done" is backed by the artefacts that prove it: passing tests named, coverage of the changed lines, the command output that was actually run. A self-report that asserts more than its evidence supports is a debt, and the audit calls it in.

This is the self-report / test-evidence discipline (Humble & Farley, 2010, "Continuous Delivery", ch. 4 on the deployment pipeline as the authoritative record): the pipeline's evidence, not the author's word, is what certifies a change.

Bad: "all tests pass" with no run output and three files left untouched by the suite.
Good: `#self-report` quotes the exact `mise run check` invocation, the green result, and the new cases that cover the changed paths.
