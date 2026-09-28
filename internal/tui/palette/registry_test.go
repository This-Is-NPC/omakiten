package palette

import (
	"regexp"
	"testing"
)

const (
	routeTasksBoard      Route = "tasks.board"
	routeTasksTable      Route = "tasks.table"
	routeSettingsGeneral Route = "settings.general"
)

func testScreens() []ScreenDescriptor {
	return []ScreenDescriptor{
		{Code: "11", Route: routeTasksBoard, TitleKey: "tasks_board"},
		{Code: "12", Route: routeTasksTable, TitleKey: "tasks_table"},
		{Code: "31", Route: routeSettingsGeneral, TitleKey: "settings_general"},
	}
}

func TestRegistryDefaultsResolveWithoutGaps(t *testing.T) {
	screens := testScreens()
	if len(screens) == 0 {
		t.Fatalf("testScreens is empty")
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
		if s.TitleKey == "" {
			t.Errorf("screen at code %q has empty TitleKey", s.Code)
		}
	}
	reg, warnings, err := New(screens, nil)
	if err != nil {
		t.Fatalf("New(testScreens) error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("New(testScreens) warnings = %v, want none", warnings)
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
	reg, warnings, err := New(testScreens(), map[string]Route{
		"11": routeSettingsGeneral,
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
	if got != routeSettingsGeneral {
		t.Fatalf("Resolve(11) = %q, want %q (override should beat positional)", got, routeSettingsGeneral)
	}
}

func TestRegistryCollisionWarning(t *testing.T) {
	defaults := []ScreenDescriptor{
		{Code: "11", Route: routeTasksBoard, TitleKey: "k1"},
		{Code: "11", Route: routeTasksTable, TitleKey: "k2"},
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
	if got != routeTasksBoard {
		t.Fatalf("Resolve(11) = %q, want first-wins %q", got, routeTasksBoard)
	}
}

func TestRegistryUnknownCodeMisses(t *testing.T) {
	reg, _, err := New(testScreens(), nil)
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
		{Code: "0a", Route: routeTasksBoard, TitleKey: "k"},
	}
	_, _, err := New(defaults, nil)
	if err == nil {
		t.Fatalf("New(malformed default code) err = nil, want error")
	}
}

func TestNewRejectsEmptyDefaultRoute(t *testing.T) {
	defaults := []ScreenDescriptor{
		{Code: "11", Route: "", TitleKey: "k"},
	}
	_, _, err := New(defaults, nil)
	if err == nil {
		t.Fatalf("New(empty default route) err = nil, want error")
	}
}

func TestNewOverrideMalformedCodeWarns(t *testing.T) {
	_, warnings, err := New(testScreens(), map[string]Route{
		"0a": routeTasksBoard,
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if len(warnings) != 1 || warnings[0].Code != "0a" {
		t.Fatalf("warnings = %v, want 1 for code 0a", warnings)
	}
}

func TestNewOverrideUnknownRouteWarns(t *testing.T) {
	_, warnings, err := New(testScreens(), map[string]Route{
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
	reg, _, err := New(testScreens(), map[string]Route{
		"99": routeSettingsGeneral,
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
