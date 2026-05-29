---
name: Risk-driven development
description: Pre-mortem authoring, blast-radius analysis, irreversibility classification, mitigation-first design.
schema_version: 2
role_affinity:
  - Owner
  - Builder
  - Reviewer
---
Risk-driven development sequences work by the risk it carries rather than by feature order: attack the most dangerous unknowns first, while there is still time and budget to respond. It draws on Boehm's spiral model (1986) for risk-first sequencing and on Klein's pre-mortem technique (*Performing a Project Premortem*, HBR, 2007) for surfacing failure modes before they occur.

## Pre-mortem

Before committing to an approach, imagine it has already failed and ask why. The pre-mortem (Klein, 2007) inverts optimism: instead of asking "what could go wrong," the team asserts "it went wrong — explain how," which surfaces failure modes that prospective risk-assessment misses. Record each imagined failure as a concrete, addressable risk.

## Blast-radius analysis

For each change, map what it can break if it goes wrong: the callers, the data it touches, the systems downstream. The blast radius — not the diff size — determines how much caution, testing, and review the change warrants. A one-line change to a shared contract can have a system-wide radius.

## Irreversibility classification

Classify each decision by how hard it is to undo. Reversible decisions (a feature flag, a config value) can be made fast and corrected cheaply. Irreversible ones (a data migration, a published API contract, a deleted record) demand more analysis up front because there is no cheap rollback. Spend caution in proportion to irreversibility.

## Mitigation-first design

Where a risk is material, design the mitigation before the feature: the feature flag, the migration's reverse path, the canary, the characterization test. Mitigation built in from the start is cheaper and more reliable than mitigation retrofitted after an incident.

## Boundaries

Risk-driven sequencing decides *order and caution*, not scope. Pair it with the regression and testing disciplines that implement the mitigations, and with time-boxing to bound the spikes that retire the largest unknowns.
