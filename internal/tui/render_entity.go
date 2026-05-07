package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
)

// renderEntityCell builds the inner content of one entity column. The cards
// are clamped to the viewport budget computed from the terminal height: cards
// outside the visible window are summarized as "▲ N above" / "▼ N below"
// hints exactly like the kanban columns. Same scroll model: per-kind offset
// stored in m.entityScroll, kept in sync with the cursor by
// syncFocusedEntityScroll on every cursor move.
func (m Model) renderEntityCell(kind entityKind) string {
	return m.renderEntityCellWithViewport(kind, m.entityViewportRows())
}

// renderEntityCellWithViewport is the same as renderEntityCell but lets the
// caller override the viewport budget — useful for renderConfig where we
// already know how many rows the tables above the entity grid consumed and
// can pass an exact number rather than relying on a static chrome estimate.
func (m Model) renderEntityCellWithViewport(kind entityKind, viewport int) string {
	focused := m.entityKind == kind
	count := m.entityCount(kind)
	cursor := m.selectedEntityIndex(kind)

	headerStyle := m.styles.hintAccent
	if !focused {
		headerStyle = m.styles.muted
	}
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(kind.plural()), count)

	lines := []string{
		headerStyle.Render(headerText),
		m.styles.separator.Render(strings.Repeat("─", entityListWidth)),
	}

	if count == 0 {
		lines = append(lines, m.styles.empty.Render("empty"))
		return strings.Join(lines, "\n")
	}

	rendered := make([]string, count)
	heights := make([]int, count)
	for index := 0; index < count; index++ {
		rendered[index] = m.renderEntityCard(kind, index, focused && index == cursor)
		heights[index] = strings.Count(rendered[index], "\n") + 1
	}

	if viewport <= 0 {
		// Height unknown — render every card; the renderer-level clamp keeps
		// the view bounded by terminal height.
		lines = append(lines, rendered...)
		return strings.Join(lines, "\n")
	}

	offset := 0
	if m.entityScroll != nil {
		offset = m.entityScroll[kind]
	}
	if offset < 0 {
		offset = 0
	}
	if offset > count-1 {
		offset = count - 1
	}

	used := 0
	end := offset
	for end < count {
		reserve := 0
		if end < count-1 {
			reserve = 1 // "▼ N below" hint line
		}
		if used+heights[end]+reserve > viewport {
			break
		}
		used += heights[end]
		end++
	}
	if end == offset {
		// Never produce an empty viewport — show at least one card.
		end = offset + 1
	}

	above := offset
	below := count - end
	if above > 0 {
		lines = append(lines, m.styles.hint.Render(fmt.Sprintf("▲ %d above", above)))
	}
	lines = append(lines, rendered[offset:end]...)
	if below > 0 {
		lines = append(lines, m.styles.hint.Render(fmt.Sprintf("▼ %d below", below)))
	}
	return strings.Join(lines, "\n")
}

// entityViewportRows is the number of terminal rows available for entity
// cards inside one column. Computed from the actual rendered config header
// (runtime/tokens tables) rather than a static chrome guess so the value
// stays correct as the tables grow or shrink across themes/data sets.
// Returns 0 when the height is unknown.
func (m Model) entityViewportRows() int {
	return m.entityCardsViewport(m.renderConfigHeader())
}

// syncFocusedEntityScroll keeps m.entityScroll[focusedKind] aligned so the
// selected card stays fully inside the column viewport. Mirrors the
// per-bucket scroll behavior of the kanban board: the offset advances by
// real card heights (cards have variable heights because of badge wrapping).
func (m *Model) syncFocusedEntityScroll() {
	kind := m.entityKind
	count := m.entityCount(kind)
	viewport := m.entityViewportRows()
	if viewport <= 0 || count == 0 {
		if m.entityScroll != nil {
			delete(m.entityScroll, kind)
		}
		return
	}

	cursor := m.selectedEntityIndex(kind)
	heights := make([]int, count)
	for i := 0; i < count; i++ {
		rendered := m.renderEntityCard(kind, i, false)
		heights[i] = strings.Count(rendered, "\n") + 1
	}

	if m.entityScroll == nil {
		m.entityScroll = map[entityKind]int{}
	}
	offset := m.entityScroll[kind]
	if offset > cursor {
		offset = cursor
	}
	for offset < cursor {
		used := 0
		fits := true
		for i := offset; i <= cursor; i++ {
			reserve := 0
			if i < count-1 {
				reserve = 1
			}
			if used+heights[i]+reserve > viewport {
				fits = false
				break
			}
			used += heights[i]
		}
		if fits {
			break
		}
		offset++
	}
	if offset < 0 {
		offset = 0
	}
	if offset > count-1 {
		offset = count - 1
	}
	m.entityScroll[kind] = offset
}

func (m Model) renderEntityCard(kind entityKind, index int, selected bool) string {
	label := m.entityCardLabel(kind, index)
	wrapped := wrapWords(label, cardContentWidth, cardContentWidth)

	// Badges line (truncated to fit card width)
	badgeLine := m.renderEntityBadges(kind, index, cardContentWidth)

	lines := make([]string, 0, len(wrapped)+1)
	lines = append(lines, wrapped...)
	if badgeLine != "" {
		lines = append(lines, badgeLine)
	}

	style := m.styles.entityCard
	if selected {
		style = m.styles.entityCardSelected
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m Model) renderEntityBadges(kind entityKind, index int, maxWidth int) string {
	switch kind {
	case entityKindLaw:
		return wrapBadges(m.renderLawBadges(index), maxWidth)
	case entityKindPersona:
		return wrapBadges(m.renderPersonaBadges(index), maxWidth)
	case entityKindSkill:
		return wrapBadges(m.renderSkillBadges(index), maxWidth)
	case entityKindTemplate:
		return wrapBadges(m.renderTemplateBadges(index), maxWidth)
	case entityKindTag:
		return wrapBadges(m.renderTagBadges(index), maxWidth)
	}
	return ""
}

// wrapBadges joins badges with single-space separators, breaking onto a new
// line whenever the next badge would overflow maxWidth. Every badge is kept;
// no truncation. A badge wider than maxWidth on its own occupies its own line.
func wrapBadges(badges []string, maxWidth int) string {
	if len(badges) == 0 {
		return ""
	}
	var lines []string
	var current []string
	currentWidth := 0
	for _, badge := range badges {
		w := lipgloss.Width(badge)
		sep := 0
		if len(current) > 0 {
			sep = 1
		}
		if len(current) > 0 && currentWidth+sep+w > maxWidth {
			lines = append(lines, strings.Join(current, " "))
			current = []string{badge}
			currentWidth = w
			continue
		}
		current = append(current, badge)
		currentWidth += sep + w
	}
	if len(current) > 0 {
		lines = append(lines, strings.Join(current, " "))
	}
	return strings.Join(lines, "\n")
}

// customBadge returns a single CUSTOM marker styled as info — same visual
// weight as other scope-style badges so the user can scan a column for
// user-owned overrides at a glance.
func (m Model) customBadge() string {
	return m.styles.badgeInfo.Render("CUSTOM")
}

func (m Model) renderLawBadges(index int) []string {
	law := m.laws[index]
	var badges []string

	// Severity badge
	severity := domain.LawSeverity(law.Severity)
	var severityBadge string
	switch severity {
	case domain.LawSeverityError:
		severityBadge = m.styles.badgeHigh.Render("ERROR")
	case domain.LawSeverityWarning:
		severityBadge = m.styles.badgeBlocker.Render("WARNING")
	default:
		severityBadge = m.styles.badgeInfo.Render("INFO")
	}
	badges = append(badges, severityBadge)

	// Scope badge
	scope := "GLOBAL"
	switch law.Scope {
	case domain.LawScopeProject:
		scope = "PROJECT"
	case domain.LawScopePersona:
		scope = "PERSONA"
	}
	badges = append(badges, m.styles.badgeScope.Render(scope))

	// Token count: matches computeMetrics (key + body) so the per-entity weight
	// matches the totals shown in the Token budget panel.
	tokens := m.counter.Count(law.Key + " " + law.Body)
	badges = append(badges, m.tokenBadge(tokens))

	if strings.TrimSpace(law.Warning) != "" {
		badges = append(badges, m.styles.badgeFix.Render("FIX"))
	}
	if law.IsCustom {
		badges = append(badges, m.customBadge())
	}

	return badges
}

func (m Model) renderPersonaBadges(index int) []string {
	persona := m.personas[index]
	// Token count: matches computeMetrics — only the description counts toward
	// the budget. Body is not bundled into context for personas.
	tokens := m.counter.Count(persona.Description)
	badges := []string{m.tokenBadge(tokens)}
	if strings.TrimSpace(persona.Warning) != "" {
		badges = append(badges, m.styles.badgeFix.Render("FIX"))
	}
	if persona.IsCustom {
		badges = append(badges, m.customBadge())
	}
	return badges
}

func (m Model) renderSkillBadges(index int) []string {
	skill := m.skills[index]
	// Skills are not part of the computeMetrics total — their bodies attach to
	// personas at injection time. The badge is informational so users can see
	// how heavy a skill body is before wiring it.
	tokens := m.counter.Count(skill.Body)
	badges := []string{m.tokenBadge(tokens)}
	if strings.TrimSpace(skill.Warning) != "" {
		badges = append(badges, m.styles.badgeFix.Render("FIX"))
	}
	if skill.IsCustom {
		badges = append(badges, m.customBadge())
	}
	return badges
}

func (m Model) renderTemplateBadges(index int) []string {
	template := m.templates[index]
	tokens := m.counter.Count(template.Body)
	badges := []string{m.tokenBadge(tokens)}
	// DEFAULT marks the template that is the active scaffold for a kind.
	// Project-scoped defaults include the project slug so the user can
	// distinguish them from the global default at a glance.
	if template.Default != "" {
		label := "DEFAULT:" + strings.ToUpper(template.Default)
		if template.ProjectSlug != "" {
			label += "·" + strings.ToUpper(template.ProjectSlug)
		}
		badges = append(badges, m.styles.badgeInfo.Render(label))
	}
	if template.IsCustom {
		badges = append(badges, m.customBadge())
	}
	return badges
}

func (m Model) tokenBadge(tokens int) string {
	label := fmt.Sprintf("TOKENS:%d", tokens)
	switch {
	case tokens > tokenBadgeRedAt:
		return m.styles.badgeTokenRed.Render(label)
	case tokens > tokenBadgeYellowAt:
		return m.styles.badgeTokenYellow.Render(label)
	default:
		return m.styles.badgeTokenGreen.Render(label)
	}
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
	label := fmt.Sprintf("USE:%d", tag.UsageCount)
	var badge string
	if tag.UsageCount == 0 {
		badge = m.styles.badgeHigh.Render(label)
	} else {
		badge = m.styles.badgeInfo.Render(label)
	}
	return []string{badge}
}

func (m Model) severityStyle(severity domain.LawSeverity) lipgloss.Style {
	switch severity {
	case domain.LawSeverityError:
		return m.styles.error
	case domain.LawSeverityWarning:
		return m.styles.warning
	case domain.LawSeverityInfo:
		return m.styles.info
	}
	return m.styles.muted
}
