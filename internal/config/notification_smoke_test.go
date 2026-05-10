package config

import (
	"path/filepath"
	"testing"
)

func TestLoadNotification_defaults(t *testing.T) {
	paths, err := filepath.Glob("../../defaults/notifications/*.yaml")
	if err != nil {
		t.Fatalf("Glob defaults: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no default notifications found")
	}
	for _, p := range paths {
		notification, err := LoadNotification(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if notification.Name == "" {
			t.Fatalf("%s: empty name", p)
		}
		if len(notification.Animation) == 0 {
			t.Fatalf("%s: empty animation", p)
		}
		if notification.Position == "" {
			t.Fatalf("%s: empty position", p)
		}
		if notification.Dismiss.Mode == "" {
			t.Fatalf("%s: empty dismiss.mode", p)
		}
	}
}
