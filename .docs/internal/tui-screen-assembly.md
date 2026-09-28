# TUI screen assembly — the nine patterns and how they are charged

Normative. This is the "how a screen is built" contract that `components/screengrid`
presupposes, plus the runbook for refactoring one screen and the diagnostic for when
a keystroke budget moves.

It came out of the Studio closeout, which measured seven defects, and out of the
step-1 pilot, which measured what each rule costs. Every rule below says **what**,
**why**, and **how it is charged** — a rule without a mechanism is tuition this
project has already paid five times.

Live work against it: the Omakiten plan `tui-uniform-screen-assembly`.

---

## 1. The five invariants

The end state, stated as claims someone can check:

1. **No screen has an unproven keystroke budget.** A production screen package
   has an executed `Test*` function that directly registers a
   `screentest.Budgets` test with at least two distinct keys; the harness proves
   every key is handled, changes concrete screen state (or its view), and the
   final state equals the initial state after the key sequence. The allowlist in
   `internal/arch/keystroke_budget_boundary_test.go` is empty, and a fixture or
   unused helper without that test does not count.
2. **Screen body composition memos use `screenlayout.BlockMemo`.** The current
   production census finds seven screen-package consumers (see §6.5 and §6.6
   finding 7); the rows-key gate does not prohibit other derived caches.
3. **No screen declares a measure.** The vocabulary in `screenkit` is the only source.
4. **No screen computes geometry.** `screenlayout.Canvas` is the only origin.
5. **Building a new screen is declaring archetype, content and keys — nothing else.**

Invariant 5 is why single-zone screens mount on `screengrid.Cell` rather than staying
on a flat section set: `Cell` is the first shape in `screengrid`'s completeness claim,
and one entry point for all screens is the only way invariant 5 can be true.

---

## 2. The problem, in two families

The Studio closeout's seven defects are two families, and both have the same shape:
**a number the screen fabricated instead of receiving.**

| family | the fabricated number | symptom |
|---|---|---|
| **cost per keystroke** | how much content to compose per frame | 41 ms per key; holding `j` froze the TUI |
| **measure** | how many columns a zone measures | the breakpoint reinvented per screen, six times |

The **row** dimension was solved correctly first, and it is the model: the gate in
`internal/arch/chrome_budget_boundary_test.go` forbids a screen copying the terminal
height into a field of its own, and forbids passing a hand-counted chrome to
`ViewportRows`. The screen declares its blocks to `screenkit.Chrome` and the budget
falls out of measurement. That gate's comment records the score: **5 of 12 call sites**
that carried a manual constant were wrong, by 1 to 5 lines, each painting past the
terminal.

The **width** dimension got the same treatment later (P6), and the **cost** dimension
is mitigated rather than removed — `BlockMemo` is the sanctioned exit from a contract
that forces eager composition. Choosing to memoise is still discipline (§7).

---

## 3. The nine patterns

### P1 — Composition does not happen in the render

A zone's body does not compose inside `Render(Canvas)`. It composes when the content
changes — in `Update`, in `Apply`, in `Lifecycle` — and stores the result. `Render`
slices and returns.

*Why:* one keystroke renders each body two or three times (the key resolve, the frame
that paints, and the re-render `reclaimSlack` provokes). A body that is a document pays
that in full, three times per key.

*Decision (task 2534, branch A): fix `screenbody.Body`; do not add a parallel push type.*
The mount now carries `screenlayout.At(b.layout.Cursor(z.spec.ID))` in each block and
offers `Body.WithCursor` for a caller-driven selection. `Body.id()` remains the first
zone's stable focus target, and `Resync()` restores that focus before clamping. That
fix landed on the live type of the day, which `description` and `projectresume` then
used; it was deliberately not a second interface that no screen calls. Their view
characterization was the evidence needed before the later migrations (including the
projectresume baseline from task 2529).

*Superseded at HEAD (measured at `30b3af64`).* Waves 6-7 moved both screens off
`screenbody`: `description` holds a `screengrid.State` (`description/description.go:18`)
and arranges through `screenlayout.ArrangeIn` (`:227`); `projectresume` holds one too
(`projectresume/projectresume.go:57`), builds a root `screengrid.Cell` (`:209`-`:210`)
and paints it with `screengrid.Render` (`:230`). `taskdetail` is not outside the mount
either — it composes its zones as a `screengrid` tree and paints them at the host box
(`taskdetail/render.go:57`). Production screen code references `screenbody` **zero**
times; the only surviving consumers are `cmd/okt-gallery/screenbody_demo.go:18` and
`taskdetail/probe_2470_falsifier_test.go`, which is a probe, not a mount. The census that
required the opposite is retired (`internal/arch/screenbody_boundary_test.go:13`); see
§6.6 finding 4. Read the paragraph above as the record of task 2534's decision, not as a
description of the tree.

*Charged by:* `screentest.Budgets`, after the fact. **The type is what prevents**
composition in `Render`; task 2589 owns the separate screengrid frame-reuse cost.

### P2 — One memo, and the key never contains `rows`

While P1 does not hold for a zone, the memo is `screenlayout.BlockMemo`, never a local
invention. `canvas.Rows()` does not go in the key.

*Why:* five screens invented five memos before `BlockMemo` existed. And a key holding
`rows` is evicted by one render and rebuilt by the other, forever, at full cost, with a
memo in the code claiming the problem was handled.

*Charged by:* `internal/arch/memo_key_rows_boundary_test.go` plus the `memoKeyedOnRows`
ruleguard rule. It is the direct analogue of elm-review's rule for `Html.Lazy`.

### P3 — One painted box is one zone

If the body draws two boxes, they are two `Cell`s inside a `Rows`.

*Why:* defects 3 and 4 of the closeout. The `tab` ring is what the layout resolved — a
body that paints two boxes while declaring one zone makes `tab` skip one and the `▸`
land on the wrong box.

*Decision (task 2562):* **goldenized census.** The defect is a runtime relationship
between the boxes a body paints and the focus ring the grid resolves. Source shape alone
cannot distinguish a second visual box inside a component from a legitimate multi-line
item, so a count-based AST gate would either miss the defect or teach reviewers to ignore
false positives. The 21 production screen packages therefore remain covered by their
`screentest.Record` baselines, with the multi-zone shapes exercised by their real-screen
stacked-path tests. The census and its denominator are recorded in §6.7.

### P4 — A read-only zone is `ScrollItems` without a cursor

Never `ScrollNone`.

*Why:* the arranger's focus ring is the sections that scroll
(`screenlayout/keys.go`, `HandleKeyIn`). A non-scrolling section that holds focus makes
the next scrollable one take the key **and the focus with it** — `j` does not go inert,
it jumps out of the zone.

*Decision (task 2562):* **explicitly review-only.** Production has two intentional
`ScrollNone` declarations, and neither is a read-only scrolling zone: `logs` uses it for
the fixed aggregate summary, while `taskdetail` uses it for the subtask carousel whose
own cursor and window are maintained by the screen. A generic gate would need an
exception for one or both, and the owner has ruled out a new allowlist. The exact census
and the review rule are recorded in §6.7.

*Charged by:* review of every new `ScrollNone` declaration, with the ownership reason
written at the declaration. **No gate.**

### P5 — Geometry comes only from the `Canvas`

No body reads `kit.AvailableWidth()`, `kit.Height` or equivalent. Width and rows arrive
together, from the arranger, already decided.

*Why:* it is the reason `Canvas` has unexported fields. The most recent screen in the
repo composed at a width it computed itself while `bodyBlock` received `canvas.Width()`
from the arranger — two numbers for the same thing.

*Charged by:* `internal/arch/section_body_geometry_boundary_test.go`, which defines the
body as the callback assigned to `Body`, the function passed to `screengrid.Cell`, or
any function receiving a `screenlayout.Canvas`. It is deliberately born green with **no
allowlist** — no wolf to teach reviewers to ignore. A `View`-level read is outside its
scope; classify before assuming a call site is a violation.

### P6 — The measure comes from the vocabulary, not the screen

`MinWidth`, `MaxWidth`, `WidthPercent` and `ColumnGap` take neither an integer literal
nor a screen-local constant. They take a named measure, defined once, in `screenkit`.

*Why:* six declarations, three archetypes, the same numbers written again. And the
precedent from §2: when the same thing happened to rows, 5 of 12 were wrong.

The measure's name says **what it is**, not **who uses it**: `feedMinWidth` in two
packages is one number tied to one screen. Comfortable reading width, feed floor, list
floor, panel floor, zone gap — each exists once.

*Charged by:* `internal/arch/width_measure_boundary_test.go`. **Paid** — zero literals
remain in non-test screen code.

### P7 — Archetype before a loose `Spec`

The body shapes are constructors, the way `Flex()` and `Fixed()` already are for rows. A
screen that is `list | inspector` calls the constructor; it does not redeclare the pair.

*Why:* changing the breakpoint must be one edit, not six. Measured: it is now 1, was 6.

*Charged by:* P6 forces most of it — with no local vocabulary, hand-assembly is expensive
and visible. The rest is review.

### P8 — Style per box, not per line

`Style.Render` is not called inside a row loop. The piece is painted once.

*Why:* cause (a) of the closeout's §4.2 — `inspectorBox` called
`m.styles.Border.Render("│")` twice on each of 1500 lines, and `duffcopy` was 29% of CPU.
A lipgloss `Style` is immutable-by-copy by design, so the per-line copy is **structural**
— no library version fixes it.

*Charged by:* the `styleRenderInLoop` ruleguard rule plus the line-by-line allowlist in
`internal/arch/style_render_loop_boundary_test.go`. The allowlist expires in both
directions and shrinks as each screen is visited.

### P9 — A screen does not construct its own style

No `lipgloss.NewStyle`, `lipgloss.Color`, `lipgloss.Style` or border constructor under
`internal/tui/screens/**`. Colour and style come from `screenkit.Styles`; width and
height measurement from `screenkit.VisibleWidth` / `BlockRows`; composition from the
components.

*Why:* it is the screen-side twin of the `componentPaintsItsOwnColour` ruleguard rule,
which already forbids this inside `components/`. A screen that builds its own style is
outside the theme, so a theme change misses it silently.

*Charged by:* a `depguard` rule denying `lipgloss` under `internal/tui/screens/**`.
Installed as `screens-do-not-import-lipgloss` in `.golangci.yml` and mirrored
by `TestScreenLipglossBoundary` in
`internal/arch/screen_lipgloss_boundary_test.go`.

The shared `screenfixture` builders are not a screen and now live at
`internal/tui/screenfixture`, outside the screen tree. `screentest` remains a
test harness under `internal/tui/screens`; `_test.go` files are explicitly
excluded from depguard because they use lipgloss to measure rendered output or
configure deterministic probes, not to ship or paint production UI. The arch
gate uses the same non-test policy and has seeded rejection plus test-only
allowance checks.

---

### Two smaller rules from the same closeout

**A bordered block either closes or does not appear.** A bordered table's last line is the
line that *closes* it, so there is no line index a cut can land on and still leave a table
behind. Trimming such a block by lines produces a titled box with no bottom and a row
sliced through the middle — the defect reported against Studio › Hooks. **Trim by field
and re-render**, because re-rendering is what moves the closing border up with it.

**A table header is chrome, not content.** It does not scroll with the rows and it is not
part of the item count the arranger windows. A header counted as an item makes the last
row unreachable by exactly the header's height.

## 4. Enforcement scoreboard

Row by row against the gates at the closeout HEAD:

| pattern | mechanism | exists today? |
|---|---|---|
| P1 composition outside the render | push-shaped type + `screentest.Budgets` | budget yes; **type no** — `screengrid.Cell` takes `func(Canvas) Block`, a pull callback the arranger invokes, so composition still happens inside the render |
| P2 one body memo, no `rows` in the key | `BlockMemo` + ruleguard + arch gate | gate yes, green; `internal/arch/memo_key_rows_boundary_test.go` covers named keys and `BlockMany`; it does not classify every derived cache |
| P3 one box = one zone | goldenized census | **yes** — 21/21 production screen packages have `screentest.Record` baselines; the census is in §6.7 |
| P4 read-only is `ScrollItems` | explicit review-only | **yes** — exactly two intentional `ScrollNone` declarations, neither a read-only scrolling zone; reasons are in §6.7 |
| P5 geometry only from `Canvas` | AST/type-aware gate, no allowlist | **yes**, green over 96 files / 70 bodies; helper traversal and receiver-qualified `Kit` fields are included and no body-level violation remains (§6.1) |
| P6 measure from the vocabulary | AST gate | **yes, and paid** — green over 96 files / 59 `Spec`s |
| P7 archetype before `Spec` | P6 + review | partial |
| P8 style per box | ruleguard + expiring allowlist | gate yes; **allowlist empty — 0 entries** (§6.3) |
| P9 screen builds no style | `depguard` on `lipgloss` | **yes** — `screens-do-not-import-lipgloss`, mirrored by the arch gate |
| G1 components stay presentation-pure | `depguard` plus `TestHexagonalBoundaries` | **yes** — components deny `config`, `domain`, `app` and `sqlite`, with seeded imports rejected |
| G8 screens do not import `lipgloss` | `depguard` plus `TestScreenLipglossBoundary` | **yes** — production screen import count is zero; test-file policy is explicit |

### Closeout zero census

The closeout ledger starts from the measured values in task 2559 and ends at the
current tree. Each zero is backed by a real-tree census and a seeded rejection
case; a fixture-only or empty-tree result is not sufficient.

| census | closeout result | receipt |
|---|---:|---|
| `screensWithoutAKeystrokeBudget` | **0** (from 17) | `TestEveryScreenPackageRecordsAKeystrokeBudget`: 21 executed registrations and fixtures; missing, stale and ghost cases reject |
| screengrid mount/path violations | **0** (from 20) | `TestConcreteScreengridPaths`: 21 packages, 23 types, 11 modes, 96 files, 69 methods and 1,406 functions; disconnected, alternate-mode, dead-evidence, residual-state and false-positive cases reject |
| `screensStillHandPaintingAState` | **0** (from 8) | `TestScreenStateCensusReadsTheRealScreens`: 22 screen packages and 96 non-test sources; six adoption failures reject |
| `styleRenderInLoop` | **0** (from 36 across 13 files) | `styleRenderInLoopAllowlist` is empty; range, classic-for and nested-render cases reject |
| Canvas-body `AvailableWidth` / `Height` | **0** | `TestSectionBodyGeometryBoundary`: 96 files and 70 bodies; width, height, receiver, named-helper, `Cell` and transitive cases reject |
| production screen Lipgloss imports | **0** (from 53 across 12 packages) | `TestScreenLipglossBoundary`: 96 production files; seeded production import rejects and test-only measurement remains allowed |

The six receipts are deliberately separate from the non-gates in P1 and P7:
those remain documented design discipline, not zeros manufactured by an
allowlist or an empty scan.

Adjacent gates in the same family: width measure, section-body geometry, navigation key,
stacked path, keystroke budget, chrome budget, row budget, paint literal, screengrid mount,
screenstate adoption, golden refresh, i18n, gallery coverage, component API, operation
surface. Allowlist excuses are identity-bound (`internal/arch/allowlist_excuse.go`), not
position-bound.

Three of those no longer say what an earlier edition of this list said they said.

**The screenbody mount census is retired.** `2d72c76b` deleted it because plan 175 left it
matching nothing; the live concrete-path gate is `TestConcreteScreengridPaths`
(`internal/arch/screengrid_boundary_test.go:20`), and what survives in
`screenbody_boundary_test.go` is the bypass-rejection case at line 43.

**gofmt did not hold the line through this plan**, so it is dropped from the sentence
above. gofmt *is* gated — `internal/arch/gofmt_boundary_test.go`, installed by `ad2979d5`
on 2026-08-07, well before this wave — which makes the breach a red gate rather than a
formatter consulted on the way out. `55228a04` re-indented `internal/tui/screens/logs/*.go`
with spaces and several commits carried mis-grouped imports; `383838e0` reformatted **19
files** to clear it, so the gate was red across five commits (`55228a04`, `3c62b736`,
`4d2ae083`, `93f6a536`, `a38c5016`). It is clean at HEAD — `gofmt -l ./internal ./cmd`
prints nothing.

**The stacked-path census claimed 21/21 one commit early.** `30b3af64` widened
`stackedPathMarkers` to `screengrid.Cell(` / `screengrid.Rows(` and its message reported 21
breakpoint bodies, 21 covered — but `stats/stacked_path_test.go` had not been staged, so
`git grep -l TestStackedPathInvariants 30b3af64 -- 'internal/tui/screens/**'` returned
**20** files and `assertStackedPathCensus` would have failed `stats` on `body && !scan`
from a clean checkout. `eeb9475e` committed the scan and the census is genuinely 21/21 at
HEAD. Note also that the gate has **no allowlist at all** — there is no
`stackedPathAllowlist` symbol anywhere in `internal/arch`; it is a pure XOR, which is
stronger than an empty allowlist and should be described as such.

---

## 5. Runbook — refactoring the next screen

1. **Record the baseline first.** Four lines and one `-update`, in its own commit,
   before touching anything. Without it the refactor measures nothing.

2. **Do not trust a number whose fixture you have not read.** A short fixture produces a
   number that *looks* like it has a known cause. An offset that stops at 4 may be the
   clamp at `MaxOffset` on a 17-item document, not the bug you are hunting.

3. **A golden shows WHERE it stopped; only a test shows HOW MUCH each key moves.** They
   are different contracts and only the second is a contract. In the step-1 pilot the
   author compared goldens at three geometries and concluded "only the hint moved" three
   times; a step test overturned it on the first run. When you swap a screen's scroll
   engine, pin the step by test — that the key does not skip an item, that `pgdn` and
   `pgup` undo each other, that the page moves more than one line. No magic number: a
   magic number only records today's behaviour.

4. **Refactor.** If the number rises, the cause is one of four:
   - style rendered per line instead of per box (P8);
   - the whole document composed per frame → `BlockMemo`, with no `rows` in the key (P1/P2);
   - expensive state re-derived per body (clone, diff, resolve);
   - `Resync` per keystroke because the screen holds a cursor that belongs to the grid.

5. **Zones:** one painted box = one zone (P3). Two boxes are two `Cell`s inside a `Rows`,
   or `tab` skips one and the `▸` lands on the wrong one.

6. **Read-only:** `ScrollItems` without a cursor (P4). Never `ScrollNone` — the arranger
   steals the focus.

7. **Two commits, never one.** `test(<screen>): record keystroke budget` (fixture only,
   allowlist line untouched), then `refactor(<screen>): mount the body on screengrid`
   (deletes the allowlist line). Deleting an allowlist line in the same commit that
   records a budget claims a mount that did not happen.

8. **Any commit that moves a `.view.golden` or a `keystroke.budget.golden` row carries the
   explanation in its body** — in either direction. A fall needs explaining as much as a
   rise; an unexplained fall is how a regression hides behind another screen's win.

9. **Budget receipts are compared per row, never by aggregate sum (S7).** Keep the
   row name and allocation value as the identity-bound unit of evidence. A closeout
   must report every changed row and explain its movement; adding or removing rows
   must not hide a regression in an unchanged aggregate total. At HEAD the Logs
   receipt is therefore the individual `feed-scroll 4954` row, not a sum of the
   screen's budget rows.

10. **Pay the host's leading row exactly once.** `screenlayout.Arrange` enters the shared
   resolver with `lead=true`: it arranges inside `screenlayout.HostBox(kit)`, which
   subtracts the opening row from the content box, then emits that blank row and reports
   `Result.BodyRows` as the full host budget. `screenlayout.ArrangeIn` enters the same
   resolver with `lead=false` because its caller already owns an explicit `Box`; a nested
   box must not spend another screen-opening row. A root `screengrid.Cell` therefore needs
   one explicit `"\n"` at the host mount (before the usual indentation), while nested cells
   must not add one. The receipt is
   `TestRootCellMountMatchesHostArrangeIncludingLeadingRow`
   (`internal/tui/components/screengrid/leading_row_test.go:14`), committed as `c3adc926`
   after being found untracked.

   **This debt is paid, not pending.** Measured at `30b3af64`, no production screen calls
   `screenlayout.Arrange` at all — the host-path resolver's only live callers are
   `screenbody/body.go:266` and `cmd/okt-gallery/arranger.go:67,129`. Fourteen screens
   mount at the host box and every one of them already emits the row, in one of two
   spellings:

   | spelling | screens (site) |
   |---|---|
   | `screenkit.Indent("\n"+view, 2)` | `board` (`board.go:474`), `commentdetail` (`commentdetail.go:212`), `description` (`description.go:129`), `entitydetail` (`entitydetail.go:192`), `plans` (`plans.go:216`), `studio` (`commands.go:75`, `hooks.go:106`, `personas.go:112`, `workflow.go:90`), `taskdetail` (`render.go:58`), `taskform` (`taskform.go:236`) |
   | `"\n" + screenkit.Indent(view, 2)` | `entitylist` (`entitylist.go:383`), `graph` (`render.go:20`), `home` (`layout.go:114`), `logs` (`render.go:36`, `:40`), `project` (`project.go:498`, `:727`), `stats` (`render.go:33`) |

   The two spellings are byte-identical because `screenkit.Indent` leaves empty lines
   empty (`internal/tui/components/screenkit/text.go:273`, `if line != ""`), so the empty
   first line the `"\n"` creates is never prefixed either way. Pick one; do not "fix" the
   other, and do not read the difference as a defect.

   The remaining seven screens mount inside a panel and must **not** add a row.
   `insights` (`render.go:36`), `relationshippicker` (`relationshippicker.go:240`),
   `settingspicker` (`settingspicker.go:194`) and `table` (`render.go:23`) return
   `kit.Panel(screengrid.Render(kit, …, s.panelBox(kit), …).View)`, and `projectresume`
   returns `kit.Panel(s.gridResult(frame).View)` (`projectresume.go:167`), which is the
   same call one hop away (`:230`). `kit.Panel` is
   itself `"\n" + Indent(k.PanelBox(content), 2)` (`screenkit/kit.go:170`) — the same law,
   spelled a third time, one layer down. `settings` returns `screenkit.Indent(body, 2)`
   against its own `bodyBox` (`settings.go:148`), and `plannetwork` builds its own frame
   (`network.go:232`).

   An earlier edition of this item listed seven screens that "still use the host path and
   owe that newline during migration", citing `board.go:469`, `commentdetail.go:213`,
   `entitydetail.go:181`, `plans.go:197`, `taskform.go:240`, `project.go:455` and
   `render.go:59`. All seven already paid it and none of the seven line numbers resolved.
   That table is withdrawn.

   A byte-identical view golden is the expected result of a migration here; a moved golden
   is a real falsification, not a row to refresh quietly. If a screen used to read
   `screenlayout.Result.BodyRows`, the answer under composition is the package function
   `screenlayout.BodyRows(kit)` (`screenlayout/budget.go:44`); `screengrid.Result` has no
   `BodyRows` field (`screengrid/grid.go:37`-`:45`).

---

## 6. Census — geometry and style boundaries

Every figure below was re-measured over production (non-`_test.go`) files under
`internal/tui/screens/`, at `30b3af64` and again after `71c4cf26` and `eeb9475e` landed
the `stats` starving-floor fix and its stacked-path scan. The totals did not move; the
`stats` line numbers did, and the numbers quoted are the later ones. The commands are
named so each one can be re-run.

The census is the migration ledger for the P5/P8 gates. It distinguishes body paths from
outer chrome and pre-mount `View` geometry: the `section_body_geometry_boundary_test.go`
rule scopes the prohibition to section/body callbacks and functions receiving
`screenlayout.Canvas`, not every width read in a screen package.

### 6.1 `kit.AvailableWidth()` classification

`git grep -n 'AvailableWidth()' -- 'internal/tui/screens/**/*.go' 'internal/tui/screens/*.go' |
grep -v _test.go` returns **12 production references: five executable reads and seven
historical comments**. The executable reads are all outer or pre-mount geometry; the
Canvas-body gate follows them transitively and finds zero violations.

| package | executable reads | comment-only |
|---|---|---|
| `board` | `board.go:463` (`emptyHint`) | — |
| `commentdetail` | — | `commentdetail.go:250` |
| `entitylist` | `layout.go:163` (`columnWidths`) | — |
| `graph` | `render.go:14` (empty-state hint box) | `fixture.go:145` |
| `project` | `project.go:688` (form-body cache key) | — |
| `projectresume` | — | `projectresume.go:200` |
| `stats` | `render.go:212` (outer summary tables) | — |
| `table` | — | `layout.go:45`, `render.go:26-27` |
| `taskform` | — | `fixture.go:22` |
| **total** | **5** | **7** |

**None of the five executable reads is a P5 gate violation.** `TestSectionBodyGeometryBoundary`
is green over 96 files and 70 bodies with an empty allowlist. The body callbacks receive
their width from `screenlayout.Canvas`; the five reads above are outer chrome or keys
formed before the body callback. The historical labels that called the removed `home`,
`insights`, `logs`, `relationshippicker`, `settingspicker`, `table`, `taskform` and
`project` sites body violations are withdrawn.

**What the gate checks transitively.** The gate follows local helper calls from each
Canvas body across the screen package. Task 2600 closed the former helper bypasses by
passing the Canvas width through the helper chain; the current transitive scan is green
with no allowlist.

The five executable reads are outer or pre-mount geometry and are sanctioned:
`board/board.go:463` paints empty chrome; `entitylist/layout.go:163` feeds the
`contentBox` handed to the grid; `graph/render.go:14` is empty-state `View` chrome;
`project/project.go:688` keys a pre-mount form cache; and `stats/render.go:212` sizes
outer summary tables. None is a Canvas body read.

The seven comments are retained as provenance and counted as comments, not violations.

### 6.2 `kit.Height` classification

`git grep -n 'kit\.Height' -- 'internal/tui/screens/**/*.go' 'internal/tui/screens/*.go' |
grep -v _test.go` returns **11 production reads**. None is inside a Canvas body.

Nine are the unmeasured-host fallback in panel-box layout helpers, all spelled
`if kit.Height <= 0`: `graph/layout.go:74`, `home/layout.go:74`, `logs/layout.go:195`,
`plans/layout.go:128`, `projectresume/projectresume.go:184`,
`relationshippicker/layout.go:87`, `settingspicker/layout.go:87`, `stats/layout.go:184` and
`table/layout.go:102`. All are pre-mount chrome geometry and sanctioned.

The other two are `plannetwork/network.go:306`, an outer panel fallback, and
`plannetwork/section.go:62`, where the host height sizes an outer box. **No
`screenlayout.Canvas` body reads `kit.Height`**, which the P5 gate independently confirms.


### 6.3 P8 style-in-loop ledger

`styleRenderInLoopAllowlist` (`internal/arch/style_render_loop_boundary_test.go`) contains
**zero entries**. The previous edition claimed 20 entries across eight files. The empty
map is not an exemption: `TestStyleRenderInLoopAllowlistStaysHonest` and
`TestStyleRenderInLoopRuleguardSkipStaysHonest` reject stale entries or a ruleguard skip
that outlives its last finding, while the seeded cases still reject loop rendering.

### 6.4 Preserved §4.1 extracts (source citation)

The source `.temp/plano-tui.md` was deleted. The §4.1 gate text needed by task 2561
is preserved here so the citation remains followable:

> **G1:** “The parent plan's §4.1 measured **zero** violations of
> `components/**` does not import `config` / `domain` / `app` / `sqlite` — and
> there is no `depguard` rule holding it. A clean state with no gate is one
> careless import from regressing silently.”

> **G8:** “Done when: `depguard` denies `lipgloss` under
> `internal/tui/screens/**` with the harnesses AND the test-file question
> explicitly resolved and the reason written on the rule; the components-purity
> rule is installed; both are mirrored in `internal/arch/arch_test.go` per the
> note at the top of `.golangci.yml` (‘Edit both files together’); and the screen
> sites are zero.”

The same §4.1 measurement records **53 production `lipgloss.` lines across 12
packages**, with `plannetwork` and `taskform` accounting for **27**, and classifies
the calls as style construction (15 `Style`, 10 `Color`, 9 `NewStyle`, 2
`NormalBorder`, 1 `TerminalColor`), measurement (20 `Width`, 6 `Height`) and
composition (2 `JoinHorizontal`, 2 `Top`, 1 `Center`). This is the G8 baseline,
not an excuse to leave the sites unowned.

At task 2561 closeout, `TestScreenLipglossBoundary` inspects **96 production
screen files** and finds **0 forbidden imports**. `TestHexagonalBoundaries`
inspects **60 production component files** for G1, and its seeded cases reject
`config`, `domain`, `app` and `sqlite` imports. Both scans refuse an empty tree;
the screen scan also proves that a lipgloss import in `_test.go` remains outside
the deliberate rendered-output measurement policy.

### 6.5 Adoption, measured four ways

`TestConcreteScreengridPaths` (`internal/arch/screengrid_boundary_test.go:20-29`)
is the current adoption gate. At HEAD it inspects **21 production screen packages,
23 concrete screen types, 11 explicit mode values, 96 production files, 69 required
screen methods and 1,406 functions**. Every inspected concrete path has a connected
body, at least one `screengrid.Render` site and at least one
`screengrid.State.HandleKey` site; the gate also rejects disconnected imports,
alternate-mode bypasses, dead or split control-flow evidence, executable
`WithLayout`, and bare `screenlayout.State` residuals
(`internal/arch/screengrid_boundary_test.go:606-638`).

The package-level census agrees with that gate. Run from the repository root:

```bash
printf 'screen packages: '
for d in internal/tui/screens/*/; do case "$d" in */screentest/|*/screenfixture/) continue;; esac; basename "$d"; done | wc -l
printf 'screengrid imports: '
for d in internal/tui/screens/*/; do case "$d" in */screentest/|*/screenfixture/) continue;; esac; rg -l 'components/screengrid' "$d" --glob '*.go' --glob '!**/*_test.go' >/dev/null && basename "$d"; done | wc -l
printf 'grid key routing: '
for d in internal/tui/screens/*/; do case "$d" in */screentest/|*/screenfixture/) continue;; esac; rg -l 'grid\.HandleKey\(' "$d" --glob '*.go' --glob '!**/*_test.go' >/dev/null && basename "$d"; done | wc -l
printf 'grid rendering: '
for d in internal/tui/screens/*/; do case "$d" in */screentest/|*/screenfixture/) continue;; esac; rg -l 'screengrid\.Render\(' "$d" --glob '*.go' --glob '!**/*_test.go' >/dev/null && basename "$d"; done | wc -l
```

The four counts are **21, 21, 21 and 21**. Current examples of the previously
contested paths are `commentdetail/layout.go:50-56` and
`entitydetail/layout.go:36-39`, which pass line blocks to `screengrid.Cell`;
`board/board.go:207-215` routes all motion to the grid while its lane memo is
declared in `board/layout.go:106-124`. `board/board.go:397` uses
`screenlayout.ArrangeIn` only to inspect a placement returned by `screengrid.Render`,
not as a second body or key-routing path.

The budget census is complete as well: there are **21 executed `Test*`
registrations of `screentest.Budgets` and 21 keystroke budget fixtures** and
`screensWithoutAKeystrokeBudget` is empty (`internal/arch/keystroke_budget_boundary_test.go:77-89`).
The reproducible gate is:

```bash
go test ./internal/arch -run TestEveryScreenPackageRecordsAKeystrokeBudget -count=1
```

This proves executed-test registration and fixture coverage. Each registered case is
also executed by `screentest.Budgets`: every key must be handled and must change
concrete screen state or its rendered view, and the complete key sequence must return
to its initial state; the per-screen allocation values remain in each
`internal/tui/screens/*/testdata/keystroke.budget.golden` fixture.

The screen-state census uses parsed production ASTs rather than a source marker. It
recognises `Panel` calls whose styled argument is `Styles.Error` or `Styles.Hint`,
including a local style alias, while ignoring comments and strings. The paired import
census resolves the `screenstate` import from import declarations, so changing
whitespace, aliases, or the spelling of an unrelated string cannot silently turn a
hand-painted state into adoption evidence.

#### Why the current gate is an AST contract

The old package-qualified grep measured neither method calls nor alternate concrete screen
types. The live gate instead walks parsed production files, carries `screengrid.State`
fields across files, and checks each concrete `Screen` path. This is why its denominator is
23 types rather than one row per package, and why Studio's four surfaces appear in one
package while still being checked as grid paths (`internal/arch/screengrid_boundary_test.go:102-123,
197-204`).

The residual check is intentionally narrower than a text search: it records executable
`WithLayout` selectors and bare `screenlayout.State` selectors from production ASTs
(`internal/arch/screengrid_boundary_test.go:587-603`). The current result is empty. The
remaining source-text matches are explanatory comments in `board`, `entitylist`, `graph`,
`plannetwork` and `taskform`; test fixtures retain `WithLayout` only to prove the seeded
contract rejects it. A raw `rg -n 'WithLayout' internal/tui/screens --glob '*.go'` therefore
must not be reported as an executable-use count.

#### The recipe

The package-level shell form above is the re-runnable census. The architecture gate is the
authoritative AST measurement:

```bash
go test ./internal/arch -run TestConcreteScreengridPaths -count=1 -v
```

The expected denominator line is `packages=21 types=23 modes=11 files=96 methods=69
functions=1406`, followed by one connected body/render/key line for each concrete path.

All previously contested routes are now covered by the same contract. The two document
readers hand composed line items to `screengrid.Cell` (`entitydetail/layout.go:14-39`,
`commentdetail/layout.go:17-56`), the Board's windowed lanes are declared through the
grid (`board/layout.go:127-135`), and every alternate mode is included in the concrete
path census. There is no two-screen exemption and no remaining adoption open question.

The post-#2600 frame-preservation audit is also closed: executable production
`WithLayout` uses are zero. The source-text command
`rg -n 'WithLayout' internal/tui/screens --glob '*.go' --glob '!**/*_test.go'` returns
six explanatory comments; the only executable matches are seeded/test-only cases, and
`TestConcreteScreengridPaths` rejects production residuals. This distinction is why the
document reports an AST result rather than a raw text-match count.

### 6.6 What this closeout learned

Eight findings, each traceable in the git log or by a named command.

**1. A block whose height depends on `canvas.Rows()` is not memoisable.** `4d2ae083` put
`taskdetail`'s sub-task board behind a `screenlayout.BlockMemo` keyed on
`taskDetailBlockKey{width: canvas.Width(), …}` while the block was **elastic in rows** —
`renderSubtaskBoard` pads its lanes out to fill `canvas.Rows()`. A block built during
`Open` at 16 rows was served to a 18-row paint, `screenlayout.distributeRows`
(`budget.go:57`) read the stale height off it, and the left column's 35 rows were split
19/16 instead of 17/18. `8d6752bd` **removed** the memo rather than re-keying it, because
adding `rows` to the key is precisely what §P2 forbids: such a key is evicted by one
render and rebuilt by the other, forever, at full cost. `task_detail.view.golden` passes
unchanged, which is the proof that the removal restored the pre-migration split rather
than blessing a new one. The row-independent half — the lane cards — stays memoised
(`taskdetail/render.go:586`, through `BlockMemo.BlockMany`). `a5afb338` states the licence
for the opposite case explicitly: `logs` memoises both its bodies because `canvas.Rows()`
is never called anywhere in that screen.

**2. The `rows`-in-key gate now closes named-key laundering and `BlockMany`.**
`memoKeyedOnRowsViolations` (`internal/arch/memo_key_rows_boundary_test.go`) tracks a
named key whose initializer contains `canvas.Rows()` as well as direct composite keys,
and scans both `.Block(` and `.BlockMany(`. Its seeded tests cover both bypass shapes;
the real tree remains green. The production `taskdetail/render.go:586` `BlockMany` call
is therefore held by the same contract as the other memo sites.

**3. Open opportunity — `screengrid.State.Resync` composes the body twice.**
`Resync` (`internal/tui/components/screengrid/keys.go:240`) paints the whole tree through
`walker.arrange(root, box, 0)` and then re-resolves the very same `(box, sections)` pairs
through `screenlayout.ResyncIn` one loop later. `Resync` keeps neither the view nor the
placements from the first pass, so the paint is discarded: every screen on the grid pays
one extra whole-body composition per keystroke. `a5afb338` measured a prototype removal —
then reverted it — at `logs feed-scroll 21420 → 16358` and
`entitylist card-cursor 16073 → 11252`, with **14 packages' recorded budget rows moving**.
Unscheduled. It requires Owner sign-off because it moves 14 goldens, and runbook item 8
requires every one of those moves to be explained in the commit body — in either
direction.

**4. The `screenbody` mount census was retired.** `2d72c76b` deleted it: `screenbody.Body`
has zero screen consumers, and its own anti-vacuity branch said a rule that matches
nothing is not a rule. The `screenbody` **production package** and `Body.WithCursor`
remain in the tree pending a separate decision; the only live consumers are
`cmd/okt-gallery/screenbody_demo.go` and `taskdetail/probe_2470_falsifier_test.go`.

**5. `logs feed-scroll` went out of budget and came back under it.** 15944 was the floor
recorded by `c6541471`; `55228a04` took it to 21420 (+34%) and shipped with an empty
commit body, breaching plan 175's global acceptance #2 with no explanation; `a5afb338`
brought it to **4938** by composing the feed items and the summary document once per
(buffer, width, theme). The fixture correction in `e29c03f3` reduced the row to **4918**
by gathering the token-strip tones before painting. The later allocation-receipt refresh
in `b1fd516f` recalibrated that committed row from **4978** to **4954** without changing
production code; `internal/tui/screens/logs/testdata/keystroke.budget.golden:3` therefore
reads `feed-scroll 4954` at HEAD. The measured attribution matters: the summary cost the
same ~3800 allocations in both revisions, and the whole +5476 was two extra whole-buffer
feed compositions per round trip — i.e. finding 3, paid by one screen.

**6. The stacked-path scan now reaches every screengrid body.** `30b3af64` widened
`stackedPathMarkers` (`internal/arch/stacked_path_boundary_test.go:25`) from
`ListInspector` / `Cols` / `BesideFeed` to include `screengrid.Cell(` and
`screengrid.Rows(`, which is what waves 6-7 actually declared — 18 screens had been
invisible to the census while it reported green. There is no allowlist: the gate is a pure
XOR (`body && !scan` and `scan && !body` both fail), so there is no excuse to spend. See
§4 for the one caveat: the census read 20/21 at `30b3af64` because `stats`'s scan was
still unstaged, and reached 21/21 with `eeb9475e`. Two of the new scans found real defects
rather than confirming health — `taskdetail`'s memo (finding 1), and `stats` declaring
`modelsMinRows = 3` as both the section's `MinRows` and the summary's yield threshold,
against seven rows of pinned chrome. A box of 3 to 7 rows cleared that floor with a
**zero-row item viewport**: the panel painted its crown and its total row with no model
rows between them, and because nothing was visible the arranger reported `Above = 0` /
`Below = 0`, so there was not even a "N below" hint. Reachable at 80, 120 and 200 columns
at heights 15-19, and again at 25-29 or 33-37 depending on width. `71c4cf26` split the one
constant into two floors.

**7. The body-memo consumer and gate census changed after #2600.** The current production
search, excluding tests and the `screentest` harness, reports seven screen packages using
`screenlayout.BlockMemo`: `board`, `description`, `entitydetail`, `logs`, `project`,
`studio` and `taskdetail` (`board/board.go:61`, `description/description.go:29`,
`entitydetail/entitydetail.go:52`, `logs/layout.go:85-86`, `project/project.go:628`,
`studio/inspector_body.go:152`, `taskdetail/taskdetail.go:181-182`). Re-run it with:

```bash
rg -l 'screenlayout\.BlockMemo' internal/tui/screens --glob '*.go' --glob '!**/*_test.go' \
  | rg -v '/screentest/' | sort
```

Board's old whole-view cache is gone; its current memo is the per-lane `BlockMemo`
(`board/board.go:56-61`, `board/layout.go:106-124`). Other derived caches remain in
`project`, `entitylist`, `plans`, `plannetwork`, `studio` and `settings` (the declarations
are discoverable with `rg -n 'type .*Cache|CacheEntry' internal/tui/screens --glob '*.go'`),
so the document no longer claims that `BlockMemo` is the only cache. The enforced
contract is specifically the absence of `Rows()` in `Block`/`BlockMany` keys, covered by
`internal/arch/memo_key_rows_boundary_test.go:14-20,116-143`.

**8. A gate that is only run in a dirty working tree is not run.** Two receipts in this
plan existed only as untracked files: `TestRootCellMountMatchesHostArrangeIncludingLeadingRow`,
committed by `c3adc926` after task 2590 was marked DONE without it, and
`stats/stacked_path_test.go`, still untracked at `30b3af64` while `30b3af64`'s own message
counted it, until `eeb9475e` staged it. Both would have passed for their author and failed
on a clean clone. `git status` is part of the receipt.

---

### 6.7 P3/P4 decision census

Task 2562 measured the production shapes after waves 6 and 7 rather than promoting either
pattern from a grep. The denominator is **21 screen packages** under
`internal/tui/screens/`, excluding the `screentest` harness.

#### P3 — goldenized census

There are **39 production `screengrid.Cell` call sites**. Sixteen packages have one
static Cell call site:

`board`, `commentdetail`, `description`, `entitydetail`, `entitylist`, `graph`, `home`,
`insights`, `plannetwork`, `projectresume`, `relationshippicker`, `settings`,
`settingspicker`, `stats`, `table` and `taskform`.

Five packages have multiple Cell call sites because they declare more than one body mode
or zone set: `logs` (2), `plans` (2), `project` (4, including its form mode), `studio`
(12 across its four surfaces) and `taskdetail` (3). `board` creates one Cell per workflow
lane at runtime; its windowed `Cols` is the lane carousel, not a composite body hidden in
one Cell. The explicit container census is **16 `Rows` / `Cols` / inspector-constructor
sites across eight production files**.

All **21/21** packages have a `goldens_test.go` recording through `screentest.Record`,
which captures the 80x24, 120x40 and 200x50 views and gates refresh with a non-trivial
state assertion. The real-screen stacked-path census also covers the Cell/container
shapes. These goldens are the P3 receipt: a visual shape change or a focus-state change
must update a reviewed baseline, while a source-count rule that cannot see the painted
box relationship is intentionally not installed.

#### P4 — explicit review-only census

The production scan finds exactly **two `ScrollNone` declarations**:

| site | shape | reason it is not P4's read-only zone |
|---|---|---|
| `logs/layout.go:71` | fixed aggregate summary Cell | the summary document is measured, fixed-height supplemental chrome; it is not a scrolling or focus target |
| `taskdetail/render.go:150` | subtask-board section | the board is an interactive carousel whose cursor and window belong to the screen's subtask component, so `ScrollNone` prevents the arranger from stealing that focus |

Every production read-only scrolling shape inspected uses `ScrollItems` with no section
cursor (`NoSelection` or the zero cursor), while selection-bearing lists use
`ScrollItems` with the arranger cursor. The two `ScrollNone` sites are therefore not
exceptions to a gate; they are outside the predicate. A syntactic gate cannot establish
that ownership distinction, and an identity-bound allowlist would merely encode these
two cases as excuses. Any new `ScrollNone` declaration remains review-only and must
state its non-arranger cursor or fixed-chrome ownership beside the declaration.

### 6.8 Owner boundary closeout

The closeout owner clarification is now the boundary record for the three claims that
package-level adoption cannot prove:

1. Screens contain interaction state, semantic action emission, screengrid archetypes,
   Canvas-driven layout and painting, but not repository or service I/O, traversal,
   projection, filtering, sorting, aggregation, normalization, validation, mutation
   engines or product-status policy. The extracted consumers are `internal/taskprojection`,
   `internal/plannetwork`, `internal/studioprojection`, `internal/settingsprojection`,
   `internal/taskvalidation` and `internal/relationshipprojection`; focused projection
   tests and the full screen suite pass.
2. Production screen files contain no direct Lipgloss imports or calls. G8 inspects 96
   production files, rejects a seeded production import, and deliberately excludes
   `_test.go` output-measurement probes. G1's components-purity scan inspects 60 files
   and rejects seeded `config`, `domain`, `app` and `sqlite` imports.
3. The concrete-path AST contract inspects 21 packages, 23 concrete types, 11 modes, 96
     files, 69 required methods and 1,406 functions. Every path has connected body
   construction, grid rendering and `State.HandleKey` routing; dead, split, or unrelated
   mode-branch evidence, residual executable `WithLayout`, and bare `screenlayout.State`
   paths are rejected.

Parent P2 steps 11 and 12 are represented by completed rows #2561 and #2536. Step 10 is
this plan. The stale `.temp/arquitetura.md` remains scratch analysis; this tracked document
supersedes its screen-boundary claims and no normative source is recreated under `.temp/`.

### 6.9 Re-audit closeout at wave 961 HEAD

The closeout was re-claimed after wave 961. The earlier receipt ending at `68fc4958`
and debrief #148376 are superseded by the final wave-961 chain:
`2ef98f11` -> `c00c9c02` -> `fd51664c` -> `06f7a8b8` -> `ddc75fba`.
The preceding remediation receipts remain part of the evidence: `4fd2c6d7` corrected
the Logs budget receipt, `6c7b32f3` closed invalid UTF-8 and the empty Plan Network
terminal sink, `db23b16e` hardened migration filesystem operations, `0e8f55a7` split
Windows safe-I/O access, and `68fc4958` translated Windows filesystem status values.
Debrief #148874 records this final chain and the resolved findings; independent final
reviews #148863, #148865 and #148867 report no findings.

The six closeout censuses remain zero and non-vacuous. The live receipts inspect 21/23/11
screengrid packages/types/modes, 96 production screen files, 22 screen packages, 70
Canvas bodies, and an empty style-loop allowlist with 0 production Lipgloss violations.
The local gates pass: `go test ./...`, `go test -count=1 ./internal/arch/...`, the
focused config and TUI suites, `mise run check`, unfiltered `golangci-lint run` with
0 issues, `govulncheck` with 0 reachable vulnerabilities, docs validation, and
aggregate coverage above the 78% floor.

Windows-native runtime execution remains an evidence gap, not a code finding. Linux can
cross-compile but cannot execute the Windows runtime; the Windows job in
`.github/workflows/assurance.yml` runs installer assurance and `internal/config` tests
matching `^TestWindows`, not the full Go runtime, application, or TUI suite. No full
Windows-runtime pass is claimed here.

The broad TUI/SQLite race target from audit comment #147776 was not rerun for this
closeout. Its earlier run timed out without failure output, so it remains unproven rather
than being reported as a pass.

The governance audit over the plan interval recorded 321 commits, including 19 commits
whose diff was confined to `internal/arch`; those were enforcement or gate-maintenance
commits, not screen migration or adoption closeouts. No wave in this plan closed on an
arch-only migration diff. The working tree and final closeout commit remain local; no
remote write is part of this evidence.

## 7. What stays discipline

Honestly: **choosing to memoise.** The gate says *when* it is needed and `BlockMemo` says
*how*, but nothing stops the next screen composing a document per frame — only CI, after
the fact. That is what P1's type is for, and it is not built.

---

### 7.1 Studio budget receipt

Studio's five budget rows are the allocation receipt for the component paint work, not a
projection extraction. The current values are:

| row | allocations | decision |
|---|---:|---|
| `commands-list` | **17576** | projection preparation removes command catalog traversal from body composition; selection remains grid-owned |
| `commands-preview` | **4570** | projection preparation removes catalog traversal while the resolved document remains memoised and its fixed box edge is painted once |
| `hooks-history` | **4022** | the compact history row uses the kit's width measurement and fixed box paint |
| `personas-related` | **9050** | persona relationships and role inference are prepared outside body composition |
| `workflow-list` | **21935** | workflow rows, reachability warnings, task grouping, and permission policy are prepared outside body composition |

The five rows are the current Studio fixture receipt. Projection preparation removes
reusable catalog, relationship, and workflow work from body composition, while the
hooks-history and personas-related rows retain their measured bundle and relationship
setup costs. The view goldens remain byte-identical, and this receipt follows the Studio
package fixture rather than claiming that every row changed or decreased.

## 8. What we decided not to do

**Do not migrate to Bubble Tea v2 for the renderer.** The Cursed Renderer's gain is on
**output** — bytes written to the terminal, mostly benefiting SSH/Wish. That is not our
bottleneck; ours is the *composition* of the strings before any write. Migration costs
`View() string` → `View() tea.View`, new import paths, and every view golden at risk, to
attack what does not hurt. The model worth having from that world is `Window`/`Draw`
(clipping by construction), not the diffing — and that is a separate, conditional
evaluation.

**Do not make `Block.Heights` authoritative in general.** Believing declared height gives
back the overdraw that `Section.Render`'s single phase exists to prevent. If lazy
windowing becomes necessary, the path is clipping by construction, not declared height.

**Do not swap the allocation gate for `benchstat`.** `screentest.Budgets` — minimum 12
runs, GC off, exact golden, `{j,k}` round trip, plus proof against a seeded violation in
`TestKeystrokeBudgetSeesAnEagerBody` — is stricter than the community norm and correct
for a shared runner.

**Do not replace `screenlayout` / `screengrid` with a library.** Nothing in the Go
ecosystem does declarative row/column distribution plus a focus ring plus per-id scroll
state. What the community solved is the layer below: what a zone hands the arranger.

---

## 9. External references

- [Bubble Tea v2 announcement](https://charm.land/blog/v2/) and [What's New](https://github.com/charmbracelet/bubbletea/discussions/1374)
- [charmbracelet/ultraviolet](https://pkg.go.dev/github.com/charmbracelet/ultraviolet) — `Drawable.Draw(scr, area)` over a cell buffer
- [Ratatui — rendering under the hood](https://ratatui.rs/concepts/rendering/under-the-hood/)
- [Elm — Html.lazy](https://guide.elm-lang.org/optimization/lazy.html) and [elm-review's misuse rule](https://github.com/jfmengels/elm-review/discussions/73)
- [bubbles/v2 viewport](https://pkg.go.dev/github.com/charmbracelet/bubbles/v2/viewport) — push via `SetContent`, and [PR #823](https://github.com/charmbracelet/bubbles/pull/823) for the virtual offset over wrapped lines
- [clipperhouse/displaywidth](https://github.com/clipperhouse/displaywidth) — already arrived via `x/ansi`; the remaining win is measuring once at write time, not re-measuring composed strings
- [lipgloss#562](https://github.com/charmbracelet/lipgloss/issues/562) — open unicode width bug; check against our goldens before any bump

## See also

- [architecture.md](architecture.md) — layers, structure, metrics
- [../tui.md](../tui.md) — the user-facing surfaces these patterns assemble
- [dev-guide.md](dev-guide.md) — build, test and gate commands
