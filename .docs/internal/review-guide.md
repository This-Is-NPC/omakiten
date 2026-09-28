# Review guide

Review severity and findings routing live here. Contribution rules live in
[CONTRIBUTING.md](../../CONTRIBUTING.md); the normative screen contract lives in
[TUI screen assembly](tui-screen-assembly.md).

## Severity

- `blocker`: the change violates an enforced boundary or merge gate. Identify
  the failing mechanism and stop the change until it is corrected.
- `concern`: the change contradicts a documented rule without an automated gate.
- `nit`: style, naming, or phrasing.

Rank by blast radius. One boundary violation outweighs a page of nits.
Use the screen-assembly enforcement scoreboard to distinguish a gated violation
from a review-only concern; do not infer severity from the number of findings.

## Verification

Review the mechanisms that can report green without proving the intended state:

- For fixtures, check the [golden and screen-baseline contracts](../../CONTRIBUTING.md#testing),
  especially meaningful state assertions and the assertion that gates refresh.
- For package and state ownership, check the [architecture boundaries](../../CONTRIBUTING.md#architecture-boundaries-enforced).
- For screen changes, check the [five invariants and their enforcement scoreboard](tui-screen-assembly.md).
- For coverage, check the evidence produced by [the merge gate](dev-guide.md#merge-gate).
- For release and documentation changes, check the [contribution checklist](../../CONTRIBUTING.md).

Before a screen refactor, record its baseline fixture in a separate commit.
Run `mise run check`; structural changes also require
`mise exec -- go test ./internal/arch/...`.

## Closing a review

Run the applicable assurance lenses and report findings to the Third Hokage.
The Hokage classifies each finding as a deviation within the task in flight or
new work that earns its own task, and creates that task when warranted.
A review with unrouted findings is incomplete.

## Update when

Update when review severity, findings routing, or verification entry points change.
