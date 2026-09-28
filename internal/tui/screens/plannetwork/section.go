package plannetwork

import (
	"fmt"
	"strings"

	networkprojection "omakiten/internal/plannetwork"
	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionOutline is the one section Plan Network declares to the arranger.
//
// One, not three: the goal and assign editors are full-panel mode overlays that
// replace the outline for a turn, the same way they always have. What the
// arranger owns is the browse outline — header chrome, scrollable rows, footer
// chrome — so the screen stops slicing its own viewport and holding a parallel
// cardlist scroll offset.
//
// Declaring ScrollItems is also how the outline acquires the standard scroll
// vocabulary on OwnsKey without restating it: a section that forgets a spelling
// cannot ship the way Flow did (#2416). Domain keys (assign, edit goal, wave
// toggle, collapse) stay on the screen and are named by it; every other
// spelling — motion included — is handed to [screengrid.State.HandleKey], which
// is the single entry to this body and routes it to the arranger that placed
// the outline's one Cell.
const sectionOutline = screenlayout.ID("plan-network-outline")
const (
	sectionGoal   = screenlayout.ID("plan-network-goal")
	sectionAssign = screenlayout.ID("plan-network-assign")
	sectionEmpty  = screenlayout.ID("plan-network-empty")
)

// outlineSpec is deliberately almost empty: Plan Network has no side-by-side
// breakpoint and no comfortable MaxWidth. It is one full-width scrolled body.
// SelectFirst keeps a row under the cursor so j/k walk items rather than nudge
// a bare offset — matching the linear cursor the outline has always had.
var outlineSpec = screenlayout.Spec{
	ID:          sectionOutline,
	MinRows:     1,
	Scroll:      screenlayout.ScrollItems,
	SelectFirst: true,
}

// outlineBox is the host body minus the outer chrome Plan Network paints
// around the bordered table (plan header, dependencies footer, next-claimable).
// The arranger only lays out the table, and charging those outer rows here is
// what leaves it the window the outline actually has: this is the one place the
// screen states its own chrome, and the arranger measures the table's borders
// and header for itself off the Block the section returns.
func (m Screen) outlineBox() screenlayout.Box {
	host := screenlayout.HostBox(m.kit)
	return screenlayout.Box{Width: host.Width, Rows: max(0, host.Rows-m.planNetworkOuterChromeRows())}
}

func (m Screen) gridBox() screenlayout.Box {
	if m.mode == ModeBrowse && len(m.planNetworkShow.Waves) > 0 {
		return m.outlineBox()
	}
	box := screenlayout.HostBox(m.kit)
	box.Rows = m.kit.Height
	box.Width = m.kit.Width
	if box.Width <= 0 {
		box.Width = m.kit.BoxWidth() + 2
	}
	return box
}

func (m Screen) outlineSection() screenlayout.Func {
	return screenlayout.Func{Def: outlineSpec, Body: m.outlineBlock}
}

// outlineRoot is the complete browse body: one painted box and therefore one
// Cell. Goal and assign editors remain full-panel overlays outside this tree.
func (m Screen) outlineRoot() screengrid.Node {
	section := m.outlineSection()
	return screengrid.Cell(section.Def, section.Body)
}

func (m Screen) goalSection() screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionGoal, MinRows: 1},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return screenlayout.Block{Header: framed.Document(m.renderPlanGoalEditor(canvas.Width())), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (m Screen) assignSection() screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionAssign, MinRows: 1},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return screenlayout.Block{Header: framed.Document(m.renderPlanAssignEditor(canvas.Width())), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (m Screen) emptySection() screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{ID: sectionEmpty, MinRows: 1},
		Body: func(screenlayout.Canvas) screenlayout.Block {
			slug := screenkit.Sanitize(m.planNetworkShow.Plan.Slug)
			header := fmt.Sprintf(m.t("tui.plans.network.header_fmt"), slug, 0, 0, 0)
			body := fmt.Sprintf(m.t("tui.plans.network.no_waves_fmt"), slug)
			view := m.renderPanel(header + "\n\n" + body)
			return screenlayout.Block{Header: framed.Document(view), Cursor: screenlayout.NoSelection()}
		},
	}
}

func (m Screen) root(frame screenhost.Frame) screengrid.Node {
	switch {
	case m.mode == ModeGoal:
		section := m.goalSection()
		return screengrid.Cell(section.Def, section.Body)
	case m.mode == ModeAssign:
		section := m.assignSection()
		return screengrid.Cell(section.Def, section.Body)
	case len(m.planNetworkShow.Waves) == 0:
		section := m.emptySection()
		return screengrid.Cell(section.Def, section.Body)
	default:
		return m.outlineRoot()
	}
}

func (m Screen) gridResult(frame screenhost.Frame) screengrid.Result {
	m = m.bindFrame(frame)
	return screengrid.Render(m.kit, m.grid, m.outlineBox(), m.outlineRoot())
}

// outlineBlock is the bordered table as items the arranger windows. The plan
// header line and the dependencies / next-claimable footers stay OUTSIDE the
// section — they historically overflow the terminal (see the golden NOTE) and
// wrapping them inside the arranger would quietly change every recorded body.
// Table chrome and rows go through Arrange so scroll occupancy is measured.
func (m Screen) outlineBlock(canvas screenlayout.Canvas) screenlayout.Block {
	selected := canvas.Cursor()
	build := m.planNetworkFullBuild()
	rows := build.Rows
	filaments, laneCount := build.Filaments, build.LaneCount

	cursorPad := "  "
	emptyLane := ""
	if laneCount > 0 {
		emptyLane = m.styles.Hint.Render(strings.Repeat(" ", laneCount+1))
	}

	budget := canvas.Width() - 10
	if laneCount > 0 {
		budget -= laneCount + 1
	}
	if budget < 32 {
		budget = 32
	}
	measured := m.planNetworkMeasureTitle(rows, 10, 14)
	layout := planNetworkBuildTable(budget, measured)

	topChrome, bottomChrome := m.planNetworkTableChrome(rows, layout, cursorPad, emptyLane)

	items := make([]string, len(rows))
	for i, row := range rows {
		primaryLane := lane(m.styles, i, filaments, laneCount)
		suppress := networkprojection.FilamentSourceIDsAtRow(filaments, rows, i)
		line := m.renderPlanNetworkRowBody(row, i == selected, primaryLane, suppress, layout)
		if i+1 < len(rows) && rows[i+1].Kind != row.Kind {
			contLane := m.renderPlanNetworkLaneContinuation(i, filaments, laneCount)
			sep := m.renderPlanNetworkSeparator(row.Kind, rows[i+1].Kind, layout, cursorPad, contLane)
			line = line + "\n" + sep
		}
		items[i] = line
	}

	return screenlayout.Block{
		Header:  topChrome,
		Items:   items,
		Heights: planNetworkRowHeights(rows),
		Footer:  []string{bottomChrome},
	}
}
