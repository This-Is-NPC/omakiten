---
name: Comment Acceptance
description: Fills the `#acceptance` guard. Project picks the format — Given/When/Then or any other testable shape.
entity: comment
laws:
  - template-fidelity
  - acceptance-criteria-required
---
**Acceptance criteria** — pick the format your project uses:

- Given/When/Then bullets, or
- numbered testable outcomes, or
- executable test stubs, or
- the project's existing convention.

Each criterion must be:
- testable (a reviewer can verify it against the implementation),
- agreed by the requester (sign-off in writing),
- and free of architectural prescription (focus on observable behaviour, not internals).

**Non-functionals** — performance / security / observability targets when they apply.

**Out of scope** — acceptance does NOT cover: <list>
