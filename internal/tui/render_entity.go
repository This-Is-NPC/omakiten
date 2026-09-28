package tui

import (
	"fmt"
	"omakiten/internal/tui/components/tokenstrip"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
)

func (m Model) entityBadges(kind entityKind, index int) []string {
	switch kind {
	case entityKindLaw:
		return m.renderLawBadges(index)
	case entityKindPersona:
		return m.renderPersonaBadges(index)
	case entityKindSkill:
		return m.renderSkillBadges(index)
	case entityKindTemplate:
		return m.renderTemplateBadges(index)
	case entityKindTag:
		return m.renderTagBadges(index)
	}
	return nil
}

// entityStateBadges are the trailing FIX / ACTIVE / CUSTOM markers, in the
// order every entity column reads them. All five families end this way; four
// of them used to spell it out.
func (m Model) entityStateBadges(warning string, active, custom bool) []string {
	s := m.styles.screenStyles()
	var badges []string
	if strings.TrimSpace(warning) != "" {
		badges = append(badges, tokenstrip.Fix(s, m.t))
	}
	if active {
		badges = append(badges, tokenstrip.Active(s, m.t))
	}
	if custom {
		badges = append(badges, tokenstrip.Custom(s, m.t))
	}
	return badges
}

func (m Model) renderLawBadges(index int) []string {
	law := m.laws[index]
	s := m.styles.screenStyles()
	var badges []string

	// Severity badge — color comes from config.severities[].color, so renaming
	// or recoloring a severity in YAML re-paints the badges.
	if pill := m.severityBadge(law.Severity); pill != "" {
		badges = append(badges, pill)
	}

	// Scope badge — only meaningful for laws wired into the active bundle.
	// Inactive catalog laws carry an empty Scope (no wiring), so rendering a
	// default GLOBAL badge would falsely assert a global binding; suppress it.
	if law.Active {
		scope := m.t("tui.badge.global")
		switch law.Scope {
		case domain.LawScopeProject:
			scope = m.t("tui.badge.project")
		case domain.LawScopePersona:
			scope = m.t("tui.badge.persona")
		}
		badges = append(badges, tokenstrip.Scope(s, scope))
	}

	// Token count: matches computeMetrics (key + body) so the per-entity weight
	// matches the totals shown in the Token budget panel.
	badges = append(badges, m.tokenBadge(m.counter.Count(law.Key+" "+law.Body)))

	return append(badges, m.entityStateBadges(law.Warning, law.Active, law.IsCustom)...)
}

func (m Model) renderPersonaBadges(index int) []string {
	persona := m.personas[index]
	// Token count: matches computeMetrics — only the description counts toward
	// the budget. Body is not bundled into context for personas.
	badges := []string{m.tokenBadge(m.counter.Count(persona.Description))}
	return append(badges, m.entityStateBadges(persona.Warning, persona.Active, persona.IsCustom)...)
}

func (m Model) renderSkillBadges(index int) []string {
	skill := m.skills[index]
	// Skills are not part of the computeMetrics total — their bodies attach to
	// personas at injection time. The badge is informational so users can see
	// how heavy a skill body is before wiring it.
	badges := []string{m.tokenBadge(m.counter.Count(skill.Body))}
	return append(badges, m.entityStateBadges(skill.Warning, skill.Active, skill.IsCustom)...)
}

func (m Model) renderTemplateBadges(index int) []string {
	template := m.templates[index]
	badges := []string{m.tokenBadge(m.counter.Count(template.Body))}
	// DEFAULT marks the template that is the active scaffold for a kind.
	// Project-scoped defaults include the project slug so the user can
	// distinguish them from the global default at a glance.
	if template.Default != "" {
		label := m.t("tui.badge.default_prefix") + ":" + strings.ToUpper(template.Default)
		if template.ProjectSlug != "" {
			label += "·" + strings.ToUpper(template.ProjectSlug)
		}
		badges = append(badges, m.styles.screenStyles().BadgeInfo.Render(label))
	}
	// A template carries no warning of its own, so the FIX slot stays empty.
	return append(badges, m.entityStateBadges("", template.Active, template.IsCustom)...)
}

func (m Model) tokenBadge(tokens int) string {
	return tokenstrip.Spend(m.styles.screenStyles(), m.t, tokens, m.tokenBadgeYellow, m.tokenBadgeRed)
}

func (m Model) entityCardLabel(kind entityKind, index int) string {
	switch kind {
	case entityKindLaw:
		return m.laws[index].Key
	case entityKindPersona:
		return m.personas[index].Key
	case entityKindSkill:
		return m.skills[index].Key
	case entityKindTemplate:
		return m.templates[index].Slug
	case entityKindTag:
		return m.tags[index].Label
	}
	return ""
}

func (m Model) renderTagBadges(index int) []string {
	tag := m.tags[index]
	label := fmt.Sprintf(m.t("tui.badge.use_fmt"), tag.UsageCount)
	// An unused tag is the one the user is being asked to act on, so it takes
	// the alarm tone rather than the neutral one.
	style := m.styles.screenStyles().BadgeInfo
	if tag.UsageCount == 0 {
		style = m.styles.screenStyles().BadgeHigh
	}
	return []string{style.Render(label)}
}

// severityStyle returns the foreground style for the severity badge
// label based on config.severities[].color. Same four-token enum as
// tokenstrip.ForColor (`error`, `warning`, `success`, `info`); unknown
// or empty values fall back to muted so the entity-screen badge keeps
// rendering. Theme authors edit palette tokens once and both badge
// types follow.
func (m Model) severityStyle(severity domain.Severity) lipgloss.Style {
	def, ok := m.severityByID(severity)
	if !ok {
		return m.styles.muted
	}
	switch strings.ToLower(strings.TrimSpace(def.Color)) {
	case "error":
		return m.styles.error
	case "warning":
		return m.styles.warning
	case "success":
		return m.styles.success
	case "info":
		return m.styles.info
	}
	return m.styles.muted
}
