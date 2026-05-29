---
name: INVEST stories
description: Wake (2003) checklist — Independent / Negotiable / Valuable / Estimable / Small / Testable. Flag missing letters.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
  - Reviewer
---
INVEST (Bill Wake, 2003) is a six-letter checklist for the quality of a user story. Run a story against each letter; name the letters it fails so the author can fix the story rather than discover the gap mid-build.

- **Independent** — the story can be built and shipped without waiting on a sibling story. Hidden ordering dependencies are a sign two stories should merge or one should be resequenced.
- **Negotiable** — it states the need, not a frozen contract of implementation detail. A story that dictates the solution leaves no room for the team to find a better one.
- **Valuable** — it delivers observable value to a user or stakeholder. A story whose value is purely internal plumbing should be folded into the story it enables.
- **Estimable** — the team can size it. Inestimable usually means unknown scope or a missing spike; surface that rather than guessing.
- **Small** — it fits comfortably within an iteration. Oversized stories are split along behavioural seams, not arbitrary halves.
- **Testable** — it carries acceptance criteria a tester could pass or fail. Untestable means the criteria are aspirations, not requirements.

## Using it in review

When a story fails a letter, state which letter and why in one line, then propose the corrective move (split, sharpen the criteria, add the benefit clause). The checklist is a diagnostic, not a gate to reject work outright.

## Boundaries

INVEST evaluates story shape; it does not prioritise or estimate. Pair it with a prioritisation skill (MoSCoW, RICE) to order the stories it has validated.
