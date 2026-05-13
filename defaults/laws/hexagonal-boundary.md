---
name: Hexagonal boundary
severity: error
---
Domain depends on nothing. Application depends on domain via interfaces (ports). Adapters depend on application — never the reverse. No shortcuts through layers; no reaching into private structs across the boundary. Enforce with arch tests.

Bad: a SQL adapter imports a domain helper directly because "it was easier"; the domain now leaks persistence concerns.
Good: the adapter defines a port in `app/`; the domain stays clean; the test asserts no forbidden import edge exists.
