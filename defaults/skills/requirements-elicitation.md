---
name: Requirements elicitation
description: Gather needs from stakeholders; INVEST-style user stories; testable acceptance criteria; documented sign-off.
schema_version: 2
role_affinity:
  - Ideator
  - Owner
---
Elicitation turns stakeholder needs into a written contract the build can be measured against. The output is a set of stories with testable acceptance criteria and a recorded sign-off — not a transcript of the conversation.

## Gather

Identify every stakeholder who has a stake in the outcome: requester, end user, the party who signs off, and any downstream system owner. Interview for needs, not solutions — capture the problem each stakeholder is trying to solve before any proposed feature. Use the Five W Two H frame to find unstated gaps.

## Shape into stories

Express each need as a user story (role, capability, benefit) and check it against the INVEST criteria (Wake, 2003): Independent, Negotiable, Valuable, Estimable, Small, Testable. A story that fails a letter is a story that needs splitting, sharpening, or deferring.

## Define acceptance

Every story carries acceptance criteria the requester and the reviewer can both verify — prefer the Given/When/Then shape for behavioural criteria. Criteria that cannot be checked are aspirations, not requirements; rewrite them until a tester could pass or fail them unambiguously.

## Record sign-off

Capture explicit agreement on scope and criteria from the accountable party. The sign-off is what distinguishes settled requirements from an ongoing conversation; later scope changes are tracked against it rather than silently absorbed.

## Boundaries

Elicitation precedes planning and implementation. It does not estimate effort or sequence work — it produces the validated, signed-off requirement set those phases consume.
