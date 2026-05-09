package config

import "testing"

func TestLoadBuddy_defaultKittenAndOwl(t *testing.T) {
	for _, p := range []string{"../../defaults/buddies/kitten.yaml", "../../defaults/buddies/owl.yaml"} {
		buddy, err := LoadBuddy(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if buddy.Name == "" {
			t.Fatalf("%s: empty name", p)
		}
		if _, ok := buddy.Animations["idle"]; !ok {
			t.Fatalf("%s: missing idle animation", p)
		}
		if _, ok := buddy.Animations["deny"]; !ok {
			t.Fatalf("%s: missing deny animation", p)
		}
	}
}
