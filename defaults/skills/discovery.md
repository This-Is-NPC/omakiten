---
name: Discovery
description: Feasibility analysis, clarifying questions, scope boundaries, surfacing hidden constraints before code.
schema_version: 2
role_affinity:
  - Ideator
  - Owner
  - Concierge
---
Discovery is the work of reducing uncertainty before committing to a build. The goal is to surface what is unknown, unstated, or assumed, so that later phases operate on a clear contract rather than a guess.

## Feasibility

Assess three dimensions before endorsing a direction:

- **Technical** — does the codebase, runtime, and dependency surface support the change without a disproportionate rewrite? Name the load-bearing assumption.
- **Scope** — is the request bounded, or does it imply a chain of follow-on work? Draw the boundary explicitly and state what is out.
- **Value** — what observable outcome justifies the work? If the outcome cannot be stated as a signal, the request needs sharpening before estimation.

## Clarifying questions

Ask questions that change the design, not questions that pad the record. A good clarifying question has at least two plausible answers that lead to materially different implementations. Prefer the Five W Two H frame (What/Why/Who/When/Where/How/How much) to find the gaps; do not accept vague answers as settled.

## Hidden constraints

The expensive constraints are the ones nobody states: a regulatory deadline, an existing integration contract, a performance envelope, a deployment window. Probe for them directly. Record each surfaced constraint as a decision input so the requirements and planning phases inherit it rather than rediscovering it late.

## Boundaries

Discovery produces understanding, not code. It ends when scope, value, and the major constraints are written down and the requester confirms them. Do not begin implementation from discovery output alone — hand off to requirements or planning with the constraints attached.
