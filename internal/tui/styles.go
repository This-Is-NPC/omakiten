package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"omakiten/internal/config"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/screenkit"
)

// kicker renders a section label in dev-editorial style. Structural labels use
// the secondary color so the primary accent stays reserved for active focus.
func (s styles) kicker(label string) string {
	return screenkit.Kicker(s.info, label)
}

// multilineFormTheme bundles the styles the shared multiline field area needs
// into the value type that field.RenderArea and Resize accept.
//
// The derivation itself lives in components/field, over the projected Styles
// rather than over the root's private fields, so a fixture and the running app
// build the same chrome from the same function. What stays here is only the
// projection this host hands it.
func (s styles) multilineFormTheme() field.Theme { return field.Multiline(s.kit) }

// taskEditTheme is the same story one size up: the task form's chrome, derived
// by components/field from the styles a screen is allowed to see.
func (s styles) taskEditTheme() field.FormTheme { return field.Form(s.kit) }

// Status messages share one terminal boundary. Keep the visible copy to one
// logical line and at most maxStatusTextCells cells. The secondary rune bound
// is deliberately finite so combining marks and other zero-width input cannot
// bypass the visible-cell cap and grow the rendered status without limit.
const (
	maxStatusTextCells = 240
	maxStatusTextRunes = 2 * maxStatusTextCells
)

// statusBadge renders a status message as `[INFO] msg` or `[ERROR] msg` based
// on a content heuristic. Untrusted producer text is made safe and bounded
// before it can influence either classification or styling.
func (s styles) statusBadge(msg string) string {
	msg = safeStatusText(msg)
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

func safeStatusText(msg string) string {
	// Preserve word boundaries for control whitespace before Sanitize removes
	// controls. Sequence introducers are deliberately left intact so ANSI/OSC
	// stripping still consumes their payloads as a unit.
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, msg)
	msg = strings.Join(strings.Fields(screenkit.Sanitize(msg)), " ")
	if utf8.RuneCountInString(msg) > maxStatusTextRunes {
		kept := 0
		for index := range msg {
			if kept == maxStatusTextRunes-1 {
				msg = msg[:index] + "…"
				break
			}
			kept++
		}
	}
	return screenkit.Truncate(msg, maxStatusTextCells)
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
	cursor       lipgloss.Style
	border       lipgloss.Style
	card         lipgloss.Style
	cardSelected lipgloss.Style
	marker       lipgloss.Style
	separator    lipgloss.Style
	empty        lipgloss.Style
	input        lipgloss.Style
	// formMultiline is the bordered chrome shared by every multi-line
	// textarea form — task description, inline comment-add, comment-edit
	// overlay. Width and Height are intentionally not preset: the
	// field area leaf overrides both per-call from the
	// live terminal geometry, so baking them in here would either be
	// shadowed (silent dead state) or surface as a stale override on
	// resize.
	formMultiline lipgloss.Style
	footer        lipgloss.Style
	hint          lipgloss.Style
	hintAccent    lipgloss.Style
	hintBox       lipgloss.Style
	// hintTasks / hintComment / hintPlan / hintAudit / hintGuard / hintTrick /
	// hintToolCall paint the per-category accents in the Logs event inspector
	// (sub-task #325 wires them). Each resolves from a `category.<name>` theme
	// token; when a theme omits the token, newStyles falls back to the generic
	// `hint` color so older / custom themes never render an unstyled glyph.
	hintTasks    lipgloss.Style
	hintComment  lipgloss.Style
	hintPlan     lipgloss.Style
	hintAudit    lipgloss.Style
	hintGuard    lipgloss.Style
	hintTrick    lipgloss.Style
	hintToolCall lipgloss.Style
	muted        lipgloss.Style
	info         lipgloss.Style
	success      lipgloss.Style
	warning      lipgloss.Style
	error        lipgloss.Style

	badgeHigh        lipgloss.Style
	badgeNormal      lipgloss.Style
	badgeLow         lipgloss.Style
	badgeBlocker     lipgloss.Style
	badgeComment     lipgloss.Style
	badgeSubtask     lipgloss.Style
	badgeInfo        lipgloss.Style
	badgeScope       lipgloss.Style
	badgeFix         lipgloss.Style
	badgeActive      lipgloss.Style
	badgeTokenGreen  lipgloss.Style
	badgeTokenYellow lipgloss.Style
	badgeTokenRed    lipgloss.Style

	// archivedCard renders archived tasks dimmed when the `A` toggle exposes
	// them in board/table/graph. Strikethrough doubles as a redundant cue
	// for users with limited color contrast.
	archivedCard lipgloss.Style

	// kit is the screenkit projection, resolved once by newStyles. Read it
	// through screenStyles; never assign it from anywhere else, or the two
	// halves of the theme drift.
	kit screenkit.Styles
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

	// categoryColor resolves a Logs-event-category token (e.g. `category.tasks`)
	// from the active theme. When the token is missing or empty it falls back
	// to the generic `hint` color (`border` token, same fallback chain as the
	// `hint` style above) so themes that pre-date the Logs event inspector
	// keep rendering without panic or a default-black glyph.
	categoryColor := func(key string) lipgloss.Color {
		if value := theme.Colors[key]; value != "" {
			return lipgloss.Color(value)
		}
		return border
	}

	resolved := styles{
		title:       lipgloss.NewStyle().Bold(true).Foreground(primary),
		nav:         lipgloss.NewStyle().Foreground(secondary),
		activeNav:   lipgloss.NewStyle().Foreground(primary).Bold(true),
		panel:       lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		commentCard: lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		cursor:      lipgloss.NewStyle().Foreground(primary),
		// systemEventCard mirrors the commentCard geometry (border + padding)
		// so the activity column stays visually consistent — same column
		// alignment, same width budget. The metadata cue comes from the text
		// color, not a different border color.
		systemEventCard: lipgloss.NewStyle().Foreground(secondary).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1),
		border:          lipgloss.NewStyle().Foreground(border),
		card:            lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
		cardSelected:    lipgloss.NewStyle().Foreground(foreground).Bold(true).Border(lipgloss.NormalBorder()).BorderForeground(primary).Padding(0, 1).Width(cardBoxWidth),
		marker:          lipgloss.NewStyle().Foreground(primary).Bold(true),
		separator:       lipgloss.NewStyle().Foreground(border),
		empty:           lipgloss.NewStyle().Foreground(border).Width(columnWidth).Align(lipgloss.Center),
		// Default border color is the muted `border` token; the form
		// helpers in render_task.go opt-in to the `primary` accent only
		// when their field is focused. Without this default, every input
		// in the create/edit form would render with the green border the
		// user reported as confusing — the eye lost which field was the
		// active one.
		input: lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		// formMultiline holds the neutral-border defaults; field.RenderArea
		// swaps BorderForeground to the accent color when its `focused` flag
		// is true. Padding(0, 2) matches the surrounding panel chrome so the
		// inner textarea inherits the same horizontal rhythm as the rest of
		// the form. Width and Height are not preset — the leaf component owns
		// per-render geometry (see styles.multilineFormTheme).
		formMultiline: lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2),
		footer:        lipgloss.NewStyle().Foreground(border),
		hint:          lipgloss.NewStyle().Foreground(border),
		hintAccent:    lipgloss.NewStyle().Foreground(primary).Bold(true),
		hintBox:       lipgloss.NewStyle().Foreground(foreground).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 2).Width(60),
		hintTasks:     lipgloss.NewStyle().Foreground(categoryColor("category.tasks")),
		hintComment:   lipgloss.NewStyle().Foreground(categoryColor("category.comment")),
		hintPlan:      lipgloss.NewStyle().Foreground(categoryColor("category.plan")),
		hintAudit:     lipgloss.NewStyle().Foreground(categoryColor("category.audit")),
		hintGuard:     lipgloss.NewStyle().Foreground(categoryColor("category.guard")),
		hintTrick:     lipgloss.NewStyle().Foreground(categoryColor("category.trick")),
		hintToolCall:  lipgloss.NewStyle().Foreground(categoryColor("category.tool_call")),
		muted:         lipgloss.NewStyle().Foreground(border),
		info:          lipgloss.NewStyle().Foreground(secondary),
		success:       lipgloss.NewStyle().Foreground(success),
		warning:       lipgloss.NewStyle().Foreground(warning),
		error:         lipgloss.NewStyle().Foreground(errorColor),

		badgeHigh:    lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeNormal:  lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeLow:     lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeBlocker: lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeComment: lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		// badgeSubtask uses the neutral secondary tone — sub-tasks are
		// structure, not alarm, so it deliberately diverges from
		// badgeBlocker's warning colour.
		badgeSubtask:     lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeInfo:        lipgloss.NewStyle().Background(secondary).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeScope:       lipgloss.NewStyle().Background(border).Foreground(foreground).Padding(0, 1).Bold(true),
		badgeFix:         lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeActive:      lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenGreen:  lipgloss.NewStyle().Background(success).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenYellow: lipgloss.NewStyle().Background(warning).Foreground(badgeFg).Padding(0, 1).Bold(true),
		badgeTokenRed:    lipgloss.NewStyle().Background(errorColor).Foreground(badgeFg).Padding(0, 1).Bold(true),

		archivedCard: lipgloss.NewStyle().Foreground(border).Strikethrough(true).Border(lipgloss.NormalBorder()).BorderForeground(border).Padding(0, 1).Width(cardBoxWidth),
	}
	// Project once, here, rather than on every read. The screen contract is
	// forty-odd styles wide and is now read per BADGE, not just per frame, so
	// rebuilding it on each call would put a struct copy of the whole theme on
	// every pill a board column paints.
	resolved.kit = resolved.project()
	return resolved
}

// ScreenStyles resolves a theme straight into the screenkit contract for hosts
// that paint components outside the TUI event loop. The dev-only component
// gallery (cmd/okt-gallery) is the only caller: it renders each component
// against the shipped theme rather than a fixture palette.
//
// screenStyles itself stays unexported because a screen must reach its styles
// through the Kit its host hands it — this entry point exists for hosts with no
// root Model to build a Kit from, not as a second door into the same styles.
func ScreenStyles(theme config.Theme) screenkit.Styles {
	return newStyles(theme).screenStyles()
}

// screenStyles is the resolved theme projected onto the subset an extracted
// screen is allowed to paint with, computed once by newStyles.
func (s styles) screenStyles() screenkit.Styles { return s.kit }

// project builds that projection. It is the ONE place root styles cross into
// screenkit, so a screen can never reach a style the contract does not grant.
// Called only by newStyles — everything else reads the memoised result.
func (s styles) project() screenkit.Styles {
	return screenkit.Styles{
		Panel:            s.panel,
		Border:           s.border,
		Separator:        s.separator,
		Marker:           s.marker,
		Info:             s.info,
		Hint:             s.hint,
		HintAccent:       s.hintAccent,
		HintBox:          s.hintBox,
		Empty:            s.empty,
		Error:            s.error,
		Warning:          s.warning,
		Success:          s.success,
		BadgeInfo:        s.badgeInfo,
		BadgeLow:         s.badgeLow,
		BadgeNormal:      s.badgeNormal,
		BadgeHigh:        s.badgeHigh,
		BadgeBlocker:     s.badgeBlocker,
		BadgeComment:     s.badgeComment,
		BadgeSubtask:     s.badgeSubtask,
		BadgeScope:       s.badgeScope,
		BadgeFix:         s.badgeFix,
		BadgeActive:      s.badgeActive,
		TokenGreen:       s.badgeTokenGreen,
		TokenYellow:      s.badgeTokenYellow,
		TokenRed:         s.badgeTokenRed,
		Card:             s.card,
		CardSelected:     s.cardSelected,
		CardArchived:     s.archivedCard,
		CommentCard:      s.commentCard,
		SystemEventCard:  s.systemEventCard,
		Input:            s.input,
		FormMultiline:    s.formMultiline,
		Cursor:           s.cursor,
		Nav:              s.nav,
		ActiveNav:        s.activeNav,
		CategoryTask:     s.hintTasks,
		CategoryComment:  s.hintComment,
		CategoryPlan:     s.hintPlan,
		CategoryAudit:    s.hintAudit,
		CategoryGuard:    s.hintGuard,
		CategoryTrick:    s.hintTrick,
		CategoryToolCall: s.hintToolCall,
	}
}
