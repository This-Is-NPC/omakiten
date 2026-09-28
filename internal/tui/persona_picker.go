package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	relationshipprojection "omakiten/internal/relationshipprojection"
	"omakiten/internal/tui/screenhost"
	"omakiten/internal/tui/screens/relationshippicker"
)

func (m *Model) openPersonaPicker(slug string) {
	persona, ok := m.findPersonaBySlug(slug)
	if !ok {
		m.status = m.t("tui.status.persona_not_found")
		return
	}
	selected := make(map[string]bool, len(persona.SkillKeys))
	for _, key := range persona.SkillKeys {
		selected[key] = true
	}
	options := make([]relationshipprojection.Option, 0, len(m.skills))
	for _, skill := range m.skills {
		options = append(options, relationshipprojection.Option{Value: skill.Key, Label: skill.Name, Selected: selected[skill.Key]})
	}
	m.relationshipPickerGeneration++
	m.personaSkillsScreen = relationshippicker.New(relationshippicker.PersonaSkills).Open(relationshippicker.Payload{
		Kind: relationshippicker.PersonaSkills, EntitySlug: slug,
		Generation: m.relationshipPickerGeneration, Options: options})
	m.status = m.t("tui.status.skill_picker")
	m.pushScreen(screenhost.PersonaSkills)
}

func (m *Model) savePersonaSkills(screen relationshippicker.Screen) {
	if m.repos.Editor == nil {
		m.status = m.t("tui.status.editor_unavailable")
		return
	}
	allowed := make(map[string]bool, len(m.skills))
	for _, skill := range m.skills {
		allowed[skill.Key] = true
	}
	keys := screen.SelectedValues()
	for _, key := range keys {
		if !allowed[key] {
			return
		}
	}
	svc, ok := m.requireOps()
	if !ok {
		return
	}
	if _, err := svc.EditPersona(m.ctx, screen.Payload().EntitySlug, domain.PersonaUpdate{SkillKeys: &keys}); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.rotateSnapshotAfterEdit(); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.popScreen()
	m.refreshStackedEntityDetail(entityKindPersona)
	m.status = m.t("tui.status.saved")
}

func (m *Model) scaffoldRelationshipSkill(screen relationshippicker.Screen) tea.Cmd {
	name := nextScaffoldName(entityKindSkill, m.snapshot())
	path, err := m.scaffoldEntity(m.ctx, entityKindSkill, m.repos, name)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	selected := make(map[string]bool, len(screen.SelectedValues())+1)
	for _, value := range screen.SelectedValues() {
		selected[value] = true
	}
	selected[domain.Slugify(name)] = true
	if err := m.rotateSnapshotAfterEdit(); err != nil {
		m.status = err.Error()
		return nil
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return nil
	}
	options := make([]relationshipprojection.Option, 0, len(m.skills))
	for _, skill := range m.skills {
		options = append(options, relationshipprojection.Option{Value: skill.Key, Label: skill.Name, Selected: selected[skill.Key]})
	}
	m.relationshipPickerGeneration++
	m.personaSkillsScreen = relationshippicker.New(relationshippicker.PersonaSkills).Open(relationshippicker.Payload{
		Kind: relationshippicker.PersonaSkills, EntitySlug: screen.Payload().EntitySlug,
		Generation: m.relationshipPickerGeneration, Options: options})
	return runExternalEditor(path)
}

func isRelationshipAction(kind screenhost.ActionKind) bool {
	switch kind {
	case screenhost.ActionSelectRelationship, screenhost.ActionSavePersonaSkills,
		screenhost.ActionSelectTemplateDefault, screenhost.ActionCreateRelationship,
		screenhost.ActionCancelRelationshipPicker:
		return true
	}
	return false
}

func (m Model) acceptRelationshipOutcome(outcome screenhost.Outcome) bool {
	screen, ok := outcome.Screen.(relationshippicker.Screen)
	if !ok || len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screen.ID() {
		return false
	}
	payload := screen.Payload()
	if outcome.Action.Generation != m.relationshipPickerGeneration || payload.Generation != m.relationshipPickerGeneration {
		return false
	}
	switch payload.Kind {
	case relationshippicker.PersonaSkills:
		_, ok = m.findPersonaBySlug(payload.EntitySlug)
	case relationshippicker.TemplateDefault:
		_, ok = m.findTemplateBySlug(payload.EntitySlug)
	default:
		ok = false
	}
	return ok
}

func (m *Model) refreshStackedEntityDetail(kind entityKind) {
	if len(m.screenStack) == 0 || m.screenStack[len(m.screenStack)-1] != screenhost.EntityDetail {
		return
	}
	slug := m.entityDetailScreen.Payload().Slug
	m.entityDetailScreen = m.boundEntityDetailScreen().Open(m.entityDetailPayload(kind, slug))
}
