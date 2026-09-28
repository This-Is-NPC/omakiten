# Agent rules

This file is the contract for anyone (human or agent) changing a
repository that uses it. It is self-contained: do not look up an
external skill to commit, and do not invent a second toolchain
beside mise.

The premises, mise rules, and commit rules below are **not**
project-specific. Copy them as-is. Architecture, layout, and
constraints of *this* tree live only under **This project**.

## Premises

- **Performance is a reason to change.** If a change is correct and
  makes sense, do it even when the measured gain is 0.01 ms. Do not
  skip a worthwhile optimisation because the delta is small. Measure
  when the cost of being wrong is high; do not use "nobody can
  perceive it" as a veto.
- **Zero dead code.** Delete unused functions, types, imports, flags,
  and files. Do not leave a replacement beside the thing it replaces.
- **Zero duplicated code.** One implementation of a behaviour. Extract
  or share rather than copy.
- **Zero legacy code.** No compatibility shims, deprecated aliases,
  dual paths, or "keep the old one until later" leftovers. Finish the
  cutover in the same change that introduces the new path.
- **Comments state the code.** A comment names what the next lines
  do, or a constraint the type system cannot. It is not a tutorial,
  a history, or a design essay.
- **Documentation lives in `docs/`.** Guides, measurements, language
  traps, and "how this is meant to be used" go there — not in
  comments, not in commit messages as a substitute for a doc, and not
  in this file except as rules.
- **English only.** Code, comments, commit messages, `docs/`,
  `README.md`, this file, mise task names and descriptions: English.

## Tooling: mise

mise owns the toolchain and every project command. Pin tools in
`mise.toml`. Run `mise install` before work. Invoke tools through
mise (`mise exec -- …` or, once defined, `mise run <task>`). Do not
call a system compiler or helper that is not the pinned one. Do not
add a parallel runner (Make, npm scripts, ad-hoc shell wrappers)
that duplicates a mise task.

### Tasks

Work lives in **scripts**, not in `mise.toml` bodies and not in
`mise-tasks/`. Each script is one job. mise **consumes** those
scripts: a `[tasks.name]` entry names the script, sets
`description` / `depends` / `env` / `sources` / `outputs` /
`usage` / `confirm`, and runs it. Git hooks and any other caller
run the same file. Do not duplicate the job as inline `run =`
shell, a file task, and a script.

- Put scripts under `scripts/`. Make them executable, with a
  shebang. Prefer `MISE_PROJECT_ROOT` when the script must know
  the repo root; otherwise relative paths from the project root
  (mise sets cwd to the directory of `mise.toml`).
- **Atomic:** one script, one responsibility. Compose larger work
  with `depends` (may run in parallel) or a `run` **array** of
  script invocations (sequential; a failure stops the rest,
  `set -e` for `sh`/`bash`/`zsh`). Aggregators (`check`, `bench`,
  `build`) only wire scripts together. The documented entry is
  `mise run <name>`; a hook calls `scripts/<name>`, not a second
  copy of the logic.
- **Idempotent:** running the same script twice with the same
  inputs leaves the tree in the same good state. Safe to re-run
  from a hook, CI, or `mise run`.
- Give every task a `description`. Use `alias` only when the short
  name is used often. Confirm destructive tasks with `confirm`.
- Declare `sources` and `outputs` when a task is skippable if
  inputs have not changed (builds). Use `sources` alone with
  `mise watch`.
- Arguments: a `usage` spec on the TOML task. Values arrive as
  `usage_*` environment variables. Do not use the deprecated Tera
  `arg()` / `option()` / `flag()` helpers.
- Do not add file tasks under `mise-tasks/` (or the other mise
  task directories). Do not keep a TOML task and a file task for
  the same name.
- List and inspect with `mise tasks ls` and
  `mise tasks info <name>`. Run with `mise run <name>`. Do not
  tell the user to `mise exec` a one-off that already has a task.
  Do not tell them to invoke `scripts/` when a task exists, except
  from git hooks and other non-mise entry points that must call
  the script directly.

## Commits (okt-task-commit)

Agents draft and create commits. The human owns publication: **never
`git push`**. When the tree is clean, say that a push is ready.

These rules inline the okt-task-commit playbook, the Conventional
Commits 1.0.0 grammar used with it, and the conventional-commits law.
There is nothing extra to fetch.

### Procedure

1. Read `git status` and `git diff --cached`. If nothing is staged,
   read the unstaged diff (`git diff` and untracked files).
2. Group hunks into **one intent per commit**. Split mixed trees with
   non-interactive staging only: `git add <path>` and
   `git restore --staged <path>`. Do not use `git add -p` or
   `git add -i`.
3. Derive **scope** from the paths touched (package, directory, or
   feature slug).
4. Draft the message (grammar below). Show every draft to the user
   **before** `git commit`.
5. Commit. Do not push. When the working tree is clean, suggest the
   user push when ready.

### Message grammar

```
<type>(<scope>)!: <subject>

<body>

<footer(s)>
```

- **type** (required): `feat`, `fix`, `docs`, `refactor`, `chore`,
  `test`, `build`, `ci`, `perf`. `feat` and `fix` are
  SemVer-significant. One type per commit: a feature and a fix are
  two commits.
- **scope**: noun in parentheses, from the paths. Omit only when no
  scope is honest.
- **!**: immediately before `:` for a breaking change
  (`feat(api)!: …`). Pair with a `BREAKING CHANGE:` footer.
- **subject**: English, imperative mood ("Add" not "Added"), ≤50
  characters, no trailing period, lowercase after the colon.
- **body** (optional): wrap at 72 columns. Explain the *why* the
  diff does not. Not a paste of the patch.
- **footers**: `Token: value` lines (`Refs: #123`,
  `BREAKING CHANGE: drops X`).

Never attribute a commit to an agent: no `Co-Authored-By: <model>`,
no `Generated with <tool>`, no model name in trailer or body. The
human running the session is the author. No opt-out. Other
`Co-Authored-By` trailers only if the user asked for that person in
this conversation.

Bad: `feat: add filter and fix duplicate insert` (two intents).
Good: `feat(filter): add priority option` then
`fix(insert): prevent duplicates`.

## This project

Omakiten is a checkpoint layer for AI-assisted development: a local-first store of tasks,
decisions, errors, solutions and handoff notes that agents read before acting and write
before leaving. Go module `omakiten`, binary `okt`, shipping a CLI, a TUI and an MCP server.

`CONTRIBUTING.md` is the canonical project checklist and wins over the
project-specific guidance below wherever the two overlap. In this tree,
the mise configuration is `.mise.toml` and the documentation root is
`.docs/`; use those paths for the tooling and documentation rules above.

### Toolchain and commands

`.mise.toml` is the source of truth for the toolchain and every task. Read it instead of
assuming a version or inventing a command.

- Run `mise run check` before opening a PR. It covers tests, lint, vulnerability scan and
  doc checks, and the pre-push hook runs the same gate.
- Run `go test ./internal/arch/...` after any structural change.
- Refresh golden fixtures one package at a time with `go test ./path/to/pkg -update`.
  A full-tree run never rewrites fixtures, and `-update` is the single supported switch.

Generated output belongs under `.tmp/`; see the generated-file layout in
`.docs/internal/dev-guide.md`. Profiling commands must set both `-o` and
`-outputdir` so Go does not leave a test binary in the working directory.

### Architecture boundaries

The codebase is hexagonal and the boundaries are enforced by tests in `internal/arch`.

- `internal/domain` is the inner core and imports nothing else from `internal/`.
- `internal/app` reaches adapters through the ports declared in `internal/app/ports.go`.
- `internal/sqlite` and `internal/configstore` are leaf adapters, isolated from each other
  and from consumers.
- `internal/tui/components/*` own their cursor, scroll and viewport state. Renderers consume
  the component API; cursor and scroll state flows through `cursorwindow.Model`,
  `picker.WithCursor`, `picker.WithScroll` and `viewport.WithScroll`.

#### TUI screens carry no logic

Building a screen is declaring its archetype, its content and its keys — nothing else. Every
other number a screen might want is received rather than computed:

- Geometry comes from `screenlayout.Canvas`.
- The measure comes from the `screenkit` vocabulary.
- `screenlayout.BlockMemo` is the only memo, and its key never contains `rows`.
- Composition happens ahead of the render, under the keystroke budget recorded by
  `screentest.Budgets`.
- Style is declared per box; a screen constructs none of its own.

The logic that used to live in screens was removed deliberately and does not return. Read
`.docs/internal/tui-screen-assembly.md` — it is normative — before adding or refactoring a screen.

### Where the knowledge lives

- `CONTRIBUTING.md` — the rules every PR follows.
- `.docs/README.md` — index and authoring rules for all documentation.
- `.docs/internal/dev-guide.md` — prerequisites, local workflow, release mechanics.
- `.docs/internal/architecture.md` and `.docs/internal/requirements.md` — the verified
  project knowledge base. Align planning and implementation with what they document.

Update the guide that covers a behavior in the same PR that changes it.

### Working with the board

Omakiten is the source of truth for tasks, plans, comments and handoffs. Read and write it
through the `okt` CLI or the Omakiten MCP tools rather than through loose files.

Findings from a review become tasks through the Third Hokage alone. Any agent that turns up
a finding reports it; the Hokage classifies it as a deviation inside the task in flight or as
new work that earns its own task, and creates the task when it earns one.

### Conventions

- Commits, comments and documentation are written in English.
- Commit subjects follow Conventional Commits: `type(scope): summary`.
- Branches are named `feature/{short-name}` or `fix/{short-name}` in kebab-case.
- `CHANGELOG.md` is generated by release-please from commit subjects. Write release prose in
  the PR body, and edit changelog text only inside the release-please PR.
- Tests use the standard library `testing` package.
- Every `internal/*` package carries a package comment, in exactly one place: a `doc.go` or
  the block above `package <name>`.
