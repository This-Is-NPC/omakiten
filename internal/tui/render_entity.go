package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/scrollwindow"
)

// renderEntityCell builds the inner content of one entity column.
// The cards wrap into multiple grid columns when the available width
// allows, and per-row scroll keeps the focused card on-screen with
// "▲ N above" / "▼ N below" summaries for hidden cards. Per-kind row
// offset stored in `m.entityScroll[kind]` (encoded as the first visible
// card index, snapped to a row boundary at render time).
func (m Model) renderEntityCell(kind entityKind) string {
	return m.renderEntityCellWithViewport(kind, m.entityViewportRows(), m.entityCellContentWidth())
}

// renderEntityCellWithViewport renders the wrapped entity grid with an
// explicit viewport budget and content-width budget — used by
// `renderSettingsEntity` (which knows the kanbanColumn inner width
// exactly). The grid packs cards left-to-right then top-to-bottom; each
// row's height is the tallest card in that row so the column stays
// visually flush regardless of badge wrap.
func (m Model) renderEntityCellWithViewport(kind entityKind, viewport int, contentWidth int) string {
	focused := m.entityKind == kind
	count := m.entityCount(kind)
	cursor := m.selectedEntityIndex(kind)

	headerStyle := m.styles.hintAccent
	if !focused {
		headerStyle = m.styles.muted
	}
	headerText := fmt.Sprintf("// %s · %d", strings.ToUpper(kind.plural()), count)

	separatorWidth := contentWidth
	if separatorWidth < entityListWidth {
		separatorWidth = entityListWidth
	}
	lines := []string{
		headerStyle.Render(headerText),
		m.styles.separator.Render(strings.Repeat("─", separatorWidth)),
	}

	if count == 0 {
		lines = append(lines, m.styles.empty.Render("empty"))
		return strings.Join(lines, "\n")
	}

	cols := entityGridCols(contentWidth)

	rendered := make([]string, count)
	cardHeights := make([]int, count)
	for index := 0; index < count; index++ {
		rendered[index] = m.renderEntityCard(kind, index, focused && index == cursor)
		cardHeights[index] = strings.Count(rendered[index], "\n") + 1
	}

	// Pack cards into rows of `cols`; each row's text is the JoinHorizontal
	// of its cards, and each row's height is the tallest card in the row.
	numRows := (count + cols - 1) / cols
	rowText := make([]string, numRows)
	rowHeights := make([]int, numRows)
	for r := 0; r < numRows; r++ {
		var cells []string
		h := 0
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= count {
				break
			}
			cells = append(cells, rendered[i])
			if cardHeights[i] > h {
				h = cardHeights[i]
			}
		}
		if len(cells) == 1 {
			rowText[r] = cells[0]
		} else {
			pieces := make([]string, 0, len(cells)*2-1)
			for idx, cell := range cells {
				if idx > 0 {
					pieces = append(pieces, " ")
				}
				pieces = append(pieces, cell)
			}
			rowText[r] = lipgloss.JoinHorizontal(lipgloss.Top, pieces...)
		}
		rowHeights[r] = h
	}

	if viewport <= 0 {
		// Height unknown — render every row; the screen-level clamp keeps
		// the view bounded by terminal height.
		lines = append(lines, rowText...)
		return strings.Join(lines, "\n")
	}

	storedCardOffset := 0
	if m.entityScroll != nil {
		storedCardOffset = m.entityScroll[kind]
	}
	rowOffset := storedCardOffset / cols
	// Shared scroll math (scrollwindow.Slice) returns the row-end; the
	// entity grid's only twist is that hints count CARDS, not rows, so
	// we translate row offsets to card counts when emitting the
	// indicator strings.
	end := scrollwindow.Slice(rowOffset, rowHeights, viewport, scrollwindow.HintsSplit)
	if rowOffset < 0 {
		rowOffset = 0
	}
	if rowOffset > numRows-1 {
		rowOffset = numRows - 1
	}
	cardsAbove := rowOffset * cols
	if cardsAbove > count {
		cardsAbove = count
	}
	cardsBelow := count - end*cols
	if cardsBelow < 0 {
		cardsBelow = 0
	}
	if cardsAbove > 0 {
		lines = append(lines, m.styles.hint.Render(fmt.Sprintf("▲ %d above", cardsAbove)))
	}
	lines = append(lines, rowText[rowOffset:end]...)
	if cardsBelow > 0 {
		lines = append(lines, m.styles.hint.Render(fmt.Sprintf("▼ %d below", cardsBelow)))
	}
	return strings.Join(lines, "\n")
}

// entityGridCols computes how many entity cards fit side-by-side inside
// the kanbanColumn at the given content width. `entityListWidth` is the
// inner width passed to the card style — `entityCardCellWidth` adds the
// ±1 border so we measure the visible card on the terminal grid.
func entityGridCols(contentWidth int) int {
	const horizontalGap = 1
	cell := entityCardCellWidth + horizontalGap
	if cell <= 0 {
		return 1
	}
	cols := (contentWidth + horizontalGap) / cell
	if cols < 1 {
		return 1
	}
	return cols
}

// entityCellContentWidth returns the width budget passed to the entity
// grid renderer when no explicit content width is supplied (e.g. by
// older callers / tests). Mirrors what `renderSettingsEntity` would
// compute given the current available width.
func (m Model) entityCellContentWidth() int {
	w := m.availableWidth() - 4
	if w < entityListWidth {
		return entityListWidth
	}
	return w
}

// entityViewportRows is the number of terminal rows available for entity
// cards inside the active Settings entity column. Sources its chrome
// from the shared `panelViewportRows` helper so it tracks live header /
// status / nav changes. Per-column chrome = 2 borders + 2 header rows
// (kicker / separator) = 4.
func (m Model) entityViewportRows() int {
	return m.panelViewportRows(4)
}

// syncFocusedEntityScroll keeps m.entityScroll[focusedKind] aligned so the
// selected card stays fully inside the column viewport. The offset is
// stored as a card index but always lands on a row boundary — the grid
// renderer wraps cards into rows of `entityGridCols(contentWidth)`, and
// scrolling jumps a whole row at a time so partial rows never appear at
// the top of the viewport.
func (m *Model) syncFocusedEntityScroll() {
	kind := m.entityKind
	count := m.entityCount(kind)
	viewport := m.entityViewportRows()
	contentWidth := m.entityCellContentWidth()
	if viewport <= 0 || count == 0 {
		if m.entityScroll != nil {
			delete(m.entityScroll, kind)
		}
		return
	}

	cols := entityGridCols(contentWidth)
	cursor := m.selectedEntityIndex(kind)
	cursorRow := cursor / cols
	numRows := (count + cols - 1) / cols

	rowHeights := make([]int, numRows)
	for r := 0; r < numRows; r++ {
		h := 0
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= count {
				break
			}
			rendered := m.renderEntityCard(kind, i, false)
			ch := strings.Count(rendered, "\n") + 1
			if ch > h {
				h = ch
			}
		}
		rowHeights[r] = h
	}

	if m.entityScroll == nil {
		m.entityScroll = map[entityKind]int{}
	}
	rowOffset := m.entityScroll[kind] / cols
	rowOffset = followScrollWindowSplit(rowOffset, cursorRow, rowHeights, viewport)
	m.entityScroll[kind] = rowOffset * cols
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

	// Severity badge — color comes from config.severities[].color via
	// styles.badgeForColor, so renaming or recoloring a severity in
	// YAML re-paints the badges without touching this switch.
	if badge := m.severityBadge(law.Severity); badge != "" {
		badges = append(badges, badge)
	}

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
	case tokens > m.tokenBadgeRed:
		return m.styles.badgeTokenRed.Render(label)
	case tokens > m.tokenBadgeYellow:
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

// severityStyle returns the foreground style for the severity badge
// label based on config.severities[].color. Same four-token enum as
// styles.badgeForColor (`error`, `warning`, `success`, `info`); unknown
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
