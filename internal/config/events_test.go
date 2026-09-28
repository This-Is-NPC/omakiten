package config

import (
	"testing"
)

func TestBundledEventRegistries(t *testing.T) {
	for _, preset := range ListPresets() {
		t.Run(preset.Name, func(t *testing.T) {
			settings, err := LoadKitConfigByKey(preset.Name)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := BuildEventRegistry(settings.Events)
			if err != nil {
				t.Fatal(err)
			}
			if len(registry.Types()) == 0 {
				t.Fatal("bundled event registry is empty")
			}
		})
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
