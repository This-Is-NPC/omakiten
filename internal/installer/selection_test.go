package installer

import (
	"slices"
	"testing"
)

func TestResolvePreset_ShellParity(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		wantName     string
		wantFellback bool
	}{
		{"empty_uses_default", "", DefaultPreset, false},
		{"whitespace_uses_default", "   ", DefaultPreset, false},
		{"named_match", "izakaya", "izakaya", false},
		{"named_match_caps", "Shokunin", "shokunin", false},
		{"numeric_index", "3", "kaiseki", false},
		{"numeric_out_of_range", "99", DefaultPreset, true},
		{"unknown_name_falls_back", "bogus", DefaultPreset, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fellback := ResolvePreset(tc.raw)
			if got != tc.wantName {
				t.Fatalf("preset: got %q want %q", got, tc.wantName)
			}
			if fellback != tc.wantFellback {
				t.Fatalf("fellback: got %v want %v", fellback, tc.wantFellback)
			}
		})
	}
}

func TestSupportedPresets_MenuOrder(t *testing.T) {
	want := []string{"omakase", "izakaya", "kaiseki", "shokunin", "orchestra"}
	if got := SupportedPresets(); !slices.Equal(got, want) {
		t.Fatalf("SupportedPresets: got %v want %v", got, want)
	}
}
