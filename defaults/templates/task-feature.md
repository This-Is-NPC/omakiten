---
name: Task — Feature (DDD)
description: Feature scaffold — domain question, bounded context, aggregate, ports, acceptance criteria, risks.
entity: task
laws:
  - template-fidelity
  - design-before-code
  - ubiquitous-language
---
## Domain question
<one-sentence question the change answers — phrased in the ubiquitous language>

## Bounded context
<the context this change touches; map link if relevant>

## Aggregate(s) touched
<aggregate name(s) + invariants the change preserves>

## Ports introduced or changed
<application-side interfaces; adapters listed separately>

## Acceptance criteria (BDD)
- **Given** … **When** … **Then** …
- **Given** … **When** … **Then** …

## Non-functionals
<performance, security, observability, scale targets>

## Risks
<top 3 risks + mitigation; ADR pointers if any decision diverges>

## Out of scope
<related contexts intentionally untouched>
