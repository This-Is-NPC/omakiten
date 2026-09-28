package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/testfixtures/runtimecache"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/relationshippicker"
)

func TestPersonaRelationshipScreenSavesSelectedSkills(t *testing.T) {
	model, _, _ := newEntityModel(t)
	if _, err := model.ops().AddSkill(context.Background(), domain.SkillInput{Key: "sqlite", Name: "SQLite"}); err != nil {
		t.Fatal(err)
	}
	if err := runtimecache.RefreshFromEditor(model.repos.Cache, model.repos.ProjectID, model.repos.Editor); err != nil {
		t.Fatal(err)
	}
	if err := model.refresh(); err != nil {
		t.Fatal(err)
	}

	model.openPersonaPicker("agent")
	if got := model.screenStack[len(model.screenStack)-1]; got != screenhost.PersonaSkills {
		t.Fatalf("route = %q", got)
	}
	screen := model.personaSkillsScreen
	if screen.Payload().EntitySlug != "agent" || screen.Payload().Generation == 0 {
		t.Fatalf("payload = %+v", screen.Payload())
	}
	screen = screen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeySpace}).Screen.(relationshippicker.Screen)
	screen = screen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeyDown}).Screen.(relationshippicker.Screen)
	screen = screen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeySpace}).Screen.(relationshippicker.Screen)
	model.applyScreenOutcome(screen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeyCtrlS}))

	persona, ok := model.findPersonaBySlug("agent")
	if !ok || len(persona.SkillKeys) != 1 || persona.SkillKeys[0] != "sqlite" {
		t.Fatalf("persona skills = %v", persona.SkillKeys)
	}
	if len(model.screenStack) != 0 || model.status != model.t("tui.status.saved") {
		t.Fatalf("post-save route/status = %v / %q", model.screenStack, model.status)
	}
}

func TestRelationshipOutcomeRejectsStaleGenerationEntityAndRoute(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.openPersonaPicker("agent")
	screen := model.personaSkillsScreen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeySpace}).Screen.(relationshippicker.Screen)
	out := screen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeyCtrlS})

	model.relationshipPickerGeneration++
	model.applyScreenOutcome(out)
	if len(model.screenStack) != 1 {
		t.Fatal("stale generation closed picker")
	}
	if persona, _ := model.findPersonaBySlug("agent"); len(persona.SkillKeys) != 1 || persona.SkillKeys[0] != "go" {
		t.Fatalf("stale generation persisted selection: %v", persona.SkillKeys)
	}

	model.relationshipPickerGeneration = screen.Payload().Generation
	model.personas = nil
	model.applyScreenOutcome(out)
	if len(model.screenStack) != 1 {
		t.Fatal("stale entity result closed picker")
	}

	model.personas = []domain.Persona{{Key: "agent"}}
	model.screenStack = nil
	model.applyScreenOutcome(out)
	if len(model.screenStack) != 0 {
		t.Fatal("inactive-route result changed navigation")
	}
}

func TestRelationshipCancelPopsToEntityDetail(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.openEntityDetail(entityKindPersona, "agent")
	model.openPersonaPicker("agent")
	model.applyScreenOutcome(model.personaSkillsScreen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeyEsc}))
	if len(model.screenStack) != 1 || model.screenStack[0] != screenhost.EntityDetail {
		t.Fatalf("cancel stack = %v", model.screenStack)
	}
}

func TestCreateSkillRefreshesAndPreselectsNewCandidate(t *testing.T) {
	model, _, _ := newEntityModel(t)
	model.openPersonaPicker("agent")
	for model.personaSkillsScreen.Cursor() < len(model.personaSkillsScreen.Options())-1 {
		model.personaSkillsScreen = model.personaSkillsScreen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeyDown}).Screen.(relationshippicker.Screen)
	}
	before := len(model.skills)
	model.applyScreenOutcome(model.personaSkillsScreen.Update(model.screenFrame(), tea.KeyMsg{Type: tea.KeyEnter}))
	if len(model.skills) != before+1 {
		t.Fatalf("skills after scaffold = %d, want %d", len(model.skills), before+1)
	}
	selected := model.personaSkillsScreen.SelectedValues()
	if len(selected) != 2 || selected[0] != "go" {
		t.Fatalf("selected skills after scaffold = %v", selected)
	}
}
