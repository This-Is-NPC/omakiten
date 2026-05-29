---
name: Design documentation
description: Capture the approach in the repo's preferred format (decision record / RFC / design doc) before coding.
schema_version: 2
role_affinity:
  - Scribe
  - Owner
  - Ideator
---
Design documentation records the intended approach *before* the code is written, so the design can be reviewed and challenged while it is still cheap to change. The artifact is a thinking tool first and a record second: writing the approach down exposes the gaps that a mental model hides.

## Choose the repo's format

Use whatever form the repository already prefers rather than inventing one:

- **Decision record (ADR)** — for a single, consequential choice with alternatives and trade-offs.
- **RFC** — for a larger proposal that needs broad review and comment before adoption.
- **Design doc** — for a feature or subsystem where the approach, interfaces, and risks need laying out together.

Matching the existing format keeps the documents discoverable and comparable.

## What a good design doc contains

State the problem and constraints, the proposed approach, the alternatives considered and why they were rejected, the interfaces and data shapes the change introduces, and the risks with their mitigations. The alternatives section is the one most often skipped and the most valuable in review — it shows the design space was explored, not assumed.

## Before coding

The document precedes implementation and is the input to a design review. Its purpose is to surface disagreement early; a design that survives review with its risks named is far cheaper than one discovered wrong mid-build. Capture the decision so the eventual code can be traced back to the reasoning.

## Boundaries

Design documentation captures the *plan*; it is not the as-built record. When implementation deviates from the design, update the document or supersede it with a decision record rather than leaving the two in conflict. It feeds, and is distinct from, the durable architecture documentation produced after the fact.
