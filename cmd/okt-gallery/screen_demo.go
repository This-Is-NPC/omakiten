package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// screenDemo mounts a real screenhost.Screen through screenfixture.Drive — the
// same Build / Bind / Keys path screentest.Record uses when it paints a golden.
// The frame comes from screenfixture (Styles, Catalog, MarkdownTokens), not the
// gallery theme: what you approve here is what the fixture records.
type screenDemo struct {
	scenarios []screenfixture.Scenario
	byName    map[string]screenfixture.Scenario
	props     []prop
	mounted   screenhost.Screen
	frame     screenhost.Frame
}

func newScreenDemo(scenarios []screenfixture.Scenario) func(demoCtx) demo {
	names := make([]string, len(scenarios))
	byName := make(map[string]screenfixture.Scenario, len(scenarios))
	for i, sc := range scenarios {
		names[i] = sc.Name
		byName[sc.Name] = sc
	}
	if len(names) == 0 {
		names = []string{"(none)"}
	}
	return func(demoCtx) demo {
		d := screenDemo{
			scenarios: scenarios,
			byName:    byName,
			props: []prop{
				choiceProp("state", "recorded golden state — same Build path as screentest.Record", 0, names...),
			},
		}
		return d
	}
}

func screenScenarios(scenarios []screenfixture.Scenario) []scenario {
	out := make([]scenario, len(scenarios))
	for i, sc := range scenarios {
		keys := "entry"
		if len(sc.Keys) > 0 {
			keys = strings.Join(sc.Keys, " ")
		}
		out[i] = scenario{
			name:   sc.Name,
			note:   "golden state after [" + keys + "] · screenfixture.Drive",
			width:  120,
			height: 40,
			props:  map[string]string{"state": sc.Name},
		}
	}
	return out
}

func (d screenDemo) current() screenfixture.Scenario {
	name := propLabel(d.props, "state")
	if sc, ok := d.byName[name]; ok {
		return sc
	}
	if len(d.scenarios) > 0 {
		return d.scenarios[0]
	}
	return screenfixture.Scenario{Name: "empty"}
}

func (d screenDemo) Resize(c demoCtx) demo {
	sc := d.current()
	if sc.Build == nil {
		d.mounted = nil
		return d
	}
	frame, err := screenfixture.FrameAt(c.kit.Width, c.kit.Height)
	if err != nil {
		panic(fmt.Sprintf("screenfixture.FrameAt: %v", err))
	}
	d.frame = frame
	d.mounted = screenfixture.Drive(sc, frame)
	return d
}

func (d screenDemo) Update(msg tea.KeyMsg, c demoCtx) demo {
	if d.mounted == nil {
		return d
	}
	out := d.mounted.Update(d.frame, msg)
	if out.Screen == nil {
		return d
	}
	d.mounted = out.Screen
	if sc := d.current(); sc.Bind != nil {
		d.mounted = sc.Bind(d.mounted)
	}
	return d
}

func (d screenDemo) View(demoCtx) string {
	if d.mounted == nil {
		return ""
	}
	return d.mounted.View(d.frame)
}

func (d screenDemo) Status(demoCtx) string {
	sc := d.current()
	body := ""
	if d.mounted != nil {
		body = ansi.Strip(d.mounted.View(d.frame))
	}
	rows := 0
	if body != "" {
		rows = strings.Count(body, "\n") + 1
	}
	return fmt.Sprintf("state %s · %d keys · %d×%d → %d rows",
		sc.Name, len(sc.Keys), d.frame.Width(), d.frame.Height(), rows)
}

func (d screenDemo) Props() []prop { return d.props }

func (d screenDemo) SetProp(p prop, c demoCtx) demo {
	d.props = replaceProp(d.props, p)
	return d.Resize(c)
}

func (d screenDemo) Help() []key.Binding {
	return []key.Binding{
		binding("j/k", "screen keys"),
		binding("tab", "screen tab"),
	}
}
