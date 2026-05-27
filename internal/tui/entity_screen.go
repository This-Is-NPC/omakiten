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
	case entityScreenSubtaskKitPicker:
		return m.updateSubtaskKitPicker(msg)
	case entityScreenDefaultPicker:
		return m.updateTemplateDefaultPicker(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.deletePending {
			m.clearDeletePrompt(m.t("tui.status.delete_cancelled"))
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
	case entityScreenSubtaskKitPicker:
		return m.renderSubtaskKitPicker()
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
			return m.renderPanel(m.t("tui.empty.law_not_found"))
		}
		// Severity badge: style comes from config-driven color token
		// (severityStyle reads m.severities[].color); label comes from
		// the registry-resolved name. This replaces the hardcoded
		// switch on the LawSeverity constants the previous version
		// used.
		badge := m.severityStyle(law.Severity).Render(m.severityLabel(law.Severity))
		dataRows = []detailRow{
			{label: m.t("tui.row.slug"), value: law.Key},
			{label: m.t("tui.row.severity"), value: badge},
			{label: m.t("tui.row.source"), value: law.SourcePath},
		}
		body = law.Body
	case entityKindSkill:
		skill, ok := m.findSkillBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel(m.t("tui.empty.skill_not_found"))
		}
		dataRows = []detailRow{
			{label: m.t("tui.row.slug"), value: skill.Key},
			{label: m.t("tui.row.name"), value: skill.Name},
			{label: m.t("tui.row.description"), value: skill.Description},
			{label: m.t("tui.row.source"), value: skill.SourcePath},
		}
		body = skill.Body
	case entityKindPersona:
		persona, ok := m.findPersonaBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel(m.t("tui.empty.persona_not_found"))
		}
		skills := strings.Join(persona.SkillKeys, ", ")
		if skills == "" {
			skills = m.styles.hint.Render(m.t("tui.empty.none"))
		}
		dataRows = []detailRow{
			{label: m.t("tui.row.slug"), value: persona.Key},
			{label: m.t("tui.row.name"), value: persona.Name},
			{label: m.t("tui.row.description"), value: persona.Description},
			{label: m.t("tui.row.skills"), value: skills},
			{label: m.t("tui.row.source"), value: persona.SourcePath},
		}
		body = persona.Body
		extraSpannedRows = []string{m.styles.hint.Render(m.t("tui.entity_screen.persona_skill_pick"))}
	case entityKindTemplate:
		template, ok := m.findTemplateBySlug(m.entityForm.slug)
		if !ok {
			return m.renderPanel(m.t("tui.empty.template_not_found"))
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
			{label: m.t("tui.row.slug"), value: template.Slug},
			{label: m.t("tui.row.name"), value: template.Name},
			{label: m.t("tui.row.description"), value: template.Description},
			{label: m.t("tui.row.entity"), value: entity},
			{label: m.t("tui.row.default"), value: defaultLabel},
			{label: m.t("tui.row.source"), value: template.SourcePath},
		}
		body = template.Body
		extraSpannedRows = []string{m.styles.hint.Render(m.t("tui.entity_screen.template_set_default"))}
	}

	bodyText := m.renderBodyMarkdown(body, valueWidth)
	if strings.TrimSpace(bodyText) == "" {
		bodyText = m.styles.hint.Render(m.t("tui.empty.body"))
	}

	screen := m.entityView.Reset(valueWidth).Custom(header)
	for _, row := range dataRows {
		screen = screen.Row(row.label, row.value)
	}
	screen = screen.Kicker(m.t("tui.kicker.body")).Span(bodyText)
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
