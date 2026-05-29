---
name: Castle tidying
description: Opportunistic, bounded cleanup of the code you pass through — leave each module a little better than you found it, without expanding the change's scope.
schema_version: 2
role_affinity:
  - Builder
---
Castle tidying is the discipline of leaving every file you open in slightly better shape than you found it, kept strictly within the blast radius of the change you came to make. It is the Boy Scout Rule (Martin, *Clean Code*) applied with a leash: improve, but do not wander.

## Tidy in the path you already touched
Clean the code your change actually crosses — rename an unclear local, delete dead code, extract a confusing inline expression — not the whole module. The cleanup rides along with work that was going to touch those lines anyway, so it adds no independent review surface.

## Keep tidying separate from behaviour change
A reviewer must be able to tell intent from cleanup at a glance. Commit structural tidying apart from behaviour-changing edits (see the conventional-commits discipline), so a tidy that introduced a regression can be reverted without losing the feature.

## Stop at the door
Tidying is not a refactoring project. When a cleanup grows past the immediate path — a cross-cutting rename, a module reshape — stop, leave it, and surface it as its own task rather than smuggling a large structural change into an unrelated diff.
