---
name: Ubiquitous language
severity: error
---
Names in code mirror names the domain experts use. Translate, never invent. The glossary lives in `docs/glossary.md` or per-bounded-context maps; both code and conversation reference the same terms.

Bad: domain expert says "invoice"; the model has `Bill`, `ChargeRequest`, and `LedgerEntry` for the same concept.
Good: the domain calls it `Invoice`; the model has one `Invoice` aggregate, and every adapter spells it `Invoice` too.
