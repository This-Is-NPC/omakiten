---
name: User story writing
description: Authoring user stories — Description, AC, DoD, scope; matches the task template verbatim.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
  - Scribe
---
A user story captures a need from the consumer's point of view so the team builds the outcome, not a feature spec in disguise. The canonical form (Cohn, *User Stories Applied*, 2004) is: *As a [role], I want [capability], so that [benefit]*. The benefit clause is the part most often dropped and the part that justifies the work.

## Structure

Author each story against the task template's sections verbatim so downstream tooling and reviewers find what they expect:

- **Description** — the role/capability/benefit statement plus any context needed to understand the need.
- **Acceptance criteria** — testable conditions in Given/When/Then or an equivalent checkable shape; the contract for done.
- **Definition of Done** — the cross-cutting bar every story must clear (tests, docs, review) beyond its own criteria.
- **Scope** — what is explicitly in and out, so the boundary is recorded rather than assumed.

## Quality bar

Check the story against INVEST (Wake, 2003): Independent, Negotiable, Valuable, Estimable, Small, Testable. A story too large to estimate or test is a candidate for splitting along a behavioural seam, not a vertical-slice technicality.

## Boundaries

The story states the need and its acceptance bar; it does not prescribe the implementation. Keep solution detail in the design or planning artifact. Match the template structure exactly — do not invent section names — so stories remain interchangeable across the workflow.
