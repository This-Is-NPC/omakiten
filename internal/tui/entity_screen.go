package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/tui/components/detailscreen"
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
			m.status = m.t("tui.status.template_remove_hint")
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
			m.status = m.t("tui.status.refreshed")
		}
	case "M":
		m.clearDeletePrompt("")
		m.toggleMarkdownRendered()
	default:
		var cmd tea.Cmd
		m.entityView, cmd = m.entityView.Update(msg, m.entityViewportHeight())
		return m, cmd
	}
	return m, nil
}

func (m *Model) closeEntityScreen(status string) {
	m.clearDeletePrompt("")
	m.entityScreen = entityScreenClosed
	m.entityForm = entityForm{}
	m.status = status
	m.entityView = detailscreen.New(0)
}

func (m *Model) clearDeletePrompt(status string) {
	m.deletePending = false
	m.deleteKind = entityKindLaw
	m.deleteSlug = ""
	m.taskDeletePendingID = 0
	m.commentDeletePendingID = 0
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
		entityDetailMinValue = 20
		entityDetailMaxValue = 140
	)
	available := m.availableWidth() - 4
	minTotal := detailscreen.LabelWidth + entityDetailMinValue + 3
	maxTotal := detailscreen.LabelWidth + entityDetailMaxValue + 3
	totalWidth := clampInt(available, minTotal, maxTotal)
	valueWidth := totalWidth - detailscreen.LabelWidth - 3

	header := m.styles.kicker(fmt.Sprintf("%s · %s", m.entityForm.kind.String(), m.entityForm.slug))

	type detailRow struct {
		label string
		value string
	}
	var dataRows []detailRow
	var body string
	var extraSpannedRows []string

	switch m.entityForm.kind {
	case entityKindLaw:
		law, ok := m.findLawBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel("Law not found")
		}
		// Severity badge: style comes from config-driven color token
		// (severityStyle reads m.severities[].color); label comes from
		// the registry-resolved name. This replaces the hardcoded
		// switch on the LawSeverity constants the previous version
		// used.
		badge := m.severityStyle(law.Severity).Render(m.severityLabel(law.Severity))
		dataRows = []detailRow{
			{label: "Slug", value: law.Key},
			{label: "Severity", value: badge},
			{label: "Source", value: law.SourcePath},
		}
		body = law.Body
	case entityKindSkill:
		skill, ok := m.findSkillBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel("Skill not found")
		}
		dataRows = []detailRow{
			{label: "Slug", value: skill.Key},
			{label: "Name", value: skill.Name},
			{label: "Description", value: skill.Description},
			{label: "Source", value: skill.SourcePath},
		}
		body = skill.Body
	case entityKindPersona:
		persona, ok := m.findPersonaBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel("Persona not found")
		}
		skills := strings.Join(persona.SkillKeys, ", ")
		if skills == "" {
			skills = m.styles.hint.Render(m.t("tui.empty.none"))
		}
		dataRows = []detailRow{
			{label: "Slug", value: persona.Key},
			{label: "Name", value: persona.Name},
			{label: "Description", value: persona.Description},
			{label: "Skills", value: skills},
			{label: "Source", value: persona.SourcePath},
		}
		body = persona.Body
		extraSpannedRows = []string{m.styles.hint.Render("p: open skill picker")}
	case entityKindTemplate:
		template, ok := m.findTemplateBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel("Template not found")
		}
		entity := template.Entity
		if entity == "" {
			entity = m.styles.hint.Render(m.t("tui.empty.none"))
		}
		defaultLabel := m.styles.hint.Render(m.t("tui.empty.none"))
		if template.Default != "" {
			text := template.Default
			if template.ProjectSlug != "" {
				text += "  (project: " + template.ProjectSlug + ")"
			} else {
				text += "  (global)"
			}
			defaultLabel = m.styles.badgeInfo.Render(strings.ToUpper(text))
		}
		dataRows = []detailRow{
			{label: "Slug", value: template.Slug},
			{label: "Name", value: template.Name},
			{label: "Description", value: template.Description},
			{label: "Entity", value: entity},
			{label: "Default", value: defaultLabel},
			{label: "Source", value: template.SourcePath},
		}
		body = template.Body
		extraSpannedRows = []string{m.styles.hint.Render("a: assign default kind")}
	}

	bodyText := m.renderBodyMarkdown(body, valueWidth)
	if strings.TrimSpace(bodyText) == "" {
		bodyText = m.styles.hint.Render(m.t("tui.empty.body"))
	}

	screen := m.entityView.Reset(valueWidth).Custom(header)
	for _, row := range dataRows {
		screen = screen.Row(row.label, row.value)
	}
	screen = screen.Kicker("Body").Span(bodyText)
	for _, row := range extraSpannedRows {
		screen = screen.Span(row)
	}
	return "\n" + indentBlock(screen.View(m.entityViewportHeight(), m.styles.border, m.styles.hint), 2)
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
