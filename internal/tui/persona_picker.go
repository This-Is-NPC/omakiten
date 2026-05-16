package tui

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/picker"
)

// openPersonaPicker initializes the multi-select picker for the persona at
// slug. It pre-checks every skill the persona currently references and lists
// every loaded skill as a row. Submission writes only the wiring entry.
func (m *Model) openPersonaPicker(slug string) {
	persona, ok := m.findPersonaBySlug(slug)
	if !ok {
		m.status = "Persona not found"
		return
	}
	checks := map[string]bool{}
	for _, key := range persona.SkillKeys {
		checks[key] = true
	}
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{
		kind:         entityKindPersona,
		mode:         entityScreenSkillPicker,
		slug:         slug,
		pickerChecks: checks,
	}
	m.entityPicker = picker.New(picker.Multi)
	m.status = "Skill picker"
}

func (m *Model) openPersonaPickerForSelected() {
	if m.entityCount(entityKindPersona) == 0 {
		m.status = "No persona selected"
		return
	}
	cursor := m.selectedEntityIndex(entityKindPersona)
	slug := m.entitySlugAt(entityKindPersona, cursor)
	if slug == "" {
		return
	}
	m.openPersonaPicker(slug)
}

func (m Model) updatePersonaPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "q" {
		return m, tea.Quit
	}
	rowCount := len(m.skills) + 1 // +1 for "+ create new skill" affordance

	// Special-case enter on the last row BEFORE delegating: the persona
	// picker is multi-select (space toggles, ctrl+s saves) but enter on
	// the sticky "+ create new skill" row escapes into the scaffold flow.
	// In multi mode the picker reports EventNone for enter, so the
	// fall-through below would be silent — handle the special row up front.
	if msg.String() == "enter" && m.entityPicker.Cursor == len(m.skills) {
		return m, m.scaffoldNewSkillFromPicker()
	}

	var cmd tea.Cmd
	m.entityPicker, cmd = m.entityPicker.Update(msg, rowCount, scrollDataRows(m.pickerViewportRows()))
	switch m.entityPicker.LastEvent() {
	case picker.EventCancel:
		m.openSelectedEntityViewForSlug(entityKindPersona, m.entityForm.slug)
	case picker.EventSelect:
		m.savePersonaPicker()
	case picker.EventToggle:
		if m.entityPicker.Cursor < len(m.skills) {
			slug := m.skills[m.entityPicker.Cursor].Key
			if m.entityForm.pickerChecks == nil {
				m.entityForm.pickerChecks = map[string]bool{}
			}
			m.entityForm.pickerChecks[slug] = !m.entityForm.pickerChecks[slug]
		}
	}
	return m, cmd
}

// pickerViewportRows returns how many picker rows fit between the screen
// chrome and the panel's internal header rows.
func (m Model) pickerViewportRows() int {
	if m.height <= 0 {
		return 0
	}
	// 2 entity-mode header + 1 leading blank + 2 footer + 2 panel borders
	// + 4 panel header rows (kicker/hint/blank/separator) = 11.
	chrome := 11
	if m.status != "" {
		chrome++
	}
	rows := m.height - chrome
	if rows < 4 {
		return 0
	}
	return rows
}

// savePersonaPicker writes only the persona wiring (skills slugs) without
// touching the persona body file. Selection is the set of checked rows.
func (m *Model) savePersonaPicker() {
	if m.repos.Editor == nil {
		m.status = "Editor not available"
		return
	}
	slugs := make([]string, 0, len(m.entityForm.pickerChecks))
	for _, skill := range m.skills {
		if m.entityForm.pickerChecks[skill.Key] {
			slugs = append(slugs, skill.Key)
		}
	}
	service := app.NewPersonaService(m.repos.activeSnapshot(), m.repos.Editor, m.repos.EntityFiles, m.repos.Slugger)
	keys := slugs
	if _, err := service.Edit(m.ctx, m.entityForm.slug, domain.PersonaUpdate{SkillKeys: &keys}); err != nil {
		m.status = err.Error()
		return
	}
	if err := m.refresh(); err != nil {
		m.status = err.Error()
		return
	}
	m.openSelectedEntityViewForSlug(entityKindPersona, m.entityForm.slug)
	m.status = "Saved"
}

// scaffoldNewSkillFromPicker creates a placeholder skill, opens $EDITOR
// against it, and pre-checks it on the picker once the editor returns.
func (m *Model) scaffoldNewSkillFromPicker() tea.Cmd {
	name := nextScaffoldName(entityKindSkill, m.snapshot())
	path, err := m.scaffoldEntity(m.ctx, entityKindSkill, m.repos, name)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	// Pre-check the new skill so it is selected on the picker after editor exits.
	if m.entityForm.pickerChecks == nil {
		m.entityForm.pickerChecks = map[string]bool{}
	}
	// The slug derives deterministically from the scaffold name.
	slug := slugFromName(name)
	m.entityForm.pickerChecks[slug] = true
	if err := m.refresh(); err != nil {
		m.status = err.Error()
	}
	return runExternalEditor(path)
}

func (m *Model) openSelectedEntityViewForSlug(kind entityKind, slug string) {
	m.entityScreen = entityScreenView
	m.entityForm = entityForm{kind: kind, mode: entityScreenView, slug: slug}
	m.entityView = detailscreen.New(0)
}

func (m Model) renderPersonaPicker() string {
	persona, _ := m.findPersonaBySlug(m.entityForm.slug)

	skills := append([]domain.Skill(nil), m.skills...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].Key < skills[j].Key })

	dataRows := make([]string, 0, len(skills)+1)
	for index, skill := range skills {
		check := "[ ]"
		if m.entityForm.pickerChecks[skill.Key] {
			check = "[x]"
		}
		marker := m.cursorMarker(m.entityPicker.Cursor == index)
		dataRows = append(dataRows, fmt.Sprintf("%s %s %s — %s", marker, check, skill.Key, skill.Name))
	}
	addMarker := m.cursorMarker(m.entityPicker.Cursor == len(skills))
	dataRows = append(dataRows, fmt.Sprintf("%s + create new skill (opens $EDITOR)", addMarker))

	header := []string{
		m.styles.kicker(fmt.Sprintf("Skills for persona · %s", persona.Key)),
		m.styles.hint.Render("up/down: move · space: toggle · enter on '+ create new': new skill · ctrl+s: save · esc: cancel"),
		"",
	}
	return m.renderPickerPanel(header, dataRows, m.entityPicker.Scroll, m.pickerViewportRows())
}
