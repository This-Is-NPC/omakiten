// Package screenlayout is the arranger a screen declares SECTIONS to instead of
// assembling a body string by hand.
//
// # The defect class this exists to make inexpressible
//
// Waves 1-5 of the layout review closed nine defects and every one of them was
// the same shape: a component held a private copy of a number another component
// already knew, and the two disagreed.
//
//   - `kit.Height - 10` — a private copy of the host's chrome height.
//   - `2 + studioFlowRow` against a bordered table — a private copy of a row's
//     line offset, computed as though one item were one line.
//   - `firstLineContaining` grepping rendered text for the cursor — a private
//     re-derivation of a line the renderer already knew.
//   - `total - viewport` — a private copy of the max scroll offset that forgot
//     the hint row the renderer reserves.
//   - six copies of `AvailableWidth() - chrome`, and six of thirteen chrome
//     constants simply wrong, every one under-charging.
//   - `panelChrome = 7` hidden in a `const` where a gate could not see it.
//
// A screen that talks to this package cannot write any of them, because it is
// never handed the inputs. It does not compute a width: the arranger gives it
// one. It does not compute a row budget: the arranger gives it one. It does not
// slice its own items, hold its own scroll offset, or locate its own cursor
// line: the arranger does all three and reports the answers back on
// [Placement]. What a section produces is CONTENT — strings, and the item
// boundaries between them. Every number is the arranger's.
//
// # What a screen declares
//
// A [Section] is a [Spec] (identity, minimum width, row bounds, scroll policy)
// plus a Render that turns a [Canvas] into a [Block]. The Canvas carries the
// width AND the rows together, so there is no way to start producing lines
// without having been told the budget for them — that is why rendering is one
// phase and not a Measure/Render pair (see [Section]).
//
// A Block is item-indexed, never line-indexed: `Items` are whole selectable
// units that may be one line or twelve, and the cursor addresses an ITEM. That
// is the assumption whose absence produced the Flow cursor defect against a
// bordered table.
//
// # What a screen declares about its columns
//
// A body goes side by side when every section opts in with [Spec.Column] and
// every COLUMN's minimum width fits at once. Three fields shape what happens
// then, and all three were added because the two screens this package was
// designed against already had the shape and no way to say it (#2446):
//
//   - [Spec.Group] stacks sections inside one column, which is what
//     `[form over subtasks] | [activity]` needs. Members keep their own scroll
//     surface, their own cursor and their own turn in the focus cycle — grouping
//     is a layout statement and nothing else.
//   - [Spec.WidthPercent] sizes a column as a share of the available width
//     rather than as a share of the surplus. The two are different functions and
//     no weight reproduces a percentage across two geometries.
//   - [Spec.ColumnGap] is the blank columns between columns, defaulting to the
//     one that shipped.
//
// Everything else about a column is unchanged: minimum plus weighted surplus,
// capped by [Spec.MaxWidth], spending the available width exactly whenever
// nothing refuses its share.
//
// # Where the state lives
//
// Screens are Elm value types, so [State] is a value the screen embeds and
// carries across frames. Offsets and cursors are moved in `Update` — by
// [State.HandleKey] for the standard keys and [State.Resync] on resize — and
// merely READ by `View`. [Arrange] is pure: it clamps for the frame it is
// rendering without persisting anything, so a `View` before the first `Update`
// still cannot overdraw.
//
// A [State] also carries the MEASUREMENT of the body the last resolve took, so
// a keystroke does not have to take it again and the only resolve left per
// keystroke is the one that paints. It carries it the same way it carries the
// offsets: frozen, copied on write, and dropped by every mutation. [Arrange]
// never consults it for anything a section might now say differently, so
// nothing it remembers can reach the screen. See [frame].
//
// # Chrome ownership — the boundary, stated
//
// Recorded against task #2424, comment 123421: screens source card and column
// chrome through two unrelated paths — `kit.Styles` for 4 of 21 packages and
// injected `Deps` for the other 17 — and leaving the section contract's
// relationship to that split implicit would let nine migration cohorts each
// calcify whichever path their screen already used.
//
// The decision is that **this package does NOT subsume chrome**. A section's
// appearance stays the screen's, through whichever of the two paths that screen
// already uses. The reasons:
//
//  1. Subsuming appearance would make a 21-package chrome unification a
//     PREREQUISITE of a layout migration, coupling two migrations that have no
//     technical reason to be coupled and no shared risk.
//  2. To render a card the engine would need the theme and all seventeen `Deps`
//     shapes, which is a screen concern by construction — it would stop being a
//     leaf and the import gate below would have to be deleted to allow it.
//  3. The split never caused a layout defect on its own. What it caused was
//     wrong NUMBERS: a screen that changed its chrome through one path and then
//     hand-updated a row constant that lived on the other.
//
// So the boundary is drawn on that last point, and it is drawn where it cuts:
//
//	APPEARANCE is the screen's — it renders the bytes of its headers, footers
//	and items, from kit.Styles or from injected Deps, and this package never
//	styles anything.
//
//	OCCUPANCY is this package's — how many rows and columns those bytes get,
//	and how many they cost. And it is MEASURED from the strings the screen
//	actually produced, never declared. A section cannot tell the arranger that
//	its header is one row; the arranger measures the header at the section's
//	width and charges what it finds, exactly as [screenkit.Chrome] does.
//
// Owning occupancy means owning both ends of it. A section that overdraws its
// allocation is the defect this package was built for; a section that leaves
// rows of it blank while hiding content is the same harm arriving from the
// other direction, and it is the arranger's to fix for the same reason — the
// screen is never told how many items will fit. So the arranger previews the
// items just outside each edge of the window (see [Placement.LeadingPartialRows]),
// exactly as cardlist has always done, and a section spends what it was given.
//
// The consequence for waves 8-16 is that a cohort changing a card's LOOK still
// has to know which of the two paths its screen uses — that question is not
// answered here and this package does not make it worse. A cohort changing a
// card's SIZE no longer has to know anything: no row constant exists to update.
//
// # Import position
//
// The package is a leaf. It may import screenkit, scrollwindow, gridtable,
// linelist and cardlist, and never a screen package or the host. Enforced by
// TestPackageIsAnImportLeaf, which is itself proved against a seeded violation.
package screenlayout
