package settingspicker

import (
	"fmt"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

func testPayload(kind Kind) Payload {
	return Payload{Kind: kind, Current: "active", Options: []Option{
		{Value: "active", Label: "Active choice", Detail: "active.yaml", Active: true},
		{Value: "candidate", Label: "Candidate choice", Detail: "candidate.yaml", Custom: true},
	}}
}

func goldenLongOptions(count int) []Option {
	options := make([]Option, count)
	for i := range options {
		options[i] = Option{
			Value:  fmt.Sprintf("profile-%02d", i),
			Label:  fmt.Sprintf("Profile %02d", i),
			Detail: fmt.Sprintf("profiles/profile-%02d.yaml", i),
			Custom: i%3 == 2,
			Active: i == 0,
		}
	}
	return options
}

func goldenLongPayload(kind Kind) Payload {
	return Payload{Kind: kind, Current: "profile-00", Options: goldenLongOptions(60)}
}

func goldenBuild(kind Kind, payload Payload) func(screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		return screenfixture.Enter(New(kind).Open(payload), frame)
	}
}

func entryFixtureScenarios(kind Kind) []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			Name:  string(kind),
			Build: goldenBuild(kind, testPayload(kind)),
		},
	}
}

func themeFixtureScenarios() []screenfixture.Scenario {
	out := entryFixtureScenarios(Theme)
	out = append(out, screenfixture.Scenario{
		Name:  "theme-selection-moved",
		Build: goldenBuild(Theme, testPayload(Theme)),
		Keys:  []string{"down"},
	})
	return out
}

func configFixtureScenarios() []screenfixture.Scenario {
	out := entryFixtureScenarios(Config)
	out = append(out, screenfixture.Scenario{
		Name:  "config-scrolled",
		Build: goldenBuild(Config, goldenLongPayload(Config)),
		Keys:  []string{"pgdown", "pgdown"},
	})
	return out
}

// FixtureScenarios returns every recorded state for the Theme, Config and
// Subtask-kit pickers.
func FixtureScenarios() []screenfixture.Scenario {
	out := make([]screenfixture.Scenario, 0, 5)
	out = append(out, themeFixtureScenarios()...)
	out = append(out, configFixtureScenarios()...)
	out = append(out, entryFixtureScenarios(SubtaskKit)...)
	return out
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	switch id {
	case screenhost.ThemePicker:
		return themeFixtureScenarios()
	case screenhost.ConfigPicker:
		return configFixtureScenarios()
	case screenhost.SubtaskKitPicker:
		return entryFixtureScenarios(SubtaskKit)
	default:
		return nil
	}
}
