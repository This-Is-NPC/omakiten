package relationshippicker

import (
	"fmt"

	relationshipprojection "omakiten/internal/relationshipprojection"
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

func testPayload(kind relationshipprojection.Kind) Payload {
	options := []relationshipprojection.Option{{Value: "go", Label: "Go", Detail: "Go engineering", Selected: true}, {Value: "sqlite", Label: "SQLite", Detail: "Data persistence"}}
	if kind == TemplateDefault {
		options = []relationshipprojection.Option{{Value: "task", Label: "task", Selected: true}, {Value: "pr", Label: "pr"}, {None: true}}
	}
	return Payload{Kind: kind, EntitySlug: "agent", ProjectSlug: "omakiten", Generation: 5, Options: options}
}

func relationshipGoldenOptions(count int) []relationshipprojection.Option {
	options := make([]relationshipprojection.Option, count)
	for i := range options {
		options[i] = relationshipprojection.Option{
			Value:    fmt.Sprintf("skill-%02d", i+1),
			Label:    fmt.Sprintf("skill-%02d · long skill name", i+1),
			Detail:   fmt.Sprintf("capability %02d of the repertoire", i+1),
			Selected: i%4 == 0,
		}
	}
	return options
}

func relationshipGoldenPayload(kind relationshipprojection.Kind, options []relationshipprojection.Option) Payload {
	return Payload{Kind: kind, EntitySlug: "agent", ProjectSlug: "omakiten", Generation: 5, Options: options}
}

func relationshipGoldenBuild(kind relationshipprojection.Kind, payload Payload) func(screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		return screenfixture.Enter(New(kind).Open(payload), frame)
	}
}

func personaSkillsFixtureScenarios() []screenfixture.Scenario {
	long := relationshipGoldenOptions(40)
	return []screenfixture.Scenario{
		{
			Name:  string(PersonaSkills),
			Build: relationshipGoldenBuild(PersonaSkills, testPayload(PersonaSkills)),
		},
		{
			Name:  "persona-skills-scrolled",
			Build: relationshipGoldenBuild(PersonaSkills, relationshipGoldenPayload(PersonaSkills, long)),
			Keys:  []string{"G", "k", "k"},
		},
		{
			Name:  "persona-skills-confirming-cancel",
			Build: relationshipGoldenBuild(PersonaSkills, testPayload(PersonaSkills)),
			Keys:  []string{"j", " ", "esc"},
		},
	}
}

func templateDefaultFixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			Name:  string(TemplateDefault),
			Build: relationshipGoldenBuild(TemplateDefault, testPayload(TemplateDefault)),
		},
		{
			Name:  "template-default-cursor",
			Build: relationshipGoldenBuild(TemplateDefault, testPayload(TemplateDefault)),
			Keys:  []string{"down", "down"},
		},
	}
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	switch id {
	case screenhost.PersonaSkills:
		return personaSkillsFixtureScenarios()
	case screenhost.TemplateDefault:
		return templateDefaultFixtureScenarios()
	default:
		return nil
	}
}
