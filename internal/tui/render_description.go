package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/viewport"
)

// renderDescriptionScreen renders the focused, full-width description
// overlay opened with `f` from the task detail view. Long descriptions
// overflow the form column when inlined, so this surface gives them
// the entire available width and a dedicated scroll budget. Mirrors
// renderCommentScreen's layout so the two overlays read as the same
// component with different content slots.
func (m Model) renderDescriptionScreen() string {
	task, ok := m.activeTask()
	if !ok {
		notFound := []string{
			m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.description_fmt"), m.taskID)),
			"",
			m.styles.hint.Render(m.t("tui.empty.task_not_found_refresh")),
		}
		return m.renderPanel(strings.Join(notFound, "\n"))
	}

	available := m.availableWidth()
	// Focus overlay deliberately drops the 120-col cap that
	// renderCommentScreen uses — `f` exists to give the description
	// every column the terminal offers, so wide screens should not
	// see whitespace gutters on the right edge.
	valueWidth := available - detailscreen.LabelWidth - 1 - 2
	if valueWidth < 24 {
		valueWidth = 24
	}

	screen := m.descriptionScreen.Reset(valueWidth).
		Custom(m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.description_fmt"), task.ID))).
		Row(m.t("tui.row.title"), task.Title).
		Row(m.t("tui.row.bucket"), task.BucketKey)
	screen = screen.Kicker(m.t("tui.kicker.description"))
	body := strings.TrimSpace(task.Description)
	if body == "" {
		screen = screen.Span(m.styles.hint.Render(m.t("tui.empty.task_no_description")))
	} else {
		// Pass the whole body as a single spanned row so the gridtable
		// wraps it inline. Emitting one row per line would draw a
		// horizontal border between every wrapped line and read like a
		// price list — same fix the comment overlay uses.
		screen = screen.Span(m.renderBodyMarkdown(task.Description, valueWidth))
	}

	return "\n" + indentBlock(screen.View(m.taskViewportHeight(), m.styles.border, m.styles.hint), 2)
}

// openDescriptionScreen opens the dedicated description overlay for the
// task currently in the detail view. Resets the embedded detailscreen
// so the body always opens at the top.
func (m *Model) openDescriptionScreen(_ domain.Task) {
	m.descriptionScreenOpen = true
	m.descriptionScreen = detailscreen.New(0)
}

// closeDescriptionScreen returns the user to the task detail view.
// Focus state on the underlying screen (form / sub-tasks / activity)
// survives the round-trip, so esc lands them back where they were.
func (m *Model) closeDescriptionScreen() {
	m.descriptionScreenOpen = false
	m.descriptionScreen = detailscreen.New(0)
}

// updateDescriptionScreen runs the key handler while the description
// overlay is on screen. Delegates scrolling to the embedded
// detailscreen sub-model; esc cancellation closes the overlay. `M`
// toggles the markdown-render toggle so users can flip raw / rendered
// inside the focus view, mirroring the comment overlay binding.
func (m *Model) updateDescriptionScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "f":
		m.closeDescriptionScreen()
		return m, nil
	case "M":
		m.toggleMarkdownRendered()
		return m, nil
	}
	var cmd tea.Cmd
	m.descriptionScreen, cmd = m.descriptionScreen.Update(msg, m.taskViewportHeight())
	if m.descriptionScreen.LastEvent() == viewport.EventCancel {
		m.closeDescriptionScreen()
	}
	return m, cmd
}
