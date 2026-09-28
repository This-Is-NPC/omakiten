package main

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/screenstate"
)

// ---- screenstate ------------------------------------------------------------

// screenstateDemo exhibits the three states a screen can be in before it has a
// body, and — more to the point — the ONE precedence that decides between them.
// The three live flags are separate props on purpose: turn two of them on and
// the resolver, not the reading order, picks what paints.
type screenstateDemo struct {
	props []prop
}

func newScreenstateDemo(demoCtx) demo {
	return screenstateDemo{props: []prop{
		choiceProp("failed", "err != nil — the highest-precedence state", 1, "yes", "no"),
		choiceProp("unavailable", "Failed with no error value — a dependency that is not there", 1, "yes", "no"),
		choiceProp("loading", "the screen has asked and not been answered", 1, "yes", "no"),
		choiceProp("empty", "the answer came back with nothing in it", 1, "yes", "no"),
		textProp("kicker", "the screen identity every state is crowned with", "plans"),
		textProp("error", "err.Error() — lands in Detail, in the Hint tone", "dial tcp 127.0.0.1:5432: connection refused"),
		textProp("hint", "Vazio only — the line that teaches the interface", ""),
	}}
}

func (d screenstateDemo) Resize(demoCtx) demo             { return d }
func (d screenstateDemo) Update(tea.KeyMsg, demoCtx) demo { return d }

func (d screenstateDemo) on(name string) bool { return propLabel(d.props, name) == "yes" }

// candidates is exactly the shape a migrated screen writes: every state it can
// be in, handed over unordered.
//
// Failed and Unavailable are both here so the pair can be compared side by
// side: they resolve to the SAME kind and the same tone, and differ only in
// whether there is an err.Error() under the message. Turn Unavailable on with
// everything else off and the panel is a failure with no Detail line — which is
// what Stats, Insights and Logs paint, and what they could not paint at all
// while Failed was the only door and `available()` answered with a bool.
func (d screenstateDemo) candidates() []screenstate.State {
	id := screenstate.For(propString(d.props, "kicker"))
	var failure error
	if d.on("failed") {
		failure = errors.New(propString(d.props, "error"))
	}
	return []screenstate.State{
		id.Failed(failure, "Could not load the plan list."),
		id.Unavailable(d.on("unavailable"), "Plans are not available for this project."),
		id.Loading(d.on("loading"), "Loading plans…"),
		id.Vazio(d.on("empty"), "No plans yet.", propString(d.props, "hint")),
	}
}

func (d screenstateDemo) View(c demoCtx) string {
	body, ok := screenstate.Resolve(c.kit, c.kit.PanelContentWidth(), d.candidates()...)
	if !ok {
		return c.kit.Panel(c.kit.Styles.Hint.Render("no state is live — the screen paints its real body"))
	}
	return c.kit.Panel(body)
}

func (d screenstateDemo) Status(demoCtx) string {
	state, ok := screenstate.Winner(d.candidates()...)
	if !ok {
		return "winner: none · the screen paints its body"
	}
	return fmt.Sprintf("winner: %s · kicker %q", kindName(state.Kind()), state.Kicker)
}

// kindName is the gallery's own spelling of a Kind. The component deliberately
// ships no String(): a name for an operator would be a paint literal in a leaf,
// and a name for a developer belongs where developers read it.
func kindName(kind screenstate.Kind) string {
	switch kind {
	case screenstate.Failed:
		return "Failed"
	case screenstate.Loading:
		return "Loading"
	default:
		return "Vazio"
	}
}

func (d screenstateDemo) Props() []prop { return d.props }

func (d screenstateDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d screenstateDemo) Help() []key.Binding { return statelessHelp() }
