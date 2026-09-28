//go:build ruleguard

// Package rules holds this repo's custom lint rules, evaluated by ruleguard
// through gocritic. They are not compiled into any binary: the build tag keeps
// them out of every normal build and golangci-lint interprets the file.
//
// These rules live in a function BODY on purpose. Anything visible in a
// signature is already held by internal/arch's component API golden, and one
// rule with two mechanisms is how two checks end up disagreeing.
package rules

import "github.com/quasilyte/go-ruleguard/dsl"

// componentPaintsItsOwnColour catches a component deciding a colour instead of
// taking one from the theme projection it was handed.
//
// The theme is resolved once, by the host, and reaches a component as
// screenkit.Styles. A component that builds a coloured style has made a second
// source of truth for that colour, which is how two surfaces end up disagreeing
// about what NORMAL looks like — the defect already written down on
// screenkit.Styles about the badge contract.
//
// Two narrowings, both paid for by a false positive the broad version produced:
//
//   - It matches the COLOUR setters, not lipgloss.NewStyle() alone.
//     `lipgloss.NewStyle().Width(n)` carries no theme — it is a geometry helper
//     — and three of the six things the first draft reported were exactly that.
//   - It requires a LITERAL colour. The second draft reported keyfooter's
//     `Foreground(color("primary", "#39FF14"))`, where `color` reads
//     theme.Colors[key] first and only falls back to the hex. That is a resolved
//     theme value with a default, not a decision, and calling it a violation is
//     the same mistake the componentisation plan records its literal census
//     making four times: a fallback is not a hardcoded value.
//
// A rule that cries wolf on a width or on a fallback gets silenced, and a
// silenced rule protects nothing.
func componentPaintsItsOwnColour(m dsl.Matcher) {
	m.Match(
		`lipgloss.NewStyle().Foreground(lipgloss.Color($s))`,
		`lipgloss.NewStyle().Background(lipgloss.Color($s))`,
		`lipgloss.NewStyle().BorderForeground(lipgloss.Color($s))`,
	).
		Where(m["s"].Text.Matches(`^"`) &&
			m.File().PkgPath.Matches(`internal/tui/components/`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`component decides a colour: take the style from screenkit.Styles instead of building one`)
}

// memoKeyedOnRows catches a BlockMemo keyed on canvas.Rows().
//
// A memo keyed on Rows() never hits. One keystroke resolves a section at two
// heights — the allocation, then the taller re-render reclaimSlack gives it —
// so a rows-keyed entry is evicted by the second render and rebuilt by the
// first, forever, at full cost, with a memo in the code to say it was handled.
// That trap is written down on screenlayout.BlockMemo; this rule is the analogue
// of elm-review's Html.Lazy check, so it fails at the editor rather than at
// a keystroke budget that moved for a reason nobody can see.
//
// Only the KEY argument is in scope. Rows() inside the build closure is the
// padding that belongs outside the memo, and a Rows() after the call is the
// same. The match is the first argument of a three-argument .Block( or
// .BlockMany(, including a composite literal nested there. Named-key data flow
// is held by the architecture scanner, which can inspect declarations across a
// package.
func memoKeyedOnRows(m dsl.Matcher) {
	m.Match(
		`$x.Block($key, $inputs, $build)`,
		`$x.BlockMany($key, $inputs, $build)`,
	).
		Where(m["key"].Text.Matches(`\.Rows\(\)`) &&
			m.File().PkgPath.Matches(`internal/tui/`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`BlockMemo key includes Rows(): a memo keyed on canvas.Rows() never hits — key on what the composition reads, pad with rows outside the memo`)
}

// styleRenderInLoop catches a lipgloss Style painted inside a for-body.
//
// Painting a style once per line is the family of cost that put Studio ›
// Commands' prompt preview at forty-one milliseconds a keystroke. The known
// sites today are not bugs — n is small — so the LINE-LEVEL allowlist lives in
// internal/arch (style_render_loop_boundary_test.go), where it expires in both
// directions: adding the correction without deleting the line fails, and a line
// pointing at code that no longer exists fails. An allowlist that does not
// shrink is debt wearing a gate's clothes; each line leaves when that screen is
// visited (wave 2 of step-2).
//
// Ruleguard cannot hold that allowlist without either raising the lint baseline
// (M9) or silencing with //nolint, which this repo has zero of. So this rule
// skips the packages whose remaining lines are on the Go allowlist, plus board,
// where card.Painter.Render takes one argument and is a box paint, not a Style.
// When a package's last allowlist line goes, drop it from the skip too — a skip
// that outlives the work is the same debt. New files, and files in packages
// already cleaned, are what this lint net is for.
//
// One wolf paid before the rule stood: matching every .Render( inside a for
// reports board's painter.Render, which is card.Painter painting a spec, not a
// style-per-line. The skip is that payment, same as the colour rule's Width-only
// NewStyle and the theme-fallback hex.
func styleRenderInLoop(m dsl.Matcher) {
	m.Match(
		`for { $*body }`,
		`for $_; $_; $_ { $*body }`,
		`for $_ { $*body }`,
		`for $_ := range $_ { $*body }`,
		`for $_, $_ := range $_ { $*body }`,
		`for range $_ { $*body }`,
	).
		Where(m["body"].Contains(`$x.Render($*_)`) &&
			m.File().PkgPath.Matches(`internal/tui/`) &&
			!m.File().Name.Matches(`_test\.go$`) &&
			!m.File().PkgPath.Matches(`internal/tui/screens/board$`)).
		Report(`Style.Render inside a line loop: paint the style once per box, not once per line`)
}
