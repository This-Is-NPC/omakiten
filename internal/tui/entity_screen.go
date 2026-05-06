package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
)

func (m Model) updateEntityScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.entityForm.mode {
	case entityScreenSkillPicker:
		return m.updatePersonaPicker(msg)
	case entityScreenThemePicker:
		return m.updateThemePicker(msg)
	case entityScreenConfigPicker:
		return m.updateConfigPicker(msg)
	case entityScreenDefaultPicker:
		return m.updateTemplateDefaultPicker(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.deletePending {
			m.clearDeletePrompt("Delete cancelled")
			return m, nil
		}
		m.closeEntityScreen("")
	case "e":
		m.clearDeletePrompt("")
		return m, m.openEntityEditor(m.entityForm.kind, m.entityForm.slug)
	case "d":
		if m.entityForm.kind == entityKindTemplate {
			m.status = "Templates auto-load — remove the .md file from templates/ and refresh"
			return m, nil
		}
		m.requestEntityDelete(m.entityForm.kind, m.entityForm.slug)
	case "p":
		m.clearDeletePrompt("")
		if m.entityForm.kind == entityKindPersona {
			m.openPersonaPicker(m.entityForm.slug)
		}
	case "a":
		m.clearDeletePrompt("")
		if m.entityForm.kind == entityKindTemplate {
			m.openTemplateDefaultPicker(m.entityForm.slug)
		}
	case "r":
		m.clearDeletePrompt("")
		if err := m.refresh(); err != nil {
			m.status = err.Error()
		} else {
			m.status = "Refreshed"
		}
	case "j", "down":
		m.entityViewScroll++
	case "k", "up":
		if m.entityViewScroll > 0 {
			m.entityViewScroll--
		}
	case "pgdown", "ctrl+d":
		m.entityViewScroll += taskViewPageStep(m.entityViewportHeight())
	case "pgup", "ctrl+u":
		m.entityViewScroll -= taskViewPageStep(m.entityViewportHeight())
		if m.entityViewScroll < 0 {
			m.entityViewScroll = 0
		}
	case "home", "g":
		m.entityViewScroll = 0
	case "end", "G":
		m.entityViewScroll = 1 << 20
	}
	return m, nil
}

func (m *Model) closeEntityScreen(status string) {
	m.clearDeletePrompt("")
	m.entityScreen = entityScreenClosed
	m.entityForm = entityForm{}
	m.status = status
	m.entityViewScroll = 0
}

func (m *Model) clearDeletePrompt(status string) {
	m.deletePending = false
	m.deleteKind = entityKindLaw
	m.deleteSlug = ""
	if status != "" {
		m.status = status
	}
}

func (m Model) renderEntityScreen() string {
	switch m.entityForm.mode {
	case entityScreenView:
		return m.renderEntityView()
	case entityScreenSkillPicker:
		return m.renderPersonaPicker()
	case entityScreenThemePicker:
		return m.renderThemePicker()
	case entityScreenConfigPicker:
		return m.renderConfigPicker()
	case entityScreenDefaultPicker:
		return m.renderTemplateDefaultPicker()
	}
	return ""
}

func (m Model) renderEntityView() string {
	const (
		entityDetailLabelWidth = 14
		entityDetailMinValue   = 20
		entityDetailMaxValue   = 140
	)
	available := m.availableWidth() - 4
	minTotal := entityDetailLabelWidth + entityDetailMinValue + 3
	maxTotal := entityDetailLabelWidth + entityDetailMaxValue + 3
	totalWidth := clampInt(available, minTotal, maxTotal)
	valueWidth := totalWidth - entityDetailLabelWidth - 3

	labelCell := func(label string) string {
		return m.styles.info.Render("// " + strings.ToUpper(label))
	}

	header := m.styles.kicker(fmt.Sprintf("%s · %s", m.entityForm.kind.String(), m.entityForm.slug))

	var dataRows [][]string
	var body string
	var extraSpannedRows [][]string

	switch m.entityForm.kind {
	case entityKindLaw:
		law, ok := m.findLawBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Law not found"), 2)
		}
		badge := m.severityStyle(domain.LawSeverity(law.Severity)).Render(law.Severity)
		dataRows = [][]string{
			{labelCell("Slug"), law.Key},
			{labelCell("Severity"), badge},
			{labelCell("Source"), law.SourcePath},
		}
		body = law.Body
	case entityKindSkill:
		skill, ok := m.findSkillBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Skill not found"), 2)
		}
		dataRows = [][]string{
			{labelCell("Slug"), skill.Key},
			{labelCell("Name"), skill.Name},
			{labelCell("Description"), skill.Description},
			{labelCell("Source"), skill.SourcePath},
		}
		body = skill.Body
	case entityKindPersona:
		persona, ok := m.findPersonaBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Persona not found"), 2)
		}
		skills := strings.Join(persona.SkillKeys, ", ")
		if skills == "" {
			skills = m.styles.hint.Render("none")
		}
		dataRows = [][]string{
			{labelCell("Slug"), persona.Key},
			{labelCell("Name"), persona.Name},
			{labelCell("Description"), persona.Description},
			{labelCell("Skills"), skills},
			{labelCell("Source"), persona.SourcePath},
		}
		body = persona.Body
		extraSpannedRows = [][]string{{m.styles.hint.Render("p: open skill picker")}}
	case entityKindTemplate:
		template, ok := m.findTemplateBySlug(m.entityForm.slug)
		if !ok {
			return "\n" + indentBlock(m.styles.panel.Render("Template not found"), 2)
		}
		entity := template.Entity
		if entity == "" {
			entity = m.styles.hint.Render("none")
		}
		defaultLabel := m.styles.hint.Render("none")
		if template.Default != "" {
			text := template.Default
			if template.ProjectSlug != "" {
				text += "  (project: " + template.ProjectSlug + ")"
			} else {
				text += "  (global)"
			}
			defaultLabel = m.styles.badgeInfo.Render(strings.ToUpper(text))
		}
		dataRows = [][]string{
			{labelCell("Slug"), template.Slug},
			{labelCell("Name"), template.Name},
			{labelCell("Description"), template.Description},
			{labelCell("Entity"), entity},
			{labelCell("Default"), defaultLabel},
			{labelCell("Source"), template.SourcePath},
		}
		body = template.Body
		extraSpannedRows = [][]string{{m.styles.hint.Render("a: assign default kind")}}
	}

	bodyText := strings.TrimRight(body, "\n")
	if strings.TrimSpace(bodyText) == "" {
		bodyText = m.styles.hint.Render("Empty body")
	}

	rows := [][]string{{header}}
	rows = append(rows, dataRows...)
	rows = append(rows,
		[]string{m.styles.kicker("Body")},
		[]string{bodyText},
	)
	rows = append(rows, extraSpannedRows...)

	table := renderGridTable(rows, []int{entityDetailLabelWidth, valueWidth}, m.styles.border)
	return m.applyEntityViewScroll(table)
}

// entityViewportHeight is the line budget for the entity detail view content
// between header and footer. Returns 0 when the height is unknown or too small
// to scroll usefully — callers should render everything and let the terminal
// scroll natively in that case.
func (m Model) entityViewportHeight() int {
	if m.height <= 0 {
		return 0
	}
	chrome := 5 // header(2) + leading blank(1) + footer(2)
	if m.status != "" {
		chrome++
	}
	h := m.height - chrome
	if h < 8 {
		return 0
	}
	return h
}

// applyEntityViewScroll slices the rendered grid to the available viewport
// based on m.entityViewScroll. Operates on the post-render line list (no
// height heuristics) so very tall bodies behave deterministically.
func (m Model) applyEntityViewScroll(content string) string {
	viewport := m.entityViewportHeight()
	lines := strings.Split(content, "\n")
	if viewport <= 0 || len(lines) <= viewport {
		return "\n" + indentBlock(content, 2)
	}
	visible, above, below := sliceViewport(lines, m.entityViewScroll, viewport-1)
	return "\n" + indentBlock(strings.Join(visible, "\n")+"\n"+m.viewportFooterHint(above, below), 2)
}
