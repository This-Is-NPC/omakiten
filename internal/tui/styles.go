package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
)

// kicker renders a section label in dev-editorial style. Structural labels use
// the secondary color so the primary accent stays reserved for active focus.
func (s styles) kicker(label string) string {
	return s.info.Render("// " + strings.ToUpper(label))
}

// kickerFocused is the focused-panel variant: replaces `//` with `▸` and
// flips to the primary accent. Used to mark which side of the task screen
// owns navigation keys without painting the full panel border green.
func (s styles) kickerFocused(label string) string {
	return s.hintAccent.Render("▸ " + strings.ToUpper(label))
}

// kickerCount renders `// LABEL · N` — kicker with a trailing count.
func (s styles) kickerCount(label string, count int) string {
	return s.info.Render(fmt.Sprintf("// %s · %d", strings.ToUpper(label), count))
}

// kickerCountFocused is the focused-panel variant of kickerCount.
func (s styles) kickerCountFocused(label string, count int) string {
	return s.hintAccent.Render(fmt.Sprintf("▸ %s · %d", strings.ToUpper(label), count))
}

// metaRow renders a definition-list row: `// LABEL` (kicker) + value, the label
// padded to labelWidth so values align across multiple rows.
func (s styles) metaRow(label, value string, labelWidth int) string {
	rendered := "// " + strings.ToUpper(label)
	pad := labelWidth - lipgloss.Width(rendered)
	if pad < 1 {
		pad = 1
	}
	return s.info.Render(rendered) + strings.Repeat(" ", pad) + value
}

// badgeForColor returns the lipgloss style that paints a config-driven
// badge (priority, severity) in the requested theme color. The accepted
// tokens are the four theme semantic names — `error`, `warning`,
// `success`, `info` — so config.{priorities,severities}[].color stays
// a stable enum and theme authors only have to edit palette tokens in
// one place. Unknown / empty colors fall back to the neutral info
// badge so the renderer never emits an unstyled pill.
func (s styles) badgeForColor(color string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(color)) {
	case "error":
		return s.badgeHigh
	case "warning":
		return s.badgeFix
	case "success":
		return s.badgeNormal
	case "info":
		return s.badgeInfo
	}
	return s.badgeInfo
}

// statusBadge renders a status message as `[INFO] msg` or `[ERROR] msg` based
// on a content heuristic. Replaces italic-on-secondary status rendering.
func (s styles) statusBadge(msg string) string {
	if msg == "" {
		return ""
	}
	level := "INFO"
	tagStyle := s.info
	lower := strings.ToLower(msg)
	for _, needle := range []string{"confirm", "pending"} {
		if strings.Contains(lower, needle) {
			level = "WARN"
			tagStyle = s.warning
			break
		}
	}
	for _, needle := range []string{"error", "fail", "not found", "required", "missing", "invalid", "exceeded"} {
		if strings.Contains(lower, needle) {
			level = "ERROR"
			tagStyle = s.error
			break
		}
	}
	return tagStyle.Render("["+level+"]") + " " + s.muted.Render(msg)
}

type styles struct {
	title           lipgloss.Style
	nav             lipgloss.Style
	activeNav       lipgloss.Style
	panel           lipgloss.Style
	commentCard     lipgloss.Style
	systemEventCard lipgloss.Style
	commentInput    lipgloss.Style
	border          lipgloss.Style
	kanbanColumn    lipgloss.Style
	card            lipgloss.Style
	cardSelected    lipgloss.Style
	entityCard      lipgloss.Style
	entityCardSelected lipgloss.Style
	marker          lipgloss.Style
	separator       lipgloss.Style
	empty           lipgloss.Style
	input           lipgloss.Style
	multilineInput  lipgloss.Style
	footer          lipgloss.Style
	hint            lipgloss.Style
	hintAccent      lipgloss.Style
	hintBox         lipgloss.Style
	muted           lipgloss.Style
	info            lipgloss.Style
	success         lipgloss.Style
	warning         lipgloss.Style
	error           lipgloss.Style

	badgeHigh        lipgloss.Style
	badgeNormal      lipgloss.Style
	badgeLow         lipgloss.Style
	badgeBlocker     lipgloss.Style
	badgeComment     lipgloss.Style
	badgeInfo        lipgloss.Style
	badgeScope       lipgloss.Style
	badgeFix         lipgloss.Style
	badgeTokenGreen  lipgloss.Style
	badgeTokenYellow lipgloss.Style
	badgeTokenRed    lipgloss.Style

	// archivedCard renders archived tasks dimmed when the `A` toggle exposes
	// them in board/table/graph. Strikethrough doubles as a redundant cue
	// for users with limited color contrast.
	archivedCard lipgloss.Style
}

func newStyles(theme config.Theme) styles {
	color := func(key, fallback string) lipgloss.Color {
		if value := theme.Colors[key]; value != "" {
			return lipgloss.Color(value)
		}
		return lipgloss.Color(fallback)
	}

	border := color("border", "#494543")
	foreground := color("foreground", "#E5E2E1")
	primary := color("primary", "#39FF14")
	secondary := color("secondary", "#8FAE9A")
	success := color("success", "#86D27A")
	warning := color("warning", "#FFB347")
	errorColor := color("error", "#FF5544")
	// badgeFg is the foreground used on filled-pill badges (dark text on a
	// bright background). Themable via the `badge_fg` color so dark-themed
	// palettes can override it.
	badgeFg := color("badge_fg", "#1A1A1A")

	return styles{
		title:          lipgloss.NewStyle().Bold(true).Foreground(primary),
		nav:            lipgloss.NewStyle().Foreground(secondary),
		activeNav:      lipgloss.NewStyle().Foreground(primary).Bold(true),
		panel:          lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		commentCard:    lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		commentInput:   lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Height(commentInputHeight),
		// systemEventCard mirrors the commentCard geometry (border + padding)
		// so the activity column stays visually consistent — same column
		// alignment, same width budget. The metadata cue comes from the text
		// color, not a different border color.
		systemEventCard: lipgloss.NewStyle().Foreground(secondary).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		border:         lipgloss.NewStyle().Foreground(border),
		kanbanColumn:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(border).Width(columnWidth).Padding(0, 0),
		card:           lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
		cardSelected:   lipgloss.NewStyle().Foreground(foreground).Bold(true).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(cardBoxWidth),
		entityCard:     lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
		entityCardSelected: lipgloss.NewStyle().Foreground(foreground).Bold(true).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(cardBoxWidth),
		marker:         lipgloss.NewStyle().Foreground(primary).Bold(true),
		separator:      lipgloss.NewStyle().Foreground(border),
		empty:          lipgloss.NewStyle().Foreground(border).Width(columnWidth).Align(lipgloss.Center),
		// Default border color is the muted `border` token; the form
		// helpers in render_task.go opt-in to the `primary` accent only
		// when their field is focused. Without this default, every input
		// in the create/edit form would render with the green border the
		// user reported as confusing — the eye lost which field was the
		// active one.
		input:          lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		multilineInput: lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2).Width(taskFormInputWidth).Height(taskDescriptionInputHeight),
		footer:         lipgloss.NewStyle().Foreground(border),
		hint:           lipgloss.NewStyle().Foreground(border),
		hintAccent:     lipgloss.NewStyle().Foreground(primary).Bold(true),
		hintBox:        lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2).Width(60),
		muted:          lipgloss.NewStyle().Foreground(border),
		info:           lipgloss.NewStyle().Foreground(secondary),
		success:        lipgloss.NewStyle().Foreground(success),
		warning:        lipgloss.NewStyle().Foreground(warning),
		error:          lipgloss.NewStyle().Foreground(errorColor),

		badgeHigh:        lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeNormal:      lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeLow:         lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeBlocker:     lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeComment:     lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		badgeInfo:        lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeScope:       lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		badgeFix:         lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenGreen:  lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenYellow: lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenRed:    lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),

		archivedCard: lipgloss.NewStyle().Foreground(border).Strikethrough(true).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
	}
}
