# Working on Omakiten

This page is for changes to the repository. For installing the product, start
with [installation](../how-to-install-and-remove.md).

## Prepare the checkout

```bash
git clone https://github.com/This-Is-NPC/omakiten.git
cd omakiten
mise install
git config core.hooksPath scripts/hooks
```

`.mise.toml` pins tools and declares every task. Scripts under `scripts/`
implement the jobs; mise, hooks, and CI consume those jobs. Use
`mise tasks ls` and `mise tasks info NAME` to inspect the current tasks.

Read [CONTRIBUTING.md](../../CONTRIBUTING.md), [design](design.md), and the
[requirements](requirements.md) before changing behavior. For TUI work,
[screen assembly](tui-screen-assembly.md) is normative.

## Run the development terminal

```bash
mise run tui
```

It builds `okt`, installs the selected workflow repository under `dev_env/`,
and runs the terminal against that isolated selection and database. Git and
repository access are needed when installing the workflow. `mise run tui:bare`
uses the existing installed development selection.

`mise run install` instead installs into the user's real application roots,
runs setup, and registers the checkout. Use it when verifying the installation
flow, rather than as a prerequisite for every code change.

## Choose verification that exercises the change

| Task | Purpose |
| --- | --- |
| `fmt`, `fmt:check` | Format sources or check formatting. |
| `build` | Build `.tmp/build/okt` with the current Git version. |
| `test` | Run all packages with real Cosign and PowerShell installer assurance, enforce coverage, then run concurrency/race checks. |
| `lint` | Run the pinned Go linter. |
| `vuln` | Scan Go dependencies for known vulnerabilities. |
| `scripts:check` | Bash syntax and ShellCheck for shell entrypoints and libraries. |
| `workspace:check` | Reject generated artifacts outside the allowed output and state directories. |
| `test:installer` | Exercise setup, wrappers, uninstall, and installer assurance. |
| `test:cross-build` | Compile filesystem safety tests for supported targets. |
| `test:coverage-checker` | Exercise coverage profile validation. |
| `gallery`, `gallery:dump` | Inspect or dump component presentations. |
| `test:profile` | Produce a focused benchmark's binary and profiles under `.tmp/profiles/`. |
| `release:dry-run` | Build and verify the release matrix with an ephemeral local signing fixture. |

Use existing focused Go tests through mise when a change touches one package:

```bash
mise exec -- go test ./internal/tui/screens/project
mise exec -- go test ./internal/arch/...
```

Run architecture tests after structural changes. Keep behavioral tests and
meaningful failure paths. Avoid tests that reproduce implementation details,
unused API fixtures, or speculative constraints. Use standard-library `testing`.
Refresh existing golden recordings one package at a time with `-update` after
confirming the visible behavior.

## Merge gate

The pre-push hook runs `scripts/local-check.sh`, which requires the checked
commit to be clean HEAD, runs `mise run check`, and posts a `local-check` status
for that commit. The gate consists of formatting, all-package tests with real
Cosign and PowerShell installer assurance, cross-builds, lint, vulnerability,
workspace, and shell checks. The same scripts back manual tasks and hooks.

Installer tests execute on the host operating system. Cross-builds compile
filesystem safety tests for Linux, macOS, Windows, and Plan 9 under `.tmp/tests/`;
they do not execute tests for another operating system. A Linux push verifies
Linux behavior and compilation for every supported target.

Authenticate the pinned GitHub CLI when needed:

```bash
mise exec -- gh auth login
```

Agents create commits; the human publishes them. Follow the commit procedure
in [AGENTS.md](../../AGENTS.md). Draft the PR using `defaults/templates/pull-request.md`
and report only verification that actually ran against the described change.

## Generated files and local state

| Directory | Contents |
| --- | --- |
| `.tmp/build/` | Local executables. |
| `.tmp/coverage/` | Coverage profiles and summaries. |
| `.tmp/tests/` | Compiled test executables. |
| `.tmp/profiles/` | Benchmark binaries and CPU/memory profiles. |
| `.tmp/cache/` | Tool and compiler caches. |
| `.tmp/release/` | Release staging and archives. |
| `dev_env/` | Persistent development configuration and database. |

Profiling must supply both `-o` and `-outputdir` so Go keeps its test binary
under `.tmp/`. Installer fixtures and `testing.T.TempDir` use the system
temporary directory, honoring `TMPDIR`. Do not put generated reports or
compiled binaries in the source tree.

## Languages

Translations live in `defaults/languages/`; English is the baseline. To scaffold
a catalog, inspect `mise tasks info language:new`, then use:

```bash
mise run language:new vi "Tiếng Việt" "Vietnamese"
```

Translate the generated values, preserving format placeholders, CLI flags,
bucket keys, and entity slugs. Bundled language tests check decoding and key
parity. Verify the picker and the affected CLI/TUI surface. Application language
preferences are described in the [language guide](../how-to-change-appearance-and-language.md).

## Releases

Release-please derives release notes and version changes from Conventional
Commits. Edit generated changelog text only in its release PR. GoReleaser,
release metadata, signing, and installer assurance are wired by
`.github/workflows/release.yml` and the release scripts.

The dry-run uses a local ephemeral key and no external signing services.
Production signing uses the configured keyless identity and transparency-log
policy. A successful dry-run is local matrix evidence, rather than a published
release or proof that production signing has completed.
