package palette

import (
	"regexp"
	"testing"
)

func TestDefaultScreensZeroGaps(t *testing.T) {
	screens := DefaultScreens()
	if len(screens) == 0 {
		t.Fatalf("DefaultScreens is empty")
	}
	codeRE := regexp.MustCompile(`^[1-9][1-9]$`)
	seen := map[string]struct{}{}
	for _, s := range screens {
		if !codeRE.MatchString(s.Code) {
			t.Errorf("screen %q has malformed code %q (want 2 digits in 1-9)", s.Route, s.Code)
		}
		if _, dup := seen[s.Code]; dup {
			t.Errorf("duplicate code %q", s.Code)
		}
		seen[s.Code] = struct{}{}
		if _, ok := validRoutes[s.Route]; !ok {
			t.Errorf("screen at code %q references unknown route %q", s.Code, s.Route)
		}
		if s.TitleKey == "" {
			t.Errorf("screen at code %q has empty TitleKey", s.Code)
		}
	}
	reg, warnings, err := New(screens, nil)
	if err != nil {
		t.Fatalf("New(DefaultScreens) error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("New(DefaultScreens) warnings = %v, want none", warnings)
	}
	for _, s := range screens {
		got, ok := reg.Resolve(s.Code)
		if !ok {
			t.Errorf("Resolve(%q) miss after registration", s.Code)
			continue
		}
		if got != s.Route {
			t.Errorf("Resolve(%q) = %q, want %q", s.Code, got, s.Route)
		}
	}
}

func TestRegistryOverrideBeatsPositional(t *testing.T) {
	reg, warnings, err := New(DefaultScreens(), map[string]Route{
		"11": RouteSettingsGeneral,
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	got, ok := reg.Resolve("11")
	if !ok {
		t.Fatalf("Resolve(11) miss after override")
	}
	if got != RouteSettingsGeneral {
		t.Fatalf("Resolve(11) = %q, want %q (override should beat positional)", got, RouteSettingsGeneral)
	}
}

func TestRegistryCollisionWarning(t *testing.T) {
	defaults := []ScreenDescriptor{
		{Code: "11", Route: RouteTasksBoard, TitleKey: "k1"},
		{Code: "11", Route: RouteTasksTable, TitleKey: "k2"},
	}
	reg, warnings, err := New(defaults, nil)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}
	if warnings[0].Code != "11" {
		t.Fatalf("warning code = %q, want 11", warnings[0].Code)
	}
	got, _ := reg.Resolve("11")
	if got != RouteTasksBoard {
		t.Fatalf("Resolve(11) = %q, want first-wins %q", got, RouteTasksBoard)
	}
}

func TestRegistryUnknownCodeMisses(t *testing.T) {
	reg, _, err := New(DefaultScreens(), nil)
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if _, ok := reg.Resolve("99"); ok {
		t.Fatalf("Resolve(99) hit, want miss")
	}
	if _, ok := reg.Resolve(""); ok {
		t.Fatalf("Resolve(empty) hit, want miss")
	}
	if _, ok := reg.Resolve("abc"); ok {
		t.Fatalf("Resolve(abc) hit, want miss")
	}
}

func TestNewRejectsMalformedDefaultCode(t *testing.T) {
	defaults := []ScreenDescriptor{
		{Code: "0a", Route: RouteTasksBoard, TitleKey: "k"},
	}
	_, _, err := New(defaults, nil)
	if err == nil {
		t.Fatalf("New(malformed default code) err = nil, want error")
	}
}

func TestNewRejectsUnknownDefaultRoute(t *testing.T) {
	defaults := []ScreenDescriptor{
		{Code: "11", Route: Route("not.a.route"), TitleKey: "k"},
	}
	_, _, err := New(defaults, nil)
	if err == nil {
		t.Fatalf("New(unknown default route) err = nil, want error")
	}
}

func TestNewOverrideMalformedCodeWarns(t *testing.T) {
	_, warnings, err := New(DefaultScreens(), map[string]Route{
		"0a": RouteTasksBoard,
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if len(warnings) != 1 || warnings[0].Code != "0a" {
		t.Fatalf("warnings = %v, want 1 for code 0a", warnings)
	}
}

func TestNewOverrideUnknownRouteWarns(t *testing.T) {
	_, warnings, err := New(DefaultScreens(), map[string]Route{
		"99": Route("bogus.route"),
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if len(warnings) != 1 || warnings[0].Code != "99" {
		t.Fatalf("warnings = %v, want 1 for code 99", warnings)
	}
}

func TestRegistryCodesSortedAscending(t *testing.T) {
	reg, _, err := New(DefaultScreens(), map[string]Route{
		"99": RouteSettingsGeneral,
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	codes := reg.Codes()
	for i := 1; i < len(codes); i++ {
		if codes[i-1] >= codes[i] {
			t.Fatalf("Codes() not sorted: %q >= %q at index %d", codes[i-1], codes[i], i)
		}
	}
}
