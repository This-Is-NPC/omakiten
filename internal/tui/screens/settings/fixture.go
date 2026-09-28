package settings

import (
	"omakiten/internal/config"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

// goldenConfigPath and goldenDBPath are the runtime locations the fixtures
// DISPLAY. They are literals rather than a materialised t.TempDir() precisely
// so the recorded wrap column is a property of the layout instead of a property
// of the machine that ran the test. Both are past the 49-column value cell an
// 80-column terminal leaves, so each wraps there and neither wraps at 200.
const (
	goldenConfigPath = "/home/omakiten/.config/omakiten/profiles/characterization-baseline/omakase.yaml"
	goldenDBPath     = "/home/omakiten/.local/share/omakiten/characterization-baseline/omakiten.db"
)

func testPayload() Payload {
	snapshot := config.BuildSnapshot(testBundle([]config.TransitionGuard{{Type: "comments_tagged"}}))
	return Payload{
		Runtime:  Runtime{Version: "v-test", Scope: "global", ConfigPath: "/tmp/omakiten.yaml", DBPath: "/tmp/omakiten.db"},
		Workflow: snapshot.Workflow(), ThemeKey: "omacon",
		Languages: config.LanguageSettings{CLI: "en", TUI: "en", AgentOutput: "en"}, Snapshot: snapshot,
	}
}

func testBundle(guards []config.TransitionGuard) config.Bundle {
	return config.Bundle{
		Kit:       config.Kit{Key: "omakase"},
		Config:    config.Settings{Workflow: config.WorkflowSettings{Active: "omakase"}, Theme: config.ThemeSettings{Active: "omacon"}, Output: config.OutputSettings{OmitEmpty: true}},
		Workflows: []config.Workflow{{ID: 1, Key: "omakase", Buckets: []config.Bucket{{ID: 1, Key: "backlog", Position: 1}, {ID: 2, Key: "dev", Position: 2}, {ID: 3, Key: "done", Position: 3}}, Transitions: []config.Transition{{From: 1, To: 2, Guards: guards}, {From: 2, To: 3}}}},
	}
}

func goldenPayload() Payload {
	payload := testPayload()
	payload.Runtime.ConfigPath = goldenConfigPath
	payload.Runtime.DBPath = goldenDBPath
	return payload
}

func goldenDualPayload() Payload {
	payload := goldenPayload()
	sub := testBundle([]config.TransitionGuard{{Type: "comments_min", Count: 2}})
	root := testBundle([]config.TransitionGuard{{Type: "comments_tagged", Count: 1, Tag: "tests-passing"}})
	root.SubtaskBundle = &sub
	payload.Snapshot = config.BuildSnapshot(root)
	payload.Workflow = payload.Snapshot.Workflow()
	return payload
}

func goldenBind(payload Payload) func(screenhost.Screen) screenhost.Screen {
	return func(screen screenhost.Screen) screenhost.Screen {
		return screen.(Screen).Bind(payload, nil)
	}
}

func generalGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(NewGeneral().Bind(goldenPayload(), nil), frame)
}

func guardsGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(NewGuards().Bind(goldenPayload(), nil), frame)
}

func guardsDualGoldenBuild(frame screenhost.Frame) screenhost.Screen {
	return screenfixture.Enter(NewGuards().Bind(goldenDualPayload(), nil), frame)
}

func generalFixtureScenarios() []screenfixture.Scenario {
	general := goldenPayload()
	bind := goldenBind(general)
	return []screenfixture.Scenario{
		{
			Name:  "general",
			Build: generalGoldenBuild,
			Bind:  bind,
		},
		{
			Name:  "general-scrolled",
			Build: generalGoldenBuild,
			Bind:  bind,
			Keys:  []string{"j", "j", "j"},
		},
		{
			Name:  "general-bottom",
			Build: generalGoldenBuild,
			Bind:  bind,
			Keys:  []string{"G"},
		},
	}
}

func guardsFixtureScenarios() []screenfixture.Scenario {
	general := goldenPayload()
	dual := goldenDualPayload()
	return []screenfixture.Scenario{
		{
			Name:  "guards",
			Build: guardsGoldenBuild,
			Bind:  goldenBind(general),
		},
		{
			Name:  "guards-dual",
			Build: guardsDualGoldenBuild,
			Bind:  goldenBind(dual),
		},
	}
}

// FixtureScenarios returns every recorded state for Settings › General and
// Settings › Guards.
func FixtureScenarios() []screenfixture.Scenario {
	out := make([]screenfixture.Scenario, 0, 5)
	out = append(out, generalFixtureScenarios()...)
	out = append(out, guardsFixtureScenarios()...)
	return out
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	switch id {
	case screenhost.SettingsGeneral:
		return generalFixtureScenarios()
	case screenhost.SettingsGuards:
		return guardsFixtureScenarios()
	default:
		return nil
	}
}
