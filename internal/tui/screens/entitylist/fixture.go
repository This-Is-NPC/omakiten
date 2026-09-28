package entitylist

import (
	"fmt"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

func testItems(kind string) []Item {
	return []Item{
		{Slug: kind + "-1", Label: kind + "-1", Badges: []string{"ACTIVE", "12 TOKENS"}},
		{Slug: kind + "-2", Label: kind + "-2", Badges: []string{"CUSTOM"}},
		{Slug: kind + "-3", Label: kind + "-3"},
	}
}

func entityListGoldenItems(count int) []Item {
	items := make([]Item, count)
	for i := range items {
		slug := fmt.Sprintf("entity-%02d", i+1)
		items[i] = Item{
			Slug:  slug,
			Label: fmt.Sprintf("%s · a deliberately long entity name that wraps inside the card cell", slug),
		}
		switch i % 3 {
		case 0:
			items[i].Badges = []string{"ACTIVE", "12 TOKENS"}
		case 1:
			items[i].Badges = []string{"CUSTOM"}
		}
	}
	return items
}

func entityListBuild(descriptor Descriptor, items []Item) func(screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		return screenfixture.Enter(New(descriptor).Bind(items, nil), frame)
	}
}

func entityListBind(items []Item) func(screenhost.Screen) screenhost.Screen {
	return func(screen screenhost.Screen) screenhost.Screen {
		return screen.(Screen).Bind(items, nil)
	}
}

func kindFixtureScenarios(descriptor Descriptor) []screenfixture.Scenario {
	items := testItems(string(descriptor.Kind))
	return []screenfixture.Scenario{
		{
			Name:  string(descriptor.Kind),
			Build: entityListBuild(descriptor, items),
			Bind:  entityListBind(items),
		},
	}
}

func lawsLongFixtureScenarios() []screenfixture.Scenario {
	long := entityListGoldenItems(42)
	descriptor := Laws()
	return []screenfixture.Scenario{
		{
			Name:  "laws-grid-cursor",
			Build: entityListBuild(descriptor, long),
			Bind:  entityListBind(long),
			Keys:  []string{"j", "j", "j", "j", "j"},
		},
		{
			Name:  "laws-grid-end",
			Build: entityListBuild(descriptor, long),
			Bind:  entityListBind(long),
			Keys:  []string{"G"},
		},
	}
}

// FixtureScenariosFor returns the scenarios for one screen ID, or nil when the
// ID is not owned by this package.
func FixtureScenariosFor(id screenhost.ID) []screenfixture.Scenario {
	switch id {
	case screenhost.SettingsLaws:
		out := kindFixtureScenarios(Laws())
		out = append(out, lawsLongFixtureScenarios()...)
		return out
	case screenhost.SettingsPersonas:
		return kindFixtureScenarios(Personas())
	case screenhost.SettingsSkills:
		return kindFixtureScenarios(Skills())
	case screenhost.SettingsTemplates:
		return kindFixtureScenarios(Templates())
	case screenhost.SettingsTags:
		return kindFixtureScenarios(Tags())
	default:
		return nil
	}
}
