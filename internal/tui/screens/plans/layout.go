package plans

import (
	"fmt"
	"strings"

	"omakiten/internal/tui/components/framed"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/markdown"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/screenhost"
)

// sectionBody is the one section the Plans list declares. MinRows is a hard
// floor (K1): with a single Weight:1 section there is no sibling to drop when
// the HostBox is short — the section simply receives whatever rows the panel
// viewport budgets.
const sectionBody = screenlayout.ID("plans-body")

// sectionGoal is the one section the plan-goal reader declares. Its ITEMS are
// the composed goal document's lines, so the arranger owns the one window over
// them: there is no second window inside a pre-fitted item to keep in step.
//
// It states no SelectFirst and its block states no Cursor, which is the whole
// declaration a body-scroll surface makes — both resolve to the no-selection
// sentinel, so screenlayout.apply moves this section's OFFSET rather than a
// selection. sectionBody above keeps SelectFirst because the plan LIST really
// does select a row.
const sectionGoal = screenlayout.ID("plans-goal")

func (s Screen) bodySection(kit screenkit.Kit) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionBody, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems, SelectFirst: true,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.bodyBlock(kit, canvas.Width(), canvas.Cursor())
		},
	}
}

func (s Screen) root(kit screenkit.Kit) screengrid.Node {
	section := s.bodySection(kit)
	return screengrid.Cell(section.Def, section.Body)
}

// bodyBlock pins the kicker / column header / rule as Header so the crown does
// not scroll away, and exposes each plan row as one CapRows-clamped item.
//
// The selection marker is painted from the cursor the ARRANGER holds — read off
// canvas.Cursor() by the section body and passed in here — so the grid's key
// routing moves the highlight without a Block.At override folding it back onto
// a second copy the screen would have to keep in step.
func (s Screen) bodyBlock(kit screenkit.Kit, width, cursor int) screenlayout.Block {
	width = max(1, width)
	nameWidth := width - 58
	if nameWidth < 12 {
		nameWidth = 12
	}
	missing := kit.T("tui.plans.list.active_wave_missing")
	if s.rows == nil || !s.rows.ready || s.rows.width != width {
		base := kit.CursorMarker(false)
		rows := make([]string, len(s.plans))
		for i, rollup := range s.plans {
			active := rollup.ActiveWaveName
			if active == "" {
				active = missing
			}
			rows[i] = base + " " + fmt.Sprintf("%-16s %-7s %5d/%-3d %4d%%  %s",
				truncate(rollup.Plan.Slug, 16), truncate(string(rollup.Plan.Status), 7),
				rollup.DoneCount, rollup.TotalCount, percent(rollup.DoneCount, rollup.TotalCount), truncate(rollup.Plan.Name+"  ‹"+active+"›", nameWidth))
		}
		if s.rows != nil {
			s.rows.width, s.rows.rows, s.rows.ready = width, rows, true
		} else {
			return s.bodyBlockWithRows(kit, width, rows)
		}
	}
	rows := append([]string(nil), s.rows.rows...)
	if len(rows) > 0 {
		i := cursor
		if i < 0 {
			i = 0
		} else if i >= len(rows) {
			i = len(rows) - 1
		}
		prefix := kit.CursorMarker(false)
		rows[i] = kit.CursorMarker(true) + strings.TrimPrefix(rows[i], prefix)
	}
	return s.bodyBlockWithRows(kit, width, rows)
}

// bodyBlockWithRows states no Cursor at all: the zero [screenlayout.Cursor] is
// cursorInherit, so the arranger keeps the selection it already holds. An empty
// list needs no NoSelection() either — clampCursor answers -1 for a body with no
// items, which is the same statement made by the one authority that has counted
// them.
func (s Screen) bodyBlockWithRows(kit screenkit.Kit, width int, rows []string) screenlayout.Block {
	kicker := kit.Styles.FocusKickerCount(kit.T("tui.plans.kicker"), len(s.plans))
	// Data rows sit after CursorMarker + space. The catalog is the column
	// labels only; this indent keeps SLUG over the first cell instead of flush
	// against the box.
	label := kit.CursorMarker(false) + " " + kit.T("tui.plans.list.header")
	columnHeader := []string{kit.Styles.Info.Render(label)}
	return framed.List(kit.Styles.Border, width, kicker, columnHeader, rows)
}

// panelBox is the geometry ArrangeIn / HandleKeyIn / ResyncIn share: panel
// content width and the PanelChrome viewport (no leading blank — Panel owns
// that row outside the arranged body). Header rows are charged by the
// arranger from Block.Header, not subtracted here.
//
// When the terminal is still unmeasured (Height<=0), Kit.ViewportRows returns
// the historical "0 means unlimited" sentinel. ArrangeIn treats 0 as ZERO rows,
// so HostBox supplies the unmeasured-height assumption instead.
func (s Screen) panelBox(kit screenkit.Kit) screenlayout.Box {
	rows := kit.Chrome().ViewportRows()
	if kit.Height <= 0 {
		rows = screenlayout.HostBox(kit).Rows
	}
	return screenlayout.Box{
		Width: kit.PanelContentWidth(),
		Rows:  rows,
	}
}

// syncBodyWindow focuses the one zone and performs a boxful Cell resync, which
// re-clamps the arranger's cursor and offset against the rollups that now
// exist. Call it on enter/resize and before opening a child route.
func (s Screen) syncBodyWindow(kit screenkit.Kit) Screen {
	s.grid = s.grid.WithFocus(sectionBody).Resync(kit, s.panelBox(kit), s.root(kit))
	return s
}

// gridView paints the list through screengrid, the single entry to the body.
//
// A one-Cell root paints exactly what arranging its section painted: the grid's
// arrangeLeaf rebuilds a screenlayout.Func from the Cell's Spec and body and
// calls the same screenlayout.ArrangeIn with the same kit, the same box and the
// same layout state. The only delta it applies is clearing
// Spec.Column/Spec.Group, and sectionBody declares neither.
func (s Screen) gridView(kit screenkit.Kit) string {
	return screengrid.Render(kit, s.grid, s.panelBox(kit), s.root(kit)).View
}

func (s GoalScreen) bodySection(frame screenhost.Frame) screenlayout.Func {
	return screenlayout.Func{
		Def: screenlayout.Spec{
			ID: sectionGoal, MinRows: 1, Weight: 1,
			Scroll: screenlayout.ScrollItems,
		},
		Body: func(canvas screenlayout.Canvas) screenlayout.Block {
			return s.readBlock(frame, canvas)
		},
	}
}

func (s GoalScreen) root(frame screenhost.Frame) screengrid.Node {
	section := s.bodySection(frame)
	return screengrid.Cell(section.Def, section.Body)
}

func (s GoalScreen) readBlock(frame screenhost.Frame, canvas screenlayout.Canvas) screenlayout.Block {
	kit := frame.Kit()
	plan := s.show.Plan
	valueWidth := canvas.Width() - gridtable.LabelWidth - 3
	if valueWidth < 24 {
		valueWidth = 24
	}
	detail := gridtable.NewDetail(valueWidth, kit.Styles.Info).
		Custom(gridtable.Styled(kit.Styles.Kicker(fmt.Sprintf(kit.T("tui.kicker.plan_goal_fmt"), screenkit.Sanitize(plan.Slug))))).
		Row(kit.T("tui.row.name"), plan.Name).
		Row(kit.T("tui.row.status"), string(plan.Status)).
		Kicker(kit.T("tui.kicker.goal"))
	if strings.TrimSpace(plan.GoalBody) == "" {
		detail = detail.Span(gridtable.Styled(kit.Styles.Hint.Render(kit.T("tui.empty.plan_no_goal"))))
	} else {
		s.md.Reload(kit.Markdown)
		detail = detail.Span(gridtable.Styled(markdown.Body(s.md, plan.GoalBody, valueWidth, s.markdownRendered)))
	}
	// One window, and it is the arranger's: the composed goal goes in as the
	// lines it is made of.
	return screenlayout.Block{Items: framed.Document(detail.View(kit.Styles.Border))}
}

func (s GoalScreen) syncBodyWindow(frame screenhost.Frame) GoalScreen {
	kit := frame.Kit()
	box := screenlayout.HostBox(kit)
	s.grid = s.grid.WithFocus(sectionGoal).Resync(kit, box, s.root(frame))
	return s
}

// gridView paints the goal reader through screengrid, on the same one-Cell
// equivalence the list above relies on. sectionGoal likewise declares neither
// Spec.Column nor Spec.Group.
func (s GoalScreen) gridView(frame screenhost.Frame) string {
	kit := frame.Kit()
	return screengrid.Render(kit, s.grid, screenlayout.HostBox(kit), s.root(frame)).View
}
