---
name: Non-functional requirements
description: Quality attributes — performance, security, usability, observability, scale, accessibility, compliance — captured separately from FRs.
schema_version: 2
role_affinity:
  - Owner
  - Ideator
---
Non-functional requirements (NFRs) describe *how well* a system behaves rather than *what* it does. They are the quality attributes of the ISO/IEC 25010 model and are easy to omit because no single feature surfaces them — yet they drive most architecture decisions. Capture them separately from functional requirements so they are not buried inside feature stories.

## Attribute checklist

Walk these categories explicitly; a silent category is usually a missed requirement:

- **Performance** — latency, throughput, resource ceilings. State the percentile and the condition (p95 under load X).
- **Security** — authn/authz model, data classification, threat surface, regulatory controls.
- **Usability** — task-completion expectations, error tolerance, learnability.
- **Observability** — what must be logged, traced, and alerted on for the system to be operable.
- **Scale** — expected and peak volumes; growth horizon the design must absorb.
- **Accessibility** — conformance target (e.g. WCAG level) where a human interface exists.
- **Compliance** — legal, audit, and retention obligations the system inherits.

## Make them testable

An NFR is only useful if it has a number and a condition. "Fast" is not a requirement; "p95 page load under 1.5s on a 3G profile" is. Pair each NFR with a measurement method so the tester can verify it and the reviewer can check the design against it.

## Boundaries

NFRs constrain the design but do not specify behaviour. Record them once at the requirement level and reference them from the stories they affect, rather than restating them per feature.
