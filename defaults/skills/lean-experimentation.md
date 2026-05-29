---
name: Lean experimentation
description: MVP design, falsifiable hypotheses, acceptance signals, build-measure-learn loops over polish.
schema_version: 2
role_affinity:
  - Ideator
  - Owner
---
Lean experimentation (Ries, *The Lean Startup*, 2011; build-measure-learn) treats a feature as a test of a hypothesis rather than a finished deliverable. The aim is validated learning at the lowest cost, not polish.

## Falsifiable hypothesis

State the belief as a sentence that could be proven wrong: "We believe that *X* will cause *Y*, measured by *Z*." If no measurement *Z* can disconfirm it, it is not a hypothesis — rewrite it until it can fail.

## Minimum viable product

The MVP is the smallest build that produces a real signal against the hypothesis, not the smallest shippable feature. Strip everything that does not contribute to the measurement. Manual steps, hardcoded paths, and rough UI are acceptable if they still generate the learning.

## Acceptance signal

Define, before building, what result would confirm the hypothesis and what result would kill it. Pre-committing the signal prevents post-hoc rationalisation of an ambiguous outcome.

## Build-measure-learn loop

Build the MVP, measure against the pre-set signal, then decide: persevere (the hypothesis held — invest further), pivot (it failed but revealed a better direction), or stop. Each loop should be short; a loop that takes a quarter to close has lost most of its learning value.

## Boundaries

Experimentation justifies rough edges only while learning; once a hypothesis is validated and the work moves to production, normal engineering discipline (tests, regression analysis, docs) applies. Do not ship experiment-grade code as if it were a finished feature without a follow-up hardening pass.
