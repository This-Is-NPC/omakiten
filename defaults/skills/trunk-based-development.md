---
name: Trunk-based development
description: Short-lived branches (<1 day), frequent rebases on main, feature flags for incomplete work, fast revert.
schema_version: 2
role_affinity:
  - Builder
  - Committer
---
Trunk-based development (Forsgren, Humble & Kim, *Accelerate*, 2018, where it correlates with elite delivery performance) keeps all work integrating into a single shared trunk continuously, rather than diverging onto long-lived branches. The practice minimises merge debt and keeps the trunk releasable.

## Short-lived branches

A branch lives less than a day before it merges back to trunk. The longer a branch lives, the more the trunk drifts beneath it and the more painful the eventual merge. If work cannot complete in a day, split it into trunk-mergeable increments rather than holding a long branch.

## Frequent integration

Rebase or merge from trunk often so your branch stays close to the shared state and conflicts surface small and early. Continuous integration against the trunk is what makes "it works on my branch" mean "it works on the trunk."

## Feature flags for incomplete work

Incomplete features merge to trunk behind a flag, off by default, rather than waiting on a branch. This keeps the code integrating continuously while the feature stays invisible until ready. The flag — not branch isolation — is the unit of in-progress concealment.

## Fast revert

Because changes are small and land on a continuously-green trunk, the recovery move for a bad change is a fast revert, not a forensic hotfix. Small batches make the revert surgical.

## Boundaries

Trunk-based development depends on a fast, trustworthy CI gate and on small batches; without them it degrades into an unstable trunk. Pair it with continuous-integration discipline and feature-flag hygiene (flags are removed once the feature is permanent, not left to accumulate).
