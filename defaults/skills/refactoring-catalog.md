---
name: Refactoring catalog
description: "Fowler 'Refactoring' (1999, 2nd ed. 2018) named refactorings — Extract, Inline, Move, Rename, Replace Conditional with Polymorphism, etc."
schema_version: 2
role_affinity:
  - Builder
  - Reviewer
---
Cite refactorings by name when proposing changes. Each name is a contract — reader knows the shape without re-reading the diff.

Composing methods:
- **Extract Function** — long function with a coherent slice; pull it out and call by intent-revealing name. Fowler §6.1.
- **Inline Function** — body is clearer than the call; remove the indirection. Fowler §6.2.
- **Extract Variable** — sub-expression repeats or hides intent; bind a name. Fowler §6.3.
- **Replace Temp with Query** — temp computed once; turn it into a method. Fowler §7.4.

Moving features:
- **Move Function / Move Field** — feature envy; relocate to the data it touches. Fowler §8.1, §8.2.
- **Slide Statements** — group related statements before extracting. Fowler §8.6.

Organizing data:
- **Replace Primitive with Object** — primitive obsession; wrap in a value type. Fowler §7.3.
- **Encapsulate Collection** — exposed list lets callers mutate; return a copy or a guarded view. Fowler §7.2.

Simplifying conditionals:
- **Decompose Conditional** — gnarly `if`/`else`; extract predicate and branch bodies into named functions. Fowler §10.1.
- **Replace Conditional with Polymorphism** — switch on type tag; replace with subclass dispatch. Fowler §10.4.
- **Replace Nested Conditional with Guard Clauses** — flatten the happy path; early-return the edge cases. Fowler §10.3.

Simplifying APIs:
- **Introduce Parameter Object** — recurring group of parameters; bundle them. Fowler §6.8.
- **Remove Flag Argument** — boolean parameter switching behaviour; split into two methods. Fowler §11.3.
