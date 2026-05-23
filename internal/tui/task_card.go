package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// taskCardSpec is the surface-agnostic input for renderTaskCard.
// Both the board's kanban cell and the plan network view build a
// spec, then defer the chrome (border style, padding, badge wrap,
// title wrap) to renderTaskCard so the visual language stays in one
// place. Adding a new card surface only requires building a spec —
// the bordered-pill aesthetic is decided here.
type taskCardSpec struct {
	// ID prefixes the title line as "#<id> ".
	ID int64
	// Title wraps within the card's inner content width.
	Title string
	// ExtraLines render between the wrapped title and the badge line.
	// Used for compact metadata like "@assignee" on plan cards;
	// callers can leave it nil for surfaces that show no extras.
	ExtraLines []string
	// Badges are pre-rendered (already coloured) pills; renderTaskCard
	// wraps them onto the badge line via wrapBadges so they break
	// across rows instead of overflowing.
	Badges []string
	// Selected swaps the border to the focused accent (cardSelected
	// style); used by both board and plan to highlight the cursor.
	Selected bool
	// Archived dims the card (archivedCard style); board surfaces
	// archived tasks under the A toggle, plan surfaces them in their
	// owning wave.
	Archived bool
	// Accent swaps the border to the accent foreground while keeping
	// width + padding. The plan network view uses this for
	// critical-path and next-claimable cards so a reviewer spots the
	// chain without losing the selection focus contract.
	Accent bool
	// BoxWidth is the outer card width (borders + padding + content).
	BoxWidth int
	// InnerWidth is the content width inside the borders + padding.
	// Callers compute this from layout helpers; passing it explicitly
	// keeps renderTaskCard free of layout-specific math.
	InnerWidth int
}

// renderTaskCard turns a taskCardSpec into the rendered card string.
// The chrome is the project's bordered-pill aesthetic from the kanban
// board, but the caller decides what content rows and badges land
// inside — the helper itself stays content-agnostic.
func (m Model) renderTaskCard(spec taskCardSpec) string {
	// Cursor chevron prepended on selected cards so the focused card
	// reads as the cursor target even when the primary-green
	// cardSelected border has to compete with adjacent accent rings
	// (next-claimable, critical-path) for the eye. Lipgloss measures
	// visual width on the styled string, so wrapWords math stays
	// honest about the prefix footprint.
	prefix := m.cursorChevron(spec.Selected) + fmt.Sprintf("#%d ", spec.ID)
	prefixWidth := lipgloss.Width(prefix)

	firstWidth := spec.InnerWidth - prefixWidth
	restWidth := spec.InnerWidth - prefixWidth
	if firstWidth < 1 {
		firstWidth = 1
	}
	if restWidth < 1 {
		restWidth = 1
	}

	wrapped := wrapWords(spec.Title, firstWidth, restWidth)
	lines := make([]string, 0, len(wrapped)+len(spec.ExtraLines)+1)
	for i, part := range wrapped {
		if i == 0 {
			lines = append(lines, prefix+part)
		} else {
			lines = append(lines, strings.Repeat(" ", prefixWidth)+part)
		}
	}
	for _, extra := range spec.ExtraLines {
		if extra == "" {
			continue
		}
		lines = append(lines, truncateText(extra, spec.InnerWidth))
	}
	if badgeLine := wrapBadges(spec.Badges, spec.InnerWidth); badgeLine != "" {
		lines = append(lines, badgeLine)
	}

	// Style priority: selected > archived > accent > default.
	// Selection wins because the cursor must always be visible;
	// archived is the next-strongest visual signal because the user
	// opted in via the A toggle and expects them dimmed; accent is
	// only a hint, so it loses to both. Default is the neutral border.
	//
	// Accent uses `info` (secondary token / muted teal) — NOT primary —
	// so the critical-path / next-claimable hint stays visually distinct
	// from `cardSelected` (primary green + Bold). Painting accent with
	// primary would collide with the selection border and hide the
	// cursor under the accent ring.
	style := m.styles.card.Width(spec.BoxWidth)
	switch {
	case spec.Selected:
		style = m.styles.cardSelected.Width(spec.BoxWidth)
	case spec.Archived:
		style = m.styles.archivedCard.Width(spec.BoxWidth)
	case spec.Accent:
		style = style.BorderForeground(m.styles.info.GetForeground())
	}
	return style.Render(strings.Join(lines, "\n"))
}

// cardHeightFromSpec computes the rendered card height the same way
// renderTaskCard would lay it out, without running the lipgloss
// border/padding pass. Used by board hot paths (syncFocusedColumnScroll)
// that previously rendered every card in the focused column just to
// count the resulting newlines — a per-keystroke walk that dominated
// the board refresh cost on long columns. Stays a method so it can
// reuse cursorChevron for prefix width.
//
// Accuracy contract: matches the renderer output when InnerWidth is
// wide enough to hold the prefix plus at least one title-word column.
// Pathologically narrow widths (InnerWidth < prefixWidth + 1) trigger
// lipgloss to wrap the prefix-prepended first line on render, which
// this helper does not model — the board layout (computeBoardLayout)
// enforces a much larger minimum so the divergence is unreachable in
// production.
func (m Model) cardHeightFromSpec(spec taskCardSpec) int {
	prefix := m.cursorChevron(spec.Selected) + fmt.Sprintf("#%d ", spec.ID)
	prefixWidth := lipgloss.Width(prefix)
	firstWidth := spec.InnerWidth - prefixWidth
	restWidth := firstWidth
	if firstWidth < 1 {
		firstWidth = 1
	}
	if restWidth < 1 {
		restWidth = 1
	}
	titleLines := len(wrapWords(spec.Title, firstWidth, restWidth))

	extraLines := 0
	for _, extra := range spec.ExtraLines {
		if extra != "" {
			extraLines++
		}
	}

	badgeLines := wrapBadgesLineCount(spec.Badges, spec.InnerWidth)

	// +2 for the top and bottom border applied by styles.card / cardSelected /
	// archivedCard — every variant uses a single-row border on each side.
	return titleLines + extraLines + badgeLines + 2
}

// wrapBadgesLineCount mirrors wrapBadges' packing algorithm but emits
// only the line count, avoiding the per-row strings.Join + final
// strings.Join wrapBadges does. Kept colocated with the layout helper
// so future wrap-policy tweaks (e.g. tighter separator) update both
// the renderer and the measurer in lockstep.
func wrapBadgesLineCount(badges []string, maxWidth int) int {
	if len(badges) == 0 {
		return 0
	}
	lines := 0
	currentWidth := 0
	currentCount := 0
	for _, badge := range badges {
		w := lipgloss.Width(badge)
		sep := 0
		if currentCount > 0 {
			sep = 1
		}
		if currentCount > 0 && currentWidth+sep+w > maxWidth {
			lines++
			currentCount = 1
			currentWidth = w
			continue
		}
		currentCount++
		currentWidth += sep + w
	}
	if currentCount > 0 {
		lines++
	}
	return lines
}
