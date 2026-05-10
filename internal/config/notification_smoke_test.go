package config

import "testing"

func TestLoadNotification_defaults(t *testing.T) {
	for _, p := range []string{"../../defaults/notifications/guard-violation.yaml", "../../defaults/notifications/agent-comment.yaml"} {
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
