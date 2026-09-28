package domain

import (
	_ "embed"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed testdata/event_registry_fixture.yaml
var eventRegistryFixture []byte

var fixtureRegistry = sync.OnceValue(func() *EventRegistry {
	var raw struct {
		Definitions map[string]struct {
			Category   string
			Display    string
			Formatter  FormatterID
			Metric     string `yaml:"metric"`
			LogVisible *bool  `yaml:"log_visible"`
			EntityType string `yaml:"entity_type"`
		}
	}
	if err := yaml.Unmarshal(eventRegistryFixture, &raw); err != nil {
		panic(err)
	}
	defs := make([]EventDef, 0, len(raw.Definitions))
	for key, d := range raw.Definitions {
		fn, ok := ResolveFormatter(d.Formatter)
		if !ok {
			panic(d.Formatter)
		}
		visible := true
		if d.LogVisible != nil {
			visible = *d.LogVisible
		}
		defs = append(defs, EventDef{Key: key, Category: EventCategory(d.Category), Display: d.Display, Formatter: fn, Metric: d.Metric, EntityType: d.EntityType, LogVisible: visible})
	}
	return NewEventRegistry(defs)
})
