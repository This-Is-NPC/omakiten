# Contributing

Start with [working on Omakiten](docs/internal/development.md). This file is the
canonical checklist for repository changes. [AGENTS.md](AGENTS.md) supplies
the agent contract and commit procedure.

## Toolchain and tasks

- Run `mise install` before work. `.mise.toml` pins tools and declares tasks.
- Invoke tools through mise. Use an existing task when it covers the job.
- Task implementations are executable scripts in `scripts/`, one job per script.
- Aggregators compose those scripts; hooks and CI use the same implementation.
- Generated output belongs under `.tmp/`. Persistent development state belongs
  under `dev_env/`. Profiling supplies both `-o` and `-outputdir` beneath `.tmp/`.

Set `core.hooksPath` to `scripts/hooks`. The pre-push hook runs the full gate
for clean HEAD and reports the `local-check` commit status. The gate runs
formatting, tests, lint, vulnerability, workspace, and shell checks.

## Architecture boundaries

- Domain imports no other internal package.
- Application services reach adapters through ports. `internal/contract`
  holds shared delivery contracts.
- TUI imports no CLI, concrete operation service, runtime, or storage adapter.
  CLI imports no TUI. `internal/terminal` binds the interactive ports.
- SQLite and configuration storage are leaf adapters isolated from each other.
- Configuration snapshots and runtime services belong to their project.
  Candidate reloads are accepted before publication.
- Components own cursor, viewport, and scroll state. Screens consume their APIs.

[The design](docs/internal/design.md) explains ownership.
[TUI screen assembly](docs/internal/tui-screen-assembly.md) is normative: screens
declare archetypes, content, keys, and styles; components own geometry and
measurement. Rendering consumes prepared composition. `BlockMemo` is the
single block cache and excludes terminal rows from its identity.

Run `mise exec -- go test ./internal/arch/...` after structural changes.

## Code and tests

- Keep one implementation of each behavior. Delete unused code and complete
  replacements in the same change.
- Use plain comments that describe the code or a constraint the type system
  cannot express. Put explanations of use and design in `docs/`.
- Every `internal/*` package carries a package comment in exactly one place:
  `doc.go` or above its package declaration.
- Use the standard-library `testing` package. Keep tests that protect observable
  behavior, failure paths, atomicity, and meaningful component state.
- Exercise existing tests before adding another fixture. A test that copies
  implementation arithmetic is not independent evidence.
- Use the shared YAML loaders for configuration fixtures and real SQLite
  transactions for persistence behavior.
- Existing screen recordings use deterministic fixtures and state assertions.
  Refresh them one package at a time with `-update`; full-tree runs do not
  rewrite recordings.
- Use native fuzzing for byte parsers and reproducible seeded sequences for
  stateful component invariants when those risks need coverage.

## Board and workflow

Read and write project state through `okt`. `project resume` recovers the
project; `task continue` reads the task checkpoint. Record decisions, evidence,
and handoffs on the board.

Review findings go to the Third Hokage. It decides whether they are deviations
within the current task or work requiring another task. Agents report findings
rather than creating parallel work unilaterally.

Workflow policy lives in its preset repository. Inspect its installed version
and source before updating. Validate the candidate package, then select it
deliberately; changing policy does not migrate existing tasks automatically.

## Documentation

All documentation is hand-written in English under `docs/`, indexed by
[the documentation map](docs/README.md). The root README explains the product
and points at complete procedures.

- How-to pages answer one question, state prerequisites, give steps, and explain
  the result and relevant failure paths.
- `cli.md` maps commands and output contracts; exact flags come from `--help`.
- `screens.md` describes terminal interaction in order of use.
- Contributor guides live in `docs/internal/`. Design, data model, and
  requirements describe current ownership and behavioral constraints; align
  implementation with them. Development covers tasks, generated output,
  verification, and releases.
- Update the guide that owns changed behavior in the same PR. Link shared
  explanations rather than duplicating them. Describe current behavior only.

## Commits and pull requests

Use English Conventional Commits: `type(scope): summary`. Subjects are
imperative, lowercase after the colon, at most 50 characters, and have no final
period. Breaking commits use `!` and a `BREAKING CHANGE:` footer. Keep one
intent per commit and follow the draft-before-commit procedure in AGENTS.md.

Name branches `feature/{short-name}` or `fix/{short-name}` in kebab-case.
Use `defaults/templates/pull-request.md` for PRs. State the concrete before and
after behavior, relevant files, actual validation, and material risks. References
must be accessible to a reviewer, without local database ids or machine paths.

Agents create commits; the human publishes them. Do not attribute commits to
models or add generated-by trailers.

Release-please generates `CHANGELOG.md` from commit subjects. Put release prose
in the PR body and edit changelog text only in the release-please PR.
