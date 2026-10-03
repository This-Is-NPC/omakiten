package config

import (
	"testing"
)

func TestFixtureEventRegistry(t *testing.T) {
	settings, err := LoadKitConfig()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := BuildEventRegistry(settings.Events)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Types()) == 0 {
		t.Fatal("fixture event registry is empty")
	}
}

func TestEventsSettingsResolveLog(t *testing.T) {
	tru := true
	fal := false
	cases := []struct {
		name      string
		settings  EventsSettings
		eventType string
		want      bool
	}{
		{
			name:      "default true with no override",
			settings:  EventsSettings{Defaults: EventChannelSettings{Log: &tru}},
			eventType: "task.created",
			want:      true,
		},
		{
			name: "override false beats default true",
			settings: EventsSettings{
				Defaults:  EventChannelSettings{Log: &tru},
				Overrides: map[string]EventChannelSettings{"tag.added": {Log: &fal}},
			},
			eventType: "tag.added",
			want:      false,
		},
		{
			name: "override nil inherits default",
			settings: EventsSettings{
				Defaults:  EventChannelSettings{Log: &fal},
				Overrides: map[string]EventChannelSettings{"tag.added": {Broadcast: &tru}},
			},
			eventType: "tag.added",
			want:      false,
		},
		{
			name:      "no defaults, no override falls through to true",
			settings:  EventsSettings{},
			eventType: "task.created",
			want:      true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.settings.ResolveLog(tc.eventType); got != tc.want {
				t.Fatalf("ResolveLog(%q) = %v, want %v", tc.eventType, got, tc.want)
			}
		})
	}
}

func TestValidateEventsSettingsExternalNaming(t *testing.T) {
	settings, err := LoadKitConfig()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		key      string
		category string
		ok       bool
	}{
		"declared external event":       {"external.ci_failed", "external", true},
		"external name, other category": {"external.ci_failed", "audit", false},
		"external category, other name": {"ci.failed", "external", false},
		"malformed external name":       {"external.CI-Failed", "external", false},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			events := settings.Events
			events.Definitions = map[string]EventDefinitionSettings{}
			for key, def := range settings.Events.Definitions {
				events.Definitions[key] = def
			}
			events.Definitions[tc.key] = EventDefinitionSettings{Category: tc.category, Display: "d", Formatter: "external"}
			err := validateEventsSettings(events)
			if (err == nil) != tc.ok {
				t.Fatalf("validateEventsSettings(%s, %s) = %v, want ok=%v", tc.key, tc.category, err, tc.ok)
			}
		})
	}
}
