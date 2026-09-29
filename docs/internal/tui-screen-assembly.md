# TUI screen assembly

This guide is normative for `internal/tui/screens/`. A screen declares its
archetype, content and keys. Shared components arrange, measure and style that
content; application operations execute outside the screen.

## Ownership

- `screenhost` defines stable screen IDs, frames, actions, outcomes and lifecycle.
- `screen_registry.go` declares placement, labels, palette routes, factories,
  chrome and reload policy. The root stores one base screen ID and a detail
  stack; navigation history stores those same IDs.
- Screens own their interaction state. The host supplies prepared payloads and
  narrow callbacks, executes semantic intents and rejects stale async results.
- `internal/contract` supplies operation inputs, results and delivery ports.
  Screens and the TUI host import no concrete operation services, runtime,
  storage adapters or CLI implementation.
- `internal/terminal` binds the production ports and starts Bubble Tea.

## Assembly rules

1. Declare geometry in `screenlayout.Spec` and grid nodes. Receive the resulting
   `screenlayout.Canvas`; do not calculate terminal rows, column allocation,
   scroll hint space or breakpoints in a screen.
2. Use `screenkit` measurements and style vocabulary. Declare style per box;
   construct no Lipgloss styles in a screen or rendered row loop.
3. Prepare content on payload changes, resize and relevant interaction changes.
   Rendering consumes prepared state. A cursor repaint must not rebuild an
   unchanged semantic projection or Markdown document.
4. Use `screenlayout.BlockMemo` for render blocks. Its identity describes the
   content and width; terminal rows are viewport state and never enter its key.
5. Let components own cursors, scroll offsets and viewport state. Renderers
   consume the component API instead of maintaining parallel integer state.
6. Express read-only scrolling sections with `ScrollItems` and no selectable
   cursor. Use `ScrollNone` only for fixed content that needs no scroll keys.
7. Sanitize persisted text at the terminal sink. Keep Markdown line breaks;
   remove terminal controls before trusting a value as styled output.
8. Receive business classifications, counters and event metadata in payloads.
   Event rows already contain category, display label, summary and visibility
   from the active project's immutable registry.
9. Route persistence, reload and project changes through host actions or ports.
   Keep generation, route and entity checks before applying asynchronous results.

## Verification

`internal/arch/arch_test.go` scans production imports and verifies the dependency
boundaries. `.golangci.yml` mirrors those rules. Existing ruleguard rules cover
component assembly constraints. Run `go test ./internal/arch/...` after structural
changes and the existing tests in affected screen and component packages.

`screenfixture` supplies deterministic data and frames for the gallery and
screen tests. `screentest.Record` records existing screen scenarios at the shared
terminal geometries, checks their state assertions before writing, and compares
independent materializations. Refresh a changed fixture one package at a time
with `go test ./internal/tui/screens/<package> -update` only after confirming the
behavioral change.

Performance measurements use the existing focused benchmarks and profiling
commands in `.mise.toml`. Put generated binaries and profiles under `.tmp/`.
Use existing tests that protect observable scroll and layout behavior.

## Refactoring a screen

Read its descriptor, payload, component declarations and existing recordings.
Move semantic work to its owning projection or application service. Pass the
result through a payload or narrow port. Keep one implementation and delete the
replaced state, renderer, alias and dispatch path in the same change.

Exercise navigation, resize, reload, persistence failure and stale async
completion with existing focused tests where those behaviors apply. Preserve
recordings that protect actual interaction state. Add tests only for a concrete
behavioral risk that those tests do not already cover.
