---
name: Code smells
description: "Fowler/Beck smell catalog — long function, large class, feature envy, primitive obsession, shotgun surgery, divergent change."
---
Smells are pattern-matching shortcuts. Each one points to a likely refactoring without prescribing the fix.

Bloaters:
- **Long Function** — body past one screen, multiple intents. → Extract Function. Fowler §3 ¶1.
- **Large Class** — class accreting unrelated responsibilities. → Extract Class. Fowler §3 ¶2.
- **Primitive Obsession** — strings/ints carrying domain meaning (`userId string`). → Replace Primitive with Object. Fowler §3 ¶3.
- **Long Parameter List** — five+ parameters, recurring groups. → Introduce Parameter Object. Fowler §3 ¶4.

Object-orientation abusers:
- **Switch Statements** — type-tag switches repeated across the codebase. → Replace Conditional with Polymorphism. Fowler §3 ¶5.
- **Repeated Switches** — same switch shape duplicated; the giveaway is shotgun surgery on add. Fowler §3 ¶6.

Change preventers:
- **Divergent Change** — one class touched for unrelated reasons. → Extract Class along the change axis. Fowler §3 ¶7.
- **Shotgun Surgery** — one change touches N classes. → Move Function / Move Field to consolidate. Fowler §3 ¶8.

Couplers:
- **Feature Envy** — method calls another object's data more than its own. → Move Function. Fowler §3 ¶9.
- **Inappropriate Intimacy** — two classes know each other's privates. → Move Method, Hide Delegate. Fowler §3 ¶10.

Dispensables:
- **Comments as Deodorant** — long comment explaining what the code does. → Extract Function with intent-revealing name. Fowler §3 ¶11.
- **Speculative Generality** — abstract base class with one subclass; "we'll need this someday". → Collapse Hierarchy / Inline Class. Fowler §3 ¶12.
- **Dead Code** — unreachable branch, unused parameter. → delete. Fowler §3 ¶13.
