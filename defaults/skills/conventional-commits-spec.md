---
name: Conventional Commits spec
description: "Conventional Commits 1.0.0 grammar — type(scope): subject, body, footers, breaking-change markers."
schema_version: 2
role_affinity:
  - Committer
---
Grammar (Conventional Commits 1.0.0):

```
<type>(<scope>)!: <subject>
<BLANK LINE>
<body>
<BLANK LINE>
<footer(s)>
```

- **type** — `feat`, `fix`, `docs`, `refactor`, `chore`, `test`, `build`, `ci`, `perf`. `feat` and `fix` are SemVer-significant.
- **scope** — optional noun in parentheses; project package, directory, or feature slug derived from the touched paths.
- **!** — append before `:` to flag a breaking change (`feat(api)!: …`). Pair with `BREAKING CHANGE:` footer.
- **subject** — imperative mood, ≤50 chars, no trailing period.
- **body** — optional; wrap at 72 columns; explain the "why" the diff does not.
- **footers** — `Token: value` lines (e.g. `Refs: #123`, `BREAKING CHANGE: drops X`). No `Co-Authored-By: <model>` ever — the human is the author.
