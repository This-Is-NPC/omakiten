---
name: Tracer-bullet shipping
description: Walking-skeleton end-to-end first; depth comes only after the full shape is observable.
schema_version: 2
role_affinity:
  - Builder
  - Owner
---
Tracer-bullet development (Hunt & Thomas, *The Pragmatic Programmer*, 1999) builds a thin end-to-end path through the entire system first, then thickens it. The tracer connects every layer — input, processing, storage, output — with minimal logic, so the full shape is observable and the integration points are proven before any single layer is deep.

## Walking skeleton first

The initial goal is a path that runs from one end of the system to the other and produces a real, if trivial, result. It is not a prototype to throw away — it is the production structure, deliberately thin. Because it touches every seam, it exposes the hard integration questions (auth boundaries, data contracts, deployment wiring) while they are still cheap to change.

## Depth after shape

Only once the skeleton runs end-to-end do you add depth, one capability at a time, each landing on a path that already works. This contrasts with bottom-up construction, where fully-built layers cannot be exercised together until the end and integration risk concentrates at the riskiest moment.

## Why it works

The tracer gives the team and stakeholders something observable early, so feedback arrives while the design is still malleable. It also keeps the system continuously demonstrable — every increment lands on a running whole rather than an unintegrated part.

## Boundaries

The skeleton is thin, not absent: it still uses real interfaces and real wiring, distinguishing it from a mock-driven prototype. Pair it with normal test discipline as depth is added — the tracer reduces integration risk, it does not waive correctness on the layers you later fill in.
