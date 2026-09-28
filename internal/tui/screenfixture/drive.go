package screenfixture

import (
	"omakiten/internal/tui/screenhost"
)

// Scenario is one recorded screen state: the same Build / Bind / Keys path
// screentest.Record drives when it paints a golden. Gallery and goldens both
// mount through Drive so what you approve by eye is what the fixture records.
//
// Build must materialise a FRESH screen on every call — Drive may run twice at
// the same geometry, and the two paintings must share no mutable state.
type Scenario struct {
	Name  string
	Keys  []string
	Build func(frame screenhost.Frame) screenhost.Screen
	// Bind re-applies host-owned deps between keystrokes. Nil when the screen
	// carries none. The frame Build received is the geometry Bind should use —
	// capture it in the Build closure the way board/table/graph goldens do.
	Bind func(screen screenhost.Screen) screenhost.Screen
}

// Enter issues the LifecycleEnter the host issues before a screen's first paint.
// Several screens initialise their viewport there; skipping it records a state
// the host never shows.
func Enter(screen screenhost.Screen, frame screenhost.Frame) screenhost.Screen {
	out := screen.Lifecycle(frame, screenhost.LifecycleEnter)
	if out.Screen == nil {
		panic("screenfixture: LifecycleEnter returned no screen")
	}
	return out.Screen
}

// Drive mounts a scenario the way screentest.Paint does: Build, then each key
// through Update with Bind between messages, then a final Bind. The returned
// screen is ready for View(frame) at the geometry Drive was given.
func Drive(scenario Scenario, frame screenhost.Frame) screenhost.Screen {
	if scenario.Build == nil {
		panic("screenfixture: scenario " + scenario.Name + " has no Build")
	}
	screen := scenario.Build(frame)
	if screen == nil {
		panic("screenfixture: scenario " + scenario.Name + " built a nil screen")
	}
	for _, key := range scenario.Keys {
		screen = bindScenario(scenario, screen)
		out := screen.Update(frame, Key(key))
		if out.Screen == nil {
			panic("screenfixture: scenario " + scenario.Name + ": key " + key + " returned no screen")
		}
		screen = out.Screen
	}
	return bindScenario(scenario, screen)
}

func bindScenario(scenario Scenario, screen screenhost.Screen) screenhost.Screen {
	if scenario.Bind == nil {
		return screen
	}
	return scenario.Bind(screen)
}
