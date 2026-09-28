# WATCHDOG.md

Review guidance for the advisor. These are the failure modes this repository pays for late —
several of them land as a *green* test, so they survive a normal read of the diff.

## Severity

- `blocker` — the change violates something already enforced by a test or by the merge gate.
  It will fail; say so and stop the agent.
- `concern` — the change contradicts a documented rule that no gate catches yet.
- `nit` — style, naming and phrasing.

Rank by blast radius rather than by count. One `blocker` on a boundary outweighs a page of nits.

## Fixtures that pass while proving nothing

- Golden fixtures are read and written through `testutil.Golden` alone. A second writer — a
  hand-rolled `os.ReadFile`/`os.WriteFile` pair, an environment switch, an alternative refresh
  flag — regenerates fixtures silently and reports green. `internal/arch/golden_refresh_boundary_test.go`
  fails the build when one reappears.
- Every `screentest.Recording` declares an `Assert`, and the assert gates the write on `-update`
  runs. A recording without one is rejected; a fixture refreshed over an empty screen proves nothing.
- Asserts name state the screen exposes — a non-zero `Scroll()`, a cursor off its default cell,
  an open mode — rather than rendered text, which the fixture already captures.
- Fixtures are refreshed one package at a time. Flag any attempt to run `-update` across the tree.

## Enforced boundaries

- Imports respect the hexagonal layering described in `AGENTS.md` and enforced by
  `internal/arch/arch_test.go`.
- Cursor and scroll state flows through `cursorwindow.Model`, `picker.WithCursor`,
  `picker.WithScroll` and `viewport.WithScroll`. Direct `*Scroll =` and `*Cursor =` writes
  outside `internal/tui/components/` are rejected by `internal/arch/scroll_state_boundary_test.go`.
- Structural changes are followed by `go test ./internal/arch/...`.

## Logic creeping back into a TUI screen

Screens declare archetype, content and keys. The logic that once lived in them was removed on
purpose, and a screen that starts computing again is the regression this repository most expects.
`.docs/internal/tui-screen-assembly.md` is the normative contract; its scoreboard also says which
patterns a gate already catches, which sets the severity.

Gated — treat a violation as a `blocker`:

- Geometry originates in `screenlayout.Canvas` (`screenlayout_canvas_boundary_test.go`).
- The measure comes from the vocabulary rather than the screen (`width_measure_boundary_test.go`).
- `screenlayout.BlockMemo` is the only memo and its key excludes `rows` (`memo_key_rows_boundary_test.go`).
- Chrome, row and keystroke budgets fall out of measurement rather than a hand-counted constant
  (`chrome_budget_boundary_test.go`, `row_budget_boundary_test.go`, `keystroke_budget_boundary_test.go`).
- Style is declared per box (`style_render_loop_boundary_test.go`, whose allowlist is still shrinking).

Review-only — raise these as a `concern`, since no gate will:

- Composition belongs ahead of the render, not inside it.
- One painted box is one zone.
- A read-only zone is `ScrollItems` without a cursor.
- An archetype is chosen before a loose `Spec`.
- A screen constructs no style of its own.

A refactor records its baseline fixture first, in its own commit, before anything is touched.

## Coverage and release hygiene

- The aggregate statement ratio stays at or above the floor enforced by `scripts/check-coverage.sh`.
  Coverage evidence that is missing, empty, malformed or stale fails closed, and there are no
  per-package exemptions to appeal to.
- `CHANGELOG.md` is generated. Hand-written version headings and manual `Unreleased` sections
  are dropped by release-please and accumulate as noise.

## Tests

- Tests use the standard library `testing` package. `testscript` is on the radar and is not
  adopted; introducing it needs an explicit amendment to `CONTRIBUTING.md`, not a PR that
  quietly adds the dependency.
- New table-driven tests use map-based subtests, so that ordering dependencies surface and
  duplicate case names fail at compile time.

## Documentation

- A behavior change and the guide that documents it land in the same PR.
- A new `internal/*` package carries a package comment in exactly one place. A `doc.go` added
  beside a main file that already describes the package is a duplicate.
- Commits, comments and documentation are written in English.

## Closing a review

A review of completed work closes by running the assurance lenses and routing what they find:
findings go to the Third Hokage, who decides whether each one is a deviation inside the task in
flight or new work that earns its own task. A review that ends with unrouted findings is incomplete.
