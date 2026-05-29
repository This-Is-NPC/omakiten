package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/app"
	"omakiten/internal/tui/components/detailscreen"
	"omakiten/internal/tui/components/viewport"
)

// renderPlanGoalScreen renders the focused, full-width plan goal overlay
// opened with `f` from the plans list view. It mirrors
// renderDescriptionScreen: a read-only form header (slug / name / status)
// followed by the plan's markdown goal_body spanned across the full width
// with its own scroll budget. Editing the goal lives elsewhere (the
// modePlanGoal textarea) — this surface is view-only.
func (m Model) renderPlanGoalScreen() string {
	plan := m.planGoalShow.Plan

	available := m.availableWidth()
	// Drop the column cap so `f` gives the goal body every column the
	// terminal offers — same rationale as the description overlay.
	valueWidth := available - detailscreen.LabelWidth - 1 - 2
	if valueWidth < 24 {
		valueWidth = 24
	}

	screen := m.planGoalScreen.Reset(valueWidth).
		Custom(m.styles.kicker(fmt.Sprintf(m.t("tui.kicker.plan_goal_fmt"), plan.Slug))).
		Row(m.t("tui.row.name"), plan.Name).
		Row(m.t("tui.row.status"), string(plan.Status))
	screen = screen.Kicker(m.t("tui.kicker.goal"))
	body := strings.TrimSpace(plan.GoalBody)
	if body == "" {
		screen = screen.Span(m.styles.hint.Render(m.t("tui.empty.plan_no_goal")))
	} else {
		// One spanned row so the gridtable wraps the body inline rather
		// than drawing a horizontal border between every wrapped line —
		// same fix the description / comment overlays use.
		screen = screen.Span(m.renderBodyMarkdown(plan.GoalBody, valueWidth))
	}

	return "\n" + indentBlock(screen.View(m.taskViewportHeight(), m.styles.border, m.styles.hint), 2)
}

// openPlanGoalScreen fetches the cursored plan's full PlanShow projection
// (the list rollup omits goal_body) and flips planGoalScreenOpen. Mirrors
// openPlanNetwork's fetch-on-demand shape: failure leaves the list view in
// place and surfaces the error in the status line. No-op when the plan list
// is empty or the repo is unwired so a stray `f` from an unpopulated
// project does nothing. Resets the embedded detailscreen so the body always
// opens at the top.
func (m *Model) openPlanGoalScreen() {
	cursor := m.plansCursor.Cursor()
	if len(m.plans) == 0 || cursor < 0 || cursor >= len(m.plans) {
		return
	}
	if m.repos.Plans == nil {
		return
	}
	planSvc := app.NewPlanServiceWithSnapshot(m.repos.Plans, m.repos.activeSnapshot())
	slug := m.plans[cursor].Plan.Slug
	show, err := planSvc.Show(m.ctx, m.project, slug)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.planGoalShow = show
	m.planGoalScreen = detailscreen.New(0)
	m.planGoalScreenOpen = true
}

// closePlanGoalScreen returns the user to the plans list view and drops
// the cached PlanShow so the next open re-fetches fresh goal text.
func (m *Model) closePlanGoalScreen() {
	m.planGoalScreenOpen = false
	m.planGoalShow = app.PlanShow{}
	m.planGoalScreen = detailscreen.New(0)
}

// updatePlanGoalScreen runs the key handler while the goal overlay is on
// screen. Delegates scrolling to the embedded detailscreen; esc / `f`
// closes; `M` toggles raw / rendered markdown — mirrors
// updateDescriptionScreen so the two read-only overlays share one
// keybinding vocabulary. Value receiver (like updateCommentScreen) so the
// dispatcher returns a Model, not *Model — the pointer-receiver close /
// toggle helpers still mutate the addressable local copy in place.
func (m Model) updatePlanGoalScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "f":
		m.closePlanGoalScreen()
		return m, nil
	case "M":
		m.toggleMarkdownRendered()
		return m, nil
	}
	var cmd tea.Cmd
	m.planGoalScreen, cmd = m.planGoalScreen.Update(msg, m.taskViewportHeight())
	if m.planGoalScreen.LastEvent() == viewport.EventCancel {
		m.closePlanGoalScreen()
	}
	return m, cmd
}
