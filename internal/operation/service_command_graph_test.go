package operation

import (
	"strings"
	"testing"

	"omakiten/internal/contract"
)

func TestRelatedCommandContextStopsAfterOneLevel(t *testing.T) {
	commands := map[string]contract.CommandBinding{
		"shape": {Persona: "agent", Skills: []string{"shape"}},
		"run":   {Persona: "agent", Skills: []string{"run"}},
		"pause": {Persona: "agent", Skills: []string{"pause"}},
	}
	skills := map[string]contract.SkillInfo{
		"shape": {Slug: "shape", Description: "Shape work", Command: &contract.CommandDefinition{Name: "shape", Next: []contract.CommandReference{{Name: "run", Context: "full"}, {Name: "pause", Context: "bare", When: "Work must stop"}}}},
		"run":   {Slug: "run", Description: "Run work", Body: "Direct a plan.", Command: &contract.CommandDefinition{Name: "run", Next: []contract.CommandReference{{Name: "pause", Context: "full"}}}},
		"pause": {Slug: "pause", Description: "Pause work", Body: "Record a handoff.", Command: &contract.CommandDefinition{Name: "pause"}},
	}
	personas := map[string]contract.PersonaInfo{"agent": {Slug: "agent", SkillRepertoire: []string{"shape", "run", "pause"}}}
	shape, err := ResolveCommandFromCatalog("shape", commands, personas, skills, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shape.Related) != 2 {
		t.Fatalf("related = %+v", shape.Related)
	}
	run, pause := shape.Related[0], shape.Related[1]
	if run.Name != "run" || run.Context != "full" || !strings.Contains(run.Markdown, "Direct a plan.") || strings.Contains(run.Markdown, "Related commands") {
		t.Fatalf("run context = %+v", run)
	}
	if pause.Name != "pause" || pause.Context != "bare" || pause.Markdown != "" || pause.When == "" {
		t.Fatalf("pause context = %+v", pause)
	}
}
