---
name: Legacy seams
description: "Feathers 'Working Effectively with Legacy Code' (2004) — seams, characterization tests, Sprout Method/Class."
schema_version: 2
role_affinity:
  - Builder
  - Tester
---
Legacy code = code without tests (Feathers). Refactoring it safely requires finding a *seam* — a place where you can change behaviour without editing in place.

Seam kinds (Feathers §4):
- **Preprocessing seam** — toggle behaviour via build flags or macros. Rare in modern Go/TS; common in C.
- **Link seam** — replace a linked dependency at build time (test double for a real client). Useful when DI was never threaded.
- **Object seam** — dispatch via interface/virtual call; swap the implementation in tests. Default seam in OO languages.

Patterns:
- **Characterization test** (Feathers §13). Before changing legacy behaviour, write a test that pins what it *currently* does — bugs and all. The test documents the contract you may or may not preserve. Without it, refactoring is gambling.
- **Sprout Method** (Feathers §6). New behaviour belongs in a new method tested in isolation, called from the legacy method. The legacy method stays mostly untouched.
- **Sprout Class** (Feathers §6). Same idea, larger surface — extract a new class instead of a method.
- **Wrap Method / Wrap Class** (Feathers §6). When the legacy callsite can't be tested directly, wrap it in a method/class you can test and route callers through.

Signal in review: diff edits a load-bearing legacy function with no covering test. Finding: "no characterization test for the legacy path — propose Sprout Method or add the test before changing behaviour. Feathers §6 / §13."
