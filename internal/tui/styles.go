package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/multilineform"
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

// kanbanColumnSized returns the kanban-column style sized to the given
// inner width and (optional) viewport budget. innerHeight is the number
// of rows the box should occupy on screen — pass 0 to keep the
// content-sized default. Centralising the policy here means every
// card-in-column surface (board lanes, settings entity grid) closes its
// bottom border on the same row regardless of how many cards fit.
func (s styles) kanbanColumnSized(innerWidth, innerHeight int) lipgloss.Style {
	style := s.kanbanColumn.Width(innerWidth)
	if innerHeight > 0 {
		style = style.Height(innerHeight)
	}
	return style
}

// multilineFormTheme bundles the styles the shared multiline-form leaf
// needs into the value type that components/multilineform.Render and
// Resize accept. One canonical theme drives the task description, the
// inline comment-add modal, and the comment-edit overlay so the three
// surfaces render with identical chrome — the prior split between
// `multilineInput` (Padding 0,2 / neutral border default) and
// `commentInput` (Padding 0,1 / always-accent border) caused subtle
// visual drift between forms and was the trigger for the unification.
func (s styles) multilineFormTheme() multilineform.Theme {
	return multilineform.Theme{
		Border:       s.formMultiline,
		BorderActive: s.hintAccent.GetForeground(),
		Cursor:       s.cursor,
	}
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
	// cursor styles the visible-state of the bubbles cursor inside any
	// textarea/textinput. Set Foreground=primary so the cursor.View()
	// reverse-pass renders as a primary-bg block over a primary-fg char,
	// guaranteeing visibility regardless of the surrounding line style.
	cursor          lipgloss.Style
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
	// formMultiline is the bordered chrome shared by every multi-line
	// textarea form — task description, inline comment-add, comment-edit
	// overlay. Width and Height are intentionally not preset: the
	// components/multilineform leaf overrides both per-call from the
	// live terminal geometry, so baking them in here would either be
	// shadowed (silent dead state) or surface as a stale override on
	// resize.
	formMultiline lipgloss.Style
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
	badgeSubtask     lipgloss.Style
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
		cursor:         lipgloss.NewStyle().Foreground(primary),
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
		// formMultiline holds the neutral-border defaults; multilineform.Render
		// swaps BorderForeground to the accent color when its `focused` flag
		// is true. Padding(0, 2) matches the surrounding panel chrome so the
		// inner textarea inherits the same horizontal rhythm as the rest of
		// the form. Width and Height are not preset — the leaf component owns
		// per-render geometry (see styles.multilineFormTheme).
		formMultiline: lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
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
		// badgeSubtask uses the neutral secondary tone — sub-tasks are
		// structure, not alarm, so it deliberately diverges from
		// badgeBlocker's warning colour.
		badgeSubtask:     lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeInfo:        lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeScope:       lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		badgeFix:         lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenGreen:  lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenYellow: lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenRed:    lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),

		archivedCard: lipgloss.NewStyle().Foreground(border).Strikethrough(true).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
	}
}
