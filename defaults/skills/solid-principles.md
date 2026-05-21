---
name: SOLID principles
description: Robert C. Martin's SOLID — Single Responsibility, Open/Closed, Liskov Substitution, Interface Segregation, Dependency Inversion.
---
Cite by abbreviation when surfacing a finding; each one carries a concrete signal pattern.

- **SRP — Single Responsibility** (Martin, *Clean Architecture* §7). A module has one reason to change. Signal: divergent change smell, mixed concerns (HTTP handler also formatting dates and writing to disk). Fix: split along the change axis.
- **OCP — Open/Closed** (Martin §8; original Meyer 1988). Open for extension, closed for modification. Signal: edits to a stable module to add a new case; switch statements that grow with each feature. Fix: dispatch via polymorphism / strategy.
- **LSP — Liskov Substitution** (Liskov 1987; Martin §9). Subtypes are substitutable without surprising callers. Signal: subclass throws `NotImplemented`, narrows preconditions, or widens postconditions. Fix: rework the hierarchy or replace inheritance with composition.
- **ISP — Interface Segregation** (Martin §10). Don't force clients to depend on methods they don't use. Signal: implementers stub half the interface. Fix: split into role-narrowed interfaces.
- **DIP — Dependency Inversion** (Martin §11). High-level modules depend on abstractions, not concretions. Signal: business logic imports a database driver or HTTP client directly. Fix: inject the abstraction; concretion lives at the composition root.
