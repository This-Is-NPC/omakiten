---
name: Regression detection
description: "git bisect (Linus Torvalds, 2005) + characterization tests (Feathers 2004) for catching and pinning regressions on legacy paths."
schema_version: 2
role_affinity:
  - Tester
  - Reviewer
---
Two complementary techniques — one finds, one prevents.

**git bisect** (Torvalds, 2005). When a regression is observed but the introducing commit is unknown:
- `git bisect start && git bisect bad <broken> && git bisect good <known-good>` — bisect walks the history binary-search style.
- Use `git bisect run <script>` when the regression has a clean repro — the script returns 0 / non-zero per commit; bisect drives itself.
- The output is the *first bad commit*. Read its diff before reverting — the smallest fix is rarely a full revert.

**Characterization tests** (Feathers, *Working Effectively with Legacy Code*, §13). When a legacy path has no covering test and you need to change it:
- Run the legacy code with representative inputs; record outputs verbatim — bugs and all.
- Encode the recorded behaviour as a test. The test now pins the contract you may or may not preserve.
- Refactor or fix with the characterization test as the safety net. When you intentionally change behaviour, update the test in the same diff — never silently.

Signal in review: a bugfix lands with no test. Finding: "no regression test for the failure mode — pin it via a characterization test before merge so the bug cannot recur silently."
