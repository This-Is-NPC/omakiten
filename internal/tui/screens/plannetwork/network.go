package plannetwork

import (
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/domain"
	networkprojection "omakiten/internal/plannetwork"
	"omakiten/internal/tui/components/field"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/screenhost"
)

type Mode uint8

const (
	ModeBrowse Mode = iota
	ModeGoal
	ModeAssign
)

type Payload struct {
	Show            domain.PlanShow
	NextClaimableID int64
	FirstBucket     string
	FinalBucket     string
	Tasks           map[int64]domain.Task
}

type Screen struct {
	mode                  Mode
	planNetworkShow       domain.PlanShow
	grid                  screengrid.State
	planNetworkCollapsed  map[int64]bool
	planNetworkRowsCache  planNetworkRowsCacheEntry
	planNetworkBuildCache *planNetworkBuildCacheEntry
	nextClaimableID       int64
	firstBucket           string
	finalBucket           string
	tasks                 map[int64]domain.Task
	goalInput             textarea.Model
	assignInput           textarea.Model
	assignTaskID          int64
	styles                screenkit.Styles
	text                  func(string) string
	kit                   screenkit.Kit
}

type Model = Screen

type networkTone uint8

const (
	toneNone networkTone = iota
	toneMuted
	toneHintAccent
	toneSuccess
	toneInfo
	toneBadgeInfo
	toneBadgeBlocker
)

// The renderer keeps short local names, while the semantic model belongs to
// the non-UI projection package.
type planNetworkRow = networkprojection.Row
type planNetworkRowKind = networkprojection.RowKind

const (
	planRowWaveHeader planNetworkRowKind = networkprojection.WaveHeader
	planRowTaskCard   planNetworkRowKind = networkprojection.TaskCard
	planRowNone       planNetworkRowKind = 2
)

type fingerprint struct{ hash hash.Hash64 }

func newFingerprint() *fingerprint { return &fingerprint{hash: fnv.New64a()} }
func (f *fingerprint) writeInt64(value int64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(value))
	_, _ = f.hash.Write(buf[:])
}
func (f *fingerprint) writeString(value string) {
	_, _ = f.hash.Write([]byte(value))
	_, _ = f.hash.Write([]byte{0})
}
func (f *fingerprint) writeBool(value bool) {
	if value {
		_, _ = f.hash.Write([]byte{1})
	} else {
		_, _ = f.hash.Write([]byte{0})
	}
}
func (f *fingerprint) sum() uint64 { return f.hash.Sum64() }

func New() Screen {
	return Screen{
		grid:                  screengrid.NewState(),
		planNetworkCollapsed:  map[int64]bool{},
		planNetworkBuildCache: &planNetworkBuildCacheEntry{},
		goalInput:             textarea.New(),
		assignInput:           textarea.New(),
	}
}

func (m Screen) ID() screenhost.ID { return screenhost.PlanNetwork }

func (m Screen) Apply(payload Payload) Screen {
	m.planNetworkShow = payload.Show
	m.nextClaimableID = payload.NextClaimableID
	m.firstBucket, m.finalBucket = payload.FirstBucket, payload.FinalBucket
	m.tasks = payload.Tasks
	m.invalidatePlanNetworkRowsCache()
	m.syncPlanNetworkScroll(m.planNetworkBuildRows())
	return m
}

func (m Screen) Open(payload Payload) Screen {
	m = New().Apply(payload)
	rows := m.planNetworkBuildRows()
	for i, row := range rows {
		if row.Kind == planRowWaveHeader && row.WaveID == payload.Show.ActiveWaveID {
			m.grid = m.grid.WithCursor(sectionOutline, i)
			break
		}
	}
	m.syncPlanNetworkScroll(rows)
	return m
}

func (m Screen) Show() domain.PlanShow { return m.planNetworkShow }
func (m Screen) Mode() Mode            { return m.mode }
func (m Screen) Cursor() int           { return m.outlineCursor() }
func (m Screen) AssignTaskID() int64   { return m.assignTaskID }
func (m Screen) EditorValue() string {
	if m.mode == ModeGoal {
		return m.goalInput.Value()
	}
	return m.assignInput.Value()
}
func (m Screen) WithEditorValue(value string) Screen {
	if m.mode == ModeGoal {
		m.goalInput.SetValue(value)
	} else {
		m.assignInput.SetValue(value)
	}
	return m
}
func (m Screen) RowCount() int {
	return len(networkprojection.BuildRows(m.planNetworkInput()).Rows)
}
func (m Screen) SelectCursor(index int) Screen {
	m.grid = m.grid.WithCursor(sectionOutline, index)
	m.syncPlanNetworkScroll(m.planNetworkBuildRows())
	return m
}

func (m Screen) bindFrame(frame screenhost.Frame) Screen {
	m.text = frame.Text
	m.kit = frame.Kit()
	s := m.kit.Styles
	m.styles = s
	return m
}

func (m Screen) Update(frame screenhost.Frame, msg tea.Msg) screenhost.Outcome {
	m = m.bindFrame(frame)
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return screenhost.Stay(m, nil)
	}
	if next, handled := m.grid.HandleKey(m.kit, m.gridBox(), key.String(), m.root(frame)); handled {
		m.grid = next
		return screenhost.Stay(m, nil)
	}
	if m.mode == ModeGoal {
		switch key.String() {
		case "esc":
			m.mode, m.goalInput = ModeBrowse, textarea.New()
			m.grid = m.grid.WithFocus(sectionOutline)
			return screenhost.SetStatus(m, m.t("tui.status.cancelled"), nil)
		case "ctrl+s":
			value := m.goalInput.Value()
			m.mode = ModeBrowse
			m.grid = m.grid.WithFocus(sectionOutline)
			return screenhost.SavePlanGoal(m, m.planNetworkShow.Plan.ID, m.planNetworkShow.Plan.Slug, value, nil)
		}
		var cmd tea.Cmd
		m.goalInput, cmd = m.goalInput.Update(key)
		return screenhost.Stay(m, cmd)
	}
	if m.mode == ModeAssign {
		switch key.String() {
		case "esc":
			m.mode, m.assignTaskID, m.assignInput = ModeBrowse, 0, textarea.New()
			m.grid = m.grid.WithFocus(sectionOutline)
			return screenhost.SetStatus(m, m.t("tui.status.cancelled"), nil)
		case "enter":
			value, taskID := strings.TrimSpace(m.assignInput.Value()), m.assignTaskID
			if value == "" {
				return screenhost.SetStatus(m, m.t("tui.status.input_required"), nil)
			}
			m.mode, m.assignTaskID = ModeBrowse, 0
			return screenhost.SetTaskAssignee(m, taskID, m.planNetworkShow.Plan.Slug, value, nil)
		}
		var cmd tea.Cmd
		m.assignInput, cmd = m.assignInput.Update(key)
		return screenhost.Stay(m, cmd)
	}
	return m.handlePlanNetworkKey(key)
}

func (m Screen) Lifecycle(frame screenhost.Frame, event screenhost.LifecycleEvent) screenhost.Outcome {
	m = m.bindFrame(frame)
	if event == screenhost.LifecycleEnter || event == screenhost.LifecycleResize {
		m = m.bindFrame(frame)
		m.grid = m.grid.Resync(m.kit, m.gridBox(), m.root(frame))
		if m.mode == ModeBrowse && len(m.planNetworkShow.Waves) > 0 {
			m.syncPlanNetworkScroll(m.planNetworkBuildRows())
		}
	}
	return screenhost.Stay(m, nil)
}

func (m Screen) View(frame screenhost.Frame) string {
	m = m.bindFrame(frame)
	if m.mode != ModeBrowse || len(m.planNetworkShow.Waves) == 0 {
		return screengrid.Render(m.kit, m.grid, m.gridBox(), m.root(frame)).View
	}
	// The grid owns the bordered table window through the screen's one Cell.
	// The plan header and the dependencies / next-claimable lines stay outside
	// it — they are the same outer chrome planNetworkOuterChromeRows charges to
	// outlineBox, including the overflow the goldens record as current behaviour.
	table := m.gridResult(frame).View
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(screenkit.Indent(m.planNetworkHeaderLine()+"\n\n"+table, 2))
	if footer := m.planNetworkDepsFooterBlock(m.planNetworkShow.Dependencies); footer != "" {
		sb.WriteString("\n\n")
		sb.WriteString(screenkit.Indent(footer, 2))
	}
	if m.nextClaimableID != 0 {
		sb.WriteString("\n  ")
		sb.WriteString(m.planNetworkNextClaimableLine())
	}
	return sb.String()
}
func (m Screen) OwnsKey(tea.KeyMsg) bool { return true }
func (m Screen) BlocksHostInput() bool   { return m.mode != ModeBrowse }
func (m Screen) OwnsFooter() bool        { return true }

func (m Screen) Footer(frame screenhost.Frame) []screenhost.FooterBinding {
	if m.mode == ModeGoal {
		return []screenhost.FooterBinding{frame.FooterSave(true), frame.FooterCancel(false)}
	}
	if m.mode == ModeAssign {
		return []screenhost.FooterBinding{{Key: "enter", Label: frame.Text("tui.footer.save"), Primary: true}, frame.FooterCancel(false)}
	}
	enter := frame.Text("tui.footer.toggle_wave")
	if m.planNetworkCursorOnTaskRow() {
		enter = frame.Text("tui.footer.open")
	}
	return []screenhost.FooterBinding{{Key: "c", Label: frame.Text("tui.footer.assignee"), Primary: true}, {Key: "e", Label: frame.Text("tui.footer.edit_goal"), Primary: true}, {Key: "enter", Label: enter, Primary: true}, frame.FooterMoveVim(false), {Key: "space", Label: frame.Text("tui.footer.toggle_wave")}, {Key: "h/l", Label: frame.Text("tui.footer.collapse_expand")}, frame.FooterTopBottom(false), frame.FooterPage(false), frame.FooterRefresh(false), frame.FooterBack(false)}
}

func (m Screen) Help(frame screenhost.Frame) []screenhost.HelpGroup {
	return []screenhost.HelpGroup{{ID: "plan_network", Title: frame.Text("tui.plans.kicker"), Bindings: []screenhost.HelpBinding{{Key: "enter", Description: frame.Text("tui.footer.open")}, {Key: "c", Description: frame.Text("tui.footer.assignee")}, {Key: "e", Description: frame.Text("tui.footer.edit_goal")}, {Key: "space", Description: frame.Text("tui.footer.toggle_wave")}, {Key: "r", Description: frame.Text("tui.footer.refresh")}}}}
}

func (m Screen) t(key string) string {
	if m.text == nil {
		return defaultText(key)
	}
	return m.text(key)
}

func defaultText(key string) string {
	defaults := map[string]string{
		"tui.plans.network.deps_footer_prefix": "Dependencies: ",
		"tui.plans.network.cross_dep_fmt":      "←W#%d",
		"tui.plans.network.badge.done":         "done",
		"tui.plans.network.badge.gated":        "gated",
		"tui.plans.network.badge.in_progress":  "in-progress",
		"tui.plans.network.badge.blocked":      "blocked",
		"tui.plans.network.badge.assigned":     "assigned",
		"tui.plans.network.badge.next":         "▶next",
		"tui.plans.network.badge.ready":        "ready",
		"tui.plans.status.assign_no_task":      "no task selected",
	}
	if value := defaults[key]; value != "" {
		return value
	}
	return key
}

func (m Screen) renderPanel(content string) string {
	return m.kit.Panel(content)
}
func (m Screen) cursorChevron(selected bool) string {
	return panel.Chevron(m.styles.HintAccent, selected)
}

// ===========================================================================
// Plan network — collapsible rails + cross-wave filaments
//
// The view renders the plan as one vertical outline. Each wave is a header
// row (▼ expanded, ▶ collapsed). Expanded waves render their tasks as
// one-line cards, with intra-wave dep edges drawn as a left-side `git log`
// style rail (├─ └─ │). Cross-wave deps surface as muted vertical filaments
// in a fixed left margin lane, so the user can trace where blockers come
// from without leaving the outline.
//
// Navigation: a single linear cursor walks every visible row (headers + task
// cards); j/k step one row at a time, space toggles the wave under the
// cursor. The flat row list makes scroll trivial (one line per row), which
// is why the multi-column-per-wave view + per-bucket vertical scroll map +
// gutter/backplane routing the previous design carried were all dropped.
// ===========================================================================

// handlePlanNetworkKey drives the linear-cursor outline. The single
// cursor walks both wave headers and task cards so toggling a wave
// stays inside the same key model as opening a task.
//
// # What the screen names, and what the grid gets
//
// The screen names its DOMAIN keys and hands every other spelling to the grid.
// Motion is not named here and is therefore the grid's: `j`/`k`, the arrows,
// pgup/pgdn/ctrl+u/ctrl+d and home/g / end/G reach
// [screengrid.State.HandleKey], which routes them to the arranger that placed
// the outline's one Cell — the same arranger that already windows the rows this
// screen renders. It moves the cursor and settles the offset in one pass,
// against the heights it just measured.
//
// Because the switch runs first, nothing the screen names can reach the grid:
// `esc` (back), `h`/`left` and `l`/`right` (collapse / expand), space, `o` and
// enter, `c`, `e` and `r` all keep their meaning here, and the grid never sees
// them. On the way in the two editor modes have already returned from
// [Screen.Update], so no keystroke reaches the grid while one is open.
func (m Screen) handlePlanNetworkKey(msg tea.KeyMsg) screenhost.Outcome {
	rows := m.planNetworkBuildRows()
	switch msg.String() {
	case "esc":
		return screenhost.Back(m, nil)
	case " ":
		m.togglePlanWaveAtCursor()
	case "left", "h":
		m.collapsePlanNetworkWave(rows)
	case "right", "l":
		m.expandPlanNetworkWave(rows)
	case "o", "enter":
		return m.openPlanNetworkRow(rows)
	case "r":
		return screenhost.Reload(m, nil)
	case "c":
		if !m.openPlanAssignEditor() {
			return screenhost.SetStatus(m, m.t("tui.plans.status.assign_no_task"), nil)
		}
	case "e":
		m.openPlanGoalEditor()
	default:
		if next, handled := m.grid.HandleKey(m.kit, m.outlineBox(), msg.String(), m.outlineRoot()); handled {
			m.grid = next
		}
	}
	return screenhost.Stay(m, nil)
}

func (m *Screen) collapsePlanNetworkWave(rows []planNetworkRow) {
	idx := m.outlineCursor()
	if len(rows) == 0 || idx < 0 || idx >= len(rows) {
		return
	}
	waveID := rows[idx].WaveID
	if m.planNetworkCollapsed == nil {
		m.planNetworkCollapsed = map[int64]bool{}
	}
	m.planNetworkCollapsed[waveID] = true
	rows = m.planNetworkBuildRows()
	m.syncPlanNetworkScroll(rows)
	m.snapCursorToWaveHeader(rows, waveID)
}

func (m *Screen) expandPlanNetworkWave(rows []planNetworkRow) {
	idx := m.outlineCursor()
	if len(rows) == 0 || idx < 0 || idx >= len(rows) || rows[idx].Kind != planRowWaveHeader {
		return
	}
	if m.planNetworkCollapsed == nil {
		m.planNetworkCollapsed = map[int64]bool{}
	}
	m.planNetworkCollapsed[rows[idx].WaveID] = false
	m.syncPlanNetworkScroll(m.planNetworkBuildRows())
}

func (m Screen) openPlanNetworkRow(rows []planNetworkRow) screenhost.Outcome {
	idx := m.outlineCursor()
	if len(rows) == 0 || idx < 0 || idx >= len(rows) {
		return screenhost.Stay(m, nil)
	}
	row := rows[idx]
	if row.Kind != planRowTaskCard {
		// Enter on a wave header toggles the row's advertised state.
		m.togglePlanWaveAtCursor()
		return screenhost.Stay(m, nil)
	}
	return screenhost.OpenTask(m, row.Task.TaskID, nil)
}

// togglePlanWaveAtCursor flips the collapse flag for the wave the
// cursor currently sits on (its own wave for tasks; the header for
// header rows). Cursor stays in place; WithItemCount(new-row-count)
// clamps the cursor to the new last row if the toggle shrank the
// list past it.
func (m *Model) togglePlanWaveAtCursor() {
	rows := m.planNetworkBuildRows()
	idx := m.outlineCursor()
	if len(rows) == 0 || idx < 0 || idx >= len(rows) {
		return
	}
	waveID := rows[idx].WaveID
	if m.planNetworkCollapsed == nil {
		m.planNetworkCollapsed = map[int64]bool{}
	}
	m.planNetworkCollapsed[waveID] = !m.planNetworkCollapsed[waveID]
	rows = m.planNetworkBuildRows()
	m.syncPlanNetworkScroll(rows)
	// Re-snap cursor onto the wave's header when the wave just
	// collapsed and the cursor used to live on a task row inside it —
	// the cursor would otherwise land on the NEXT wave's header (or
	// a later task), which silently teleports the cursor.
	if m.planNetworkCollapsed[waveID] {
		m.snapCursorToWaveHeader(rows, waveID)
	}
}

// snapCursorToWaveHeader walks BACK from the clamped cursor to the header of the
// wave named, and seeds the cursor there.
//
// It is a DOMAIN jump, not a window move, which is why it is spelled with
// [screengrid.State.WithCursor]: that write keeps the frame the last composition
// recorded, where WithLayout would throw it away and make the next keystroke
// re-render the body to rediscover an arrangement nothing had changed.
func (m *Model) snapCursorToWaveHeader(rows []planNetworkRow, waveID int64) {
	for i := clampOutlineCursor(m.outlineCursor(), len(rows)); i >= 0 && i < len(rows); i-- {
		if rows[i].Kind == planRowWaveHeader && rows[i].WaveID == waveID {
			m.grid = m.grid.WithCursor(sectionOutline, i)
			return
		}
	}
}

// openPlanGoalEditor flips the model into modePlanGoal, pre-filling
// the bubbles textarea with the focused plan's current goal_body. The
// editor is sqlite-backed end-to-end (no tempfile / no $EDITOR shell-
// out) — submit hits PlanService.UpdateGoalBody and reloadPlanNetwork
// refreshes the projection so the next render reflects the new body.
func (m *Model) openPlanGoalEditor() {
	if m.planNetworkShow.Plan.ID == 0 {
		return
	}
	m.mode = ModeGoal
	m.grid = m.grid.WithFocus(sectionGoal)
	m.goalInput = textarea.New()
	m.goalInput.SetValue(screenkit.SanitizeMultiline(m.planNetworkShow.Plan.GoalBody))
	m.goalInput.Focus()
}

// openPlanAssignEditor drives the `c` binding: opens the single-line
// assignee input pre-targeted at the task under the cursor. The
// previous implementation called PlanService.ClaimNext, which moved
// the task into the next bucket as part of the claim — that bypassed
// the preset's bucket guards (omakase requires a self-branch comment
// before backlog → dev). The new flow only writes assigned_to;
// bucket transitions stay manual via the board's move binding.
func (m *Model) openPlanAssignEditor() bool {
	if m.planNetworkShow.Plan.ID == 0 {
		return false
	}
	rows := m.planNetworkBuildRows()
	idx := m.outlineCursor()
	if len(rows) == 0 || idx < 0 || idx >= len(rows) {
		return false
	}
	row := rows[idx]
	if row.Kind != planRowTaskCard || row.Task.TaskID == 0 {
		return false
	}
	m.mode, m.assignTaskID = ModeAssign, row.Task.TaskID
	m.grid = m.grid.WithFocus(sectionAssign)
	m.assignInput = textarea.New()
	m.assignInput.SetValue(screenkit.Sanitize(row.Task.AssignedTo))
	m.assignInput.Focus()
	return true
}

// outlineCursor is the outline's selected row index, read from the one place
// that holds it: the arranger state under the grid, keyed by the screen's single
// section. There is no second copy on the screen to disagree with it.
func (m Model) outlineCursor() int { return m.grid.Layout().Cursor(sectionOutline) }

// clampOutlineCursor is where a row index lands once the outline has the rows it
// actually has: the arranger's no-selection sentinel on an empty outline, the
// last row for an index the projection outgrew, and the first row for a cursor
// nothing has seeded yet — which is what outlineSpec's SelectFirst asks for.
//
// The arranger clamps too, on the next key and on every render, so this is not
// what keeps the WINDOW honest. It is what keeps the screen's own READS honest:
// the footer, `c`, enter and the collapse re-snap all index rows with the stored
// cursor before any of that runs.
func clampOutlineCursor(cursor, rows int) int {
	if rows <= 0 {
		return -1
	}
	if cursor < 0 {
		return 0
	}
	return min(cursor, rows-1)
}

// syncPlanNetworkScroll parks the outline cursor on the grid, clamped to the
// rows that exist. That is the whole of it.
//
// It used to end in a resync on a bare arranger state — the residual of the
// days when this screen drove the arranger itself, and the last thing in this
// package reaching past the grid to touch it. Nothing needs it any more: [screengrid.State.HandleKey] settles the focused leaf's offset out of
// the frame the keystroke measured, and Arrange clamps for the frame it paints
// without persisting, so the window follows the cursor whether or not anything
// was written here. What the residual did cost is the bookkeeping it needed to
// run — an item count and a viewport, and the viewport is a chrome measurement
// this screen renders strings to take. Heights still vary per row because
// wave↔task transitions inject a separator line; the arranger measures those
// from the Block returned by the grid's single Cell, as it always did.
func (m *Model) syncPlanNetworkScroll(rows []planNetworkRow) {
	m.grid = m.grid.WithCursor(sectionOutline, clampOutlineCursor(m.outlineCursor(), len(rows)))
}

// planNetworkOuterChromeRows is every row the plan-network body draws OUTSIDE
// the bordered table: the plan header and the blank under it, the dependency
// footer with its own blank spacer, and the next-claimable hint.
//
// It counts them without formatting the single-line header and claim hint
// merely to count them; the dependency footer still goes through its real
// wrapping path because its height is width-bound. The hand-counted `5` this
// replaced assumed all four optional rows were always present; a plan with no
// dependencies and no claimable task draws two of them, and the outline was
// short by three rows on every such plan.
func (m Model) planNetworkOuterChromeRows() int {
	rows := 2 // plan header + blank spacer
	if footer := m.planNetworkDepsFooterBlock(m.planNetworkShow.Dependencies); footer != "" {
		rows += 2 + strings.Count(footer, "\n") // blank spacer + footer lines
	}
	if m.nextClaimableID != 0 {
		rows++
	}
	return rows
}

// planNetworkHeaderLine is the plan progress + keymap line above the table.
func (m Model) planNetworkHeaderLine() string {
	show := m.planNetworkShow
	pct := networkprojection.CompletionPercent(show.DoneCount, show.TotalCount)
	return m.styles.HintAccent.Render(fmt.Sprintf(m.t("tui.plans.network.header_fmt"), screenkit.Sanitize(show.Plan.Slug), show.DoneCount, show.TotalCount, pct) + "   " + m.t("tui.plans.network.keymap"))
}

// planNetworkNextClaimableLine is the "next claimable: #N" hint under the table.
func (m Model) planNetworkNextClaimableLine() string {
	return m.styles.HintAccent.Render(fmt.Sprintf(m.t("tui.plans.network.next_claimable_fmt"), m.nextClaimableID))
}

// planNetworkTableChrome renders the bordered table's static chrome for a row
// set: the top border, the column header, the separator under the header (only
// when there are rows to separate from) and the bottom border.
//
// Returned as strings so renderPlanNetwork joins exactly what the row budget
// measured. The budget measures them at a probe layout, which is sound because
// every one of these elements is a single line at any column width — the same
// probe technique screenkit.Chrome.Box uses on a style.
func (m Model) planNetworkTableChrome(rows []planNetworkRow, layout planNetworkTableLayout, cursorPad, lane string) (top []string, bottom string) {
	top = []string{
		m.renderPlanNetworkSeparator(planRowNone, planRowTaskCard, layout, cursorPad, lane),
		m.renderPlanNetworkHeaderRow(layout, cursorPad, lane),
	}
	if len(rows) == 0 {
		return top, m.renderPlanNetworkSeparator(planRowNone, planRowNone, layout, cursorPad, lane)
	}
	top = append(top, m.renderPlanNetworkSeparator(planRowTaskCard, rows[0].Kind, layout, cursorPad, lane))
	return top, m.renderPlanNetworkSeparator(rows[len(rows)-1].Kind, planRowNone, layout, cursorPad, lane)
}

// ===========================================================================
// Row projection — flatten waves + tasks into a single linear list.
// ===========================================================================

// planNetworkBuildCacheEntry memoises the FULL semantic projection
// (rows + cross-blocker index + next-claimable id + critical-path DFS).
// valid stays false until the first build so a fresh model never serves
// a stale zero build.
type planNetworkBuildCacheEntry struct {
	valid bool
	key   uint64
	build networkprojection.Projection
}

// planNetworkFullBuild returns the full plan-network projection the
// renderer consumes — memoised so the critical-path DFS + the
// next-claimable peek run once per state change instead of once per
// View(). Value receiver: the cache lives behind a pointer
// (m.planNetworkBuildCache) so the freshly built projection persists back
// through the shared entry even though View() reaches here on a value
// copy. The key extends the row-only fingerprint with the dependency edge
// set, because the critical-path DFS reads show.Dependencies — an input
// the row skeleton (and thus the row-only key) does not depend on.
func (m Model) planNetworkFullBuild() networkprojection.Projection {
	key := m.planNetworkFullBuildKey()
	if m.planNetworkBuildCache != nil && m.planNetworkBuildCache.valid && m.planNetworkBuildCache.key == key {
		return m.planNetworkBuildCache.build
	}
	build := networkprojection.Build(m.planNetworkInput())
	if m.planNetworkBuildCache != nil {
		*m.planNetworkBuildCache = planNetworkBuildCacheEntry{valid: true, key: key, build: build}
	}
	return build
}

// planNetworkFullBuildKey fingerprints every input the full build reads:
// the structural row inputs (via the shared row-key shape) PLUS the
// dependency edges the critical-path DFS walks. The next-claimable peek
// reads only the same plan id + task buckets already in the row key, so
// no extra term is needed for it.
func (m Model) planNetworkFullBuildKey() uint64 {
	f := newFingerprint()
	f.writeInt64(int64(m.planNetworkRowsCacheKey()))
	for _, d := range m.planNetworkShow.Dependencies {
		f.writeInt64(d.TaskID)
		f.writeInt64(d.DependsOnTaskID)
	}
	for _, wave := range m.planNetworkShow.Waves {
		for _, task := range wave.Tasks {
			if snapshot, ok := m.tasks[task.TaskID]; ok && snapshot.ParentID != nil {
				f.writeInt64(task.TaskID)
				f.writeInt64(*snapshot.ParentID)
			}
		}
	}
	return f.sum()
}

// invalidatePlanNetworkBuildCache drops the memoised full build. Called
// from the same mutation seams that drop the row cache so a reload always
// recomputes the DFS against the freshly-loaded dependencies.
func (m *Model) invalidatePlanNetworkBuildCache() {
	if m.planNetworkBuildCache != nil {
		m.planNetworkBuildCache.valid = false
	}
}

// planNetworkBuildRows is the row-only shim used by the input
// handlers — they never need the auxiliary indices, only the row
// slice for cursor clamps and kind checks. Skips the SQL peek + the
// critical-path DFS so the projection stays cheap on every keystroke
// (the full projection runs once per render in renderPlanNetwork).
//
// Caches the result on (planID, collapsedMap, per-task id+bucket) so
// the 11 invocation sites across handlePlanNetworkKey (j/k/h/l/space/
// pgup/pgdn/g/G/enter, plus reloadPlanNetwork + togglePlanWaveAtCursor)
// only rebuild when one of those changes — every other call short-
// circuits to the cached slice. Pointer receiver so the cache writes
// back into the model.
func (m *Model) planNetworkBuildRows() []planNetworkRow {
	key := m.planNetworkRowsCacheKey()
	if m.planNetworkRowsCache.valid && m.planNetworkRowsCache.key == key {
		return m.planNetworkRowsCache.rows
	}
	rows := networkprojection.BuildRows(m.planNetworkInput()).Rows
	m.planNetworkRowsCache = planNetworkRowsCacheEntry{
		valid: true,
		key:   key,
		rows:  rows,
	}
	return rows
}

// planNetworkCursorOnTaskRow reports whether the plan-network cursor
// currently sits on a task card (vs. a wave header). The footer uses
// it to describe Enter accurately: Enter on a task row opens the task,
// while Enter on a wave header toggles collapse/expand (see the
// "o", "enter" arm of handlePlanNetworkKey). Value receiver — it
// projects rows directly through the projection's row-only build without writing
// the pointer-receiver row cache, so calling it from the render path
// never mutates model state.
func (m Model) planNetworkCursorOnTaskRow() bool {
	rows := networkprojection.BuildRows(m.planNetworkInput()).Rows
	idx := m.outlineCursor()
	if idx < 0 || idx >= len(rows) {
		return false
	}
	return rows[idx].Kind == planRowTaskCard
}

func (m Model) planNetworkInput() networkprojection.Input {
	return networkprojection.Input{
		Show:            m.planNetworkShow,
		NextClaimableID: m.nextClaimableID,
		FirstBucket:     m.firstBucket,
		FinalBucket:     m.finalBucket,
		Tasks:           m.tasks,
		Collapsed:       m.planNetworkCollapsed,
	}
}

// planNetworkRowsCacheEntry holds the cached row projection plus the
// fingerprint that produced it. valid stays false until the first
// build so a fresh model never hands out a stale empty slice.
type planNetworkRowsCacheEntry struct {
	valid bool
	key   uint64
	rows  []planNetworkRow
}

// planNetworkRowsCacheKey fingerprints the inputs the semantic row projection
// depends on: the focused plan, collapse state, and each task's id/bucket.
func (m Model) planNetworkRowsCacheKey() uint64 {
	f := newFingerprint()
	f.writeInt64(m.planNetworkShow.Plan.ID)

	collapsedKeys := make([]int64, 0, len(m.planNetworkCollapsed))
	for k := range m.planNetworkCollapsed {
		collapsedKeys = append(collapsedKeys, k)
	}
	sort.Slice(collapsedKeys, func(i, j int) bool { return collapsedKeys[i] < collapsedKeys[j] })
	for _, k := range collapsedKeys {
		f.writeInt64(k)
		f.writeBool(m.planNetworkCollapsed[k])
	}

	for _, wv := range m.planNetworkShow.Waves {
		f.writeInt64(wv.Wave.ID)
		for _, t := range wv.Tasks {
			f.writeInt64(t.TaskID)
			f.writeString(t.BucketKey)
		}
	}
	return f.sum()
}

// invalidatePlanNetworkRowsCache drops the memoised projection so the
// next planNetworkBuildRows call rebuilds. Used by mutation paths
// (assign, edit) whose effect on the rows would not be visible via
// the cache key alone — the cache key covers structural inputs, but
// some mutation paths (assignee write, bucket move outside the row
// projection) still want a fresh build after they finish.
func (m *Model) invalidatePlanNetworkRowsCache() {
	m.planNetworkRowsCache.valid = false
	// The full build (DFS + peek + cross-blockers) derives from the same
	// projection, so drop it on the same seam — a reload whose refetched
	// PlanShow fingerprints identically must still recompute against the
	// freshly-loaded dependencies.
	m.invalidatePlanNetworkBuildCache()
}

// ===========================================================================
// Renderer — assemble filament lanes + row bodies, apply scroll window.
// ===========================================================================

// planNetworkTableLayout records the column widths of the bordered
// task table. Title is flex (consumes whatever remains after fixed
// columns); Bucket / Deps are fixed widths chosen so the most
// common values (`backlog`/`dev`/`review` and `#NNN  #MMM`) fit
// without truncation in standard terminals.
type planNetworkTableLayout struct {
	Title  int
	Bucket int
	Deps   int
}

// Total returns the table's interior width (excluding the right
// border character). Narrow terminals can collapse the Deps column
// to zero width, in which case only one inner separator survives;
// the wave-header row uses this to span the full table.
func (l planNetworkTableLayout) Total() int {
	innerSeps := 2
	if l.Deps <= 0 {
		innerSeps = 1
	}
	return l.Title + l.Bucket + l.Deps + innerSeps
}

// planNetworkBuildTable allocates column widths given the available
// width budget AND the measured longest row content. Title hugs the
// longest task/wave text plus a small right padding so Bucket /
// Deps sit close to the title column instead of floating at the
// terminal's right edge. The budget caps Title so the table never
// overflows; the minimum keeps short titles readable.
func planNetworkBuildTable(budget int, measuredTitle int) planNetworkTableLayout {
	bucket := 10
	deps := 14
	minTitle := 24
	const padding = 4
	if budget < minTitle+bucket+deps+3 {
		// Narrow terminal — shrink fixed cols first, let Title own
		// whatever remains.
		if budget < minTitle+bucket+3 {
			bucket = budget - minTitle - 3
			if bucket < 6 {
				bucket = 6
			}
			deps = 0
		} else {
			deps = budget - minTitle - bucket - 3
			if deps < 6 {
				deps = 6
			}
		}
		title := budget - bucket - deps - 2
		if title < minTitle {
			title = minTitle
		}
		return planNetworkTableLayout{Title: title, Bucket: bucket, Deps: deps}
	}
	title := measuredTitle + padding
	if title < minTitle {
		title = minTitle
	}
	if maxTitle := budget - bucket - deps - 2; title > maxTitle {
		title = maxTitle
	}
	return planNetworkTableLayout{Title: title, Bucket: bucket, Deps: deps}
}

// planNetworkMeasureTitle walks every row and returns the largest
// Title-cell content width (rail + glyph + state badge + #id +
// title text + @assignee). Wave header text influences the result
// indirectly by demanding `title >= waveText - bucket - deps - 2`
// so the wave header doesn't truncate under the chosen layout.
func (m Model) planNetworkMeasureTitle(rows []planNetworkRow, bucketW, depsW int) int {
	const innerSeparators = 2
	maxW := 0
	for _, r := range rows {
		w := 0
		switch r.Kind {
		case planRowTaskCard:
			w = m.planNetworkTaskTitleWidth(r)
		case planRowWaveHeader:
			w = m.planNetworkWaveTitleWidth(r, bucketW, depsW, innerSeparators)
		}
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

func (m Model) planNetworkTaskTitleWidth(row planNetworkRow) int {
	railW := screenkit.VisibleWidth(row.Rail)
	badge, _ := m.planNetworkRowStateBadge(row)
	badgeW := 0
	if badge != "" {
		badgeW = screenkit.VisibleWidth(badge) + 1
	}
	title := fmt.Sprintf("#%d %s", row.Task.TaskID, screenkit.Sanitize(row.Task.Title))
	if row.Task.AssignedTo != "" {
		title += " @" + truncateAgentHandle(screenkit.Sanitize(row.Task.AssignedTo), 18)
	}
	return railW + 2 + badgeW + screenkit.VisibleWidth(title)
}

func (m Model) planNetworkWaveTitleWidth(row planNetworkRow, bucketW, depsW, separators int) int {
	glyph := "▼"
	if row.Collapsed {
		glyph = "▶"
	}
	text := fmt.Sprintf(m.t("tui.plans.network.wave_glyph_header_fmt"), glyph, row.WavePos, strings.ToUpper(screenkit.Sanitize(row.WaveName)), row.WaveDone, row.WaveTotal)
	if row.WaveActive {
		text += " " + m.t("tui.plans.network.active_tag")
	}
	return screenkit.VisibleWidth(text) - bucketW - depsW - separators
}

// planNetworkRowStateBadge returns the inline state badge for a task
// row, chosen by precedence:
//
//	done > gated > in-progress > blocked > assigned > next > ready
//
// Rationale per slot:
//   - done           — bucket is the workflow's final position.
//   - gated          — wave is not the plan's active wave.
//   - in-progress    — bucket is BETWEEN first and final (e.g. dev,
//     review). State of fact: work is in flight.
//     Wins over blocked so a blocker added mid-flight
//     does not visually erase the in-flight status.
//   - blocked        — task still in the first bucket with an
//     unfinished blocker chain.
//   - assigned       — task still in the first bucket but has a
//     named owner. Differentiates "claimed,
//     waiting to start" from in-flight work.
//   - next           — next-claimable hint, no owner yet.
//   - ready          — default.
//
// The badge sits just after the status glyph in the Title cell so a
// horizontal scan reveals the state without reading the title text.
// Wave header rows return ("", _). Badge text comes from the i18n
// catalog (tui.plans.network.badge.*).
func (m Model) planNetworkRowStateBadge(row planNetworkRow) (string, networkTone) {
	if row.Kind != planRowTaskCard {
		return "", toneNone
	}
	switch row.Status {
	case networkprojection.StatusDone:
		return m.t("tui.plans.network.badge.done"), toneSuccess
	case networkprojection.StatusGated:
		return m.t("tui.plans.network.badge.gated"), toneMuted
	case networkprojection.StatusInProgress:
		return m.t("tui.plans.network.badge.in_progress"), toneBadgeInfo
	case networkprojection.StatusBlocked:
		return m.t("tui.plans.network.badge.blocked"), toneBadgeBlocker
	case networkprojection.StatusAssigned:
		return m.t("tui.plans.network.badge.assigned"), toneBadgeInfo
	case networkprojection.StatusNext:
		return m.t("tui.plans.network.badge.next"), toneHintAccent
	default:
		return m.t("tui.plans.network.badge.ready"), toneSuccess
	}
}

// planNetworkRowDepsCell collects the dependency ids surfaced for
// this row. Intra-wave non-rail blockers prefix with `←`; cross-wave
// blockers not covered by a filament prefix with `←W`. Returns the
// empty string when there are no deps to surface.
func (m Model) planNetworkRowDepsCell(row planNetworkRow, suppressedCrossIDs map[int64]bool) string {
	var parts []string
	for _, id := range row.IntraBlockers {
		parts = append(parts, fmt.Sprintf("←#%d", id))
	}
	for _, id := range row.CrossBlockers {
		if suppressedCrossIDs[id] {
			continue
		}
		parts = append(parts, fmt.Sprintf(m.t("tui.plans.network.cross_dep_fmt"), id))
	}
	return strings.Join(parts, " ")
}

// padCell pads a one-line cell to width. Delegates to gridtable.PadLine so
// header/separator cells and every other table cell share the same pad policy.
func padCell(s string, width int) string {
	return gridtable.PadLine(s, width)
}

// padCellDashed pads a one-line cell to the given width using a
// `- - - -` pattern (alternating space + dash, starting with a
// space) styled muted. Used for task title cells so the eye can
// trace from the title text to the next column without losing
// alignment. lipgloss width awareness keeps SGR escape sequences
// out of the math.
func (m Model) padCellDashed(s string, width int) string {
	w := screenkit.VisibleWidth(s)
	if w >= width {
		return s
	}
	remaining := width - w
	pad := strings.Repeat(" -", (remaining+1)/2)[:remaining]
	return s + m.styles.Hint.Render(pad)
}

// renderPlanNetworkRowBody renders one row of the bordered task
// table as a single content line. Title / Bucket / Deps are
// truncated to their column widths — multi-line wrapping was
// removed because variable row heights broke cursor / scroll math
// and the user accepted tighter rows over wrapped titles. The
// caller appends a transition separator below this row only when
// the next row is a different kind.
func (m Model) renderPlanNetworkRowBody(row planNetworkRow, selected bool, lanePrimary string, suppressedCrossIDs map[int64]bool, layout planNetworkTableLayout) string {
	if row.Kind == planRowWaveHeader {
		return m.renderPlanNetworkWaveRow(row, selected, lanePrimary, layout)
	}
	if row.Kind != planRowTaskCard {
		return ""
	}
	return m.renderPlanNetworkTaskRow(row, selected, lanePrimary, suppressedCrossIDs, layout)
}

func (m Model) renderPlanNetworkWaveRow(row planNetworkRow, selected bool, lanePrimary string, layout planNetworkTableLayout) string {
	cursor := m.cursorChevron(selected)
	if cursor == "" {
		cursor = "  "
	}
	glyph := "▼"
	if row.Collapsed {
		glyph = "▶"
	}
	text := fmt.Sprintf(m.t("tui.plans.network.wave_glyph_header_fmt"), glyph, row.WavePos, strings.ToUpper(screenkit.Sanitize(row.WaveName)), row.WaveDone, row.WaveTotal)
	if row.WaveActive {
		text += " " + m.t("tui.plans.network.active_tag")
	}
	style := m.styles.HintAccent
	if row.Collapsed && !row.WaveActive {
		style = m.styles.Hint
	}
	border := m.styles.Hint.Render("│")
	return cursor + lanePrimary + style.Render(padCell(truncateText(text, layout.Total()), layout.Total())) + border
}

func (m Model) renderPlanNetworkTaskRow(row planNetworkRow, selected bool, lanePrimary string, suppressedCrossIDs map[int64]bool, layout planNetworkTableLayout) string {
	cursor := m.cursorChevron(selected)
	if cursor == "" {
		cursor = "  "
	}
	border := m.styles.Hint.Render("│")

	statusGlyph, statusStyle := m.planNetworkRowStatusGlyph(row)
	badgeText, badgeStyle := m.planNetworkRowStateBadge(row)
	rail := ""
	if row.Rail != "" {
		rail = m.styles.Hint.Render(row.Rail)
	}
	prefix := m.renderTone(statusStyle, statusGlyph) + " "
	if badgeText != "" {
		prefix += m.renderTone(badgeStyle, badgeText) + " "
	}
	// Sub-task rows get an indent glyph + a parent reference so
	// the WBS hierarchy reads at a glance — including the
	// cross-wave case where a sub-task lands in a different wave
	// than its parent. The parent lookup is in-memory against the
	subPrefix := ""
	parentSuffix := ""
	if row.ParentID != nil {
		subPrefix = m.styles.Hint.Render("↳ ")
		parentSuffix = "  " + m.styles.Hint.Render(fmt.Sprintf("↳#%d", *row.ParentID))
	}
	titleText := fmt.Sprintf("#%d %s", row.Task.TaskID, screenkit.Sanitize(row.Task.Title))
	if row.Task.AssignedTo != "" {
		titleText += " @" + truncateAgentHandle(screenkit.Sanitize(row.Task.AssignedTo), 18)
	}
	prefixWidth := screenkit.VisibleWidth(rail) + screenkit.VisibleWidth(prefix) + screenkit.VisibleWidth(subPrefix)
	suffixWidth := screenkit.VisibleWidth(parentSuffix)
	titleBudget := layout.Title - prefixWidth - suffixWidth
	if titleBudget < 1 {
		titleBudget = 1
	}
	titleText = truncateText(titleText, titleBudget)
	if row.IsCritical {
		titleText = m.styles.Info.Render(titleText)
	}
	titleCell := m.padCellDashed(rail+prefix+subPrefix+titleText+parentSuffix, layout.Title)

	bucketCell := truncateText(screenkit.Sanitize(row.Task.BucketKey), layout.Bucket)
	bucketStyled := m.styles.Hint.Render(padCell(bucketCell, layout.Bucket))

	if layout.Deps <= 0 {
		return cursor + lanePrimary + titleCell + border + bucketStyled + border
	}
	depsCell := truncateText(m.planNetworkRowDepsCell(row, suppressedCrossIDs), layout.Deps)
	depsStyled := m.styles.Hint.Render(padCell(depsCell, layout.Deps))

	return cursor + lanePrimary + titleCell + border + bucketStyled + border + depsStyled + border
}

// planNetworkRowHeights returns the physical line count of each
// row. With the compact layout every row is one content line, plus
// one transition separator below when the next row's kind differs
// (wave→task or task→wave). Heights drive the heights-aware scroll
// helpers without re-rendering each row.
func planNetworkRowHeights(rows []planNetworkRow) []int {
	h := make([]int, len(rows))
	for i, r := range rows {
		h[i] = 1
		if i+1 < len(rows) && rows[i+1].Kind != r.Kind {
			h[i]++ // wave↔task transition sep
		}
	}
	return h
}

// renderPlanNetworkHeaderRow draws the static column-header line at
// the top of the bordered table. The header sits above the first
// data row and is fixed chrome — it never scrolls, so the column
// labels stay visible regardless of cursor position. Narrow
// terminals may zero the Deps column; the helper drops its label
// and trailing border in that case.
func (m Model) renderPlanNetworkHeaderRow(layout planNetworkTableLayout, cursorPad, lane string) string {
	border := m.styles.Hint.Render("│")
	title := m.styles.HintAccent.Render(padCell(m.t("tui.plans.network.column.title"), layout.Title))
	bucket := m.styles.HintAccent.Render(padCell(m.t("tui.plans.network.column.bucket"), layout.Bucket))
	if layout.Deps <= 0 {
		return cursorPad + lane + title + border + bucket + border
	}
	deps := m.styles.HintAccent.Render(padCell(m.t("tui.plans.network.column.deps"), layout.Deps))
	return cursorPad + lane + title + border + bucket + border + deps + border
}

// renderPlanNetworkLaneContinuation paints the lane block for
// continuation / separator sub-lines. Only lanes that are still
// "flowing" through to the next row (source through row before
// final destination) render their vertical `│`; lanes closing at
// this row or beyond render empty cells. Trailing slot is always
// a space (no arrowhead on continuation lines).
func (m Model) renderPlanNetworkLaneContinuation(rowIdx int, filaments []networkprojection.Filament, laneCount int) string {
	if laneCount <= 0 {
		return ""
	}
	cells := make([]string, laneCount)
	for i := range cells {
		cells[i] = " "
	}
	for _, f := range filaments {
		if f.SrcRow <= rowIdx && rowIdx < f.EndRow() {
			cells[f.Lane] = "│"
		}
	}
	return m.styles.Hint.Render(strings.Join(cells, "") + " ")
}

// renderPlanNetworkSeparator emits a row-separator line for the
// bordered table. The junction characters depend on the kinds of
// the rows above and below the separator: a wave-header row has no
// inner column separators, a task row does, so the separator must
// open / close / cross the inner verticals as the layout changes.
// Pass planRowNone for the side that has no row (top / bottom of
// the table).
func (m Model) renderPlanNetworkSeparator(above, below planNetworkRowKind, layout planNetworkTableLayout, cursorPad, lane string) string {
	aboveHasCols := above == planRowTaskCard
	belowHasCols := below == planRowTaskCard

	mid := "─"
	leftSeg := strings.Repeat(mid, layout.Title)
	bucketSeg := strings.Repeat(mid, layout.Bucket)
	depsSeg := strings.Repeat(mid, layout.Deps)

	var firstJunction, secondJunction, rightCorner string
	switch {
	case above == planRowNone:
		// Top border.
		rightCorner = "┐"
		if belowHasCols {
			firstJunction = "┬"
			secondJunction = "┬"
		} else {
			firstJunction = mid
			secondJunction = mid
		}
	case below == planRowNone:
		// Bottom border.
		rightCorner = "┘"
		if aboveHasCols {
			firstJunction = "┴"
			secondJunction = "┴"
		} else {
			firstJunction = mid
			secondJunction = mid
		}
	default:
		rightCorner = "┤"
		switch {
		case aboveHasCols && belowHasCols:
			firstJunction = "┼"
			secondJunction = "┼"
		case aboveHasCols && !belowHasCols:
			firstJunction = "┴"
			secondJunction = "┴"
		case !aboveHasCols && belowHasCols:
			firstJunction = "┬"
			secondJunction = "┬"
		default:
			firstJunction = mid
			secondJunction = mid
		}
	}
	// Narrow-terminal fallback collapses the Deps column to zero
	// width; skip its segment + junction so the right border doesn't
	// double up with the bucket separator.
	var sep string
	if layout.Deps > 0 {
		sep = leftSeg + firstJunction + bucketSeg + secondJunction + depsSeg + rightCorner
	} else {
		sep = leftSeg + firstJunction + bucketSeg + rightCorner
	}
	return cursorPad + lane + m.styles.Hint.Render(sep)
}

// renderPlanNetworkLane paints the fixed-width lane block for one
// row of the outline. Each lane is 1 char wide; a final trailing
// slot carries the horizontal arm head:
//   - "►" on rows where a filament branches or terminates,
//   - "─" on source rows (arm continues into body, no arrowhead),
//   - " " on rows with no filament touch.
//
// Source rows paint ┌ at the source's lane and extend ─ to the
// right edge so the eye traces straight into the task body.
// Intermediate destinations paint ├──►; final destinations paint
// └──►. Crossings between a horizontal arm and an unrelated lane's
// pass-through vertical become ┼ junctions.
// renderPlanNetworkLane paints the fixed-width lane block for one
// row of the outline. Delegates to lane — the glyph math lives
// with the rest of the filament painter in this package.
func (m Model) renderPlanNetworkLane(rowIdx int, filaments []networkprojection.Filament, laneCount int) string {
	return lane(m.styles, rowIdx, filaments, laneCount)
}

// planNetworkRowStatusGlyph maps the row's semantic flags onto a
// status glyph + style. Done and gated rows are distinct (✓ / ⊘);
// every other state collapses to ○ — the parallel inline state badge
// disambiguates blocked / in-progress / next / ready in text. Sharing
// FinalBucket / Gated with planNetworkRowStateBadge keeps both surfaces
// driven by the same flags (no hardcoded bucket-key lookups).
func (m Model) planNetworkRowStatusGlyph(row planNetworkRow) (string, networkTone) {
	switch row.Status {
	case networkprojection.StatusDone:
		return "✓", toneSuccess
	case networkprojection.StatusGated:
		return "⊘", toneMuted
	default:
		return "○", toneHintAccent
	}
}

// ===========================================================================
// Cross-wave + intra-wave dep indices (shared with renderer + filaments).
// ===========================================================================

// ===========================================================================
// Critical-path / next-claimable / deps footer / agent handle helper.
// (Same contract as before — only the call sites moved.)
// ===========================================================================

// planNetworkDepsFooter renders the deterministic dependency groups supplied
// by the projection as the localized outline footer.
// planNetworkDepsFooterBlock is the dependency footer as it reaches the screen:
// styled, and wrapped to the width a bare indented block may paint at.
//
// The footer is indented into the body rather than framed, so no box wraps it —
// an outline with enough cross-task edges painted 124 cells, past a 120-column
// terminal as well as an 80-column one. Both the render and the row budget go
// through here so the wrap cannot be charged in one place and not the other:
// the budget used to measure the unwrapped string and hand the data window a
// row the footer had already taken.
func (m Model) planNetworkDepsFooterBlock(deps []domain.TaskDependency) string {
	footer := m.planNetworkDepsFooter(deps)
	if footer == "" {
		return ""
	}
	return m.kit.WrapBody(m.styles.Hint.Render(footer))
}

func (m Model) planNetworkDepsFooter(deps []domain.TaskDependency) string {
	if len(deps) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(m.t("tui.plans.network.deps_footer_prefix"))
	for i, group := range networkprojection.GroupDependencies(deps) {
		if i > 0 {
			sb.WriteString("  ")
		}
		parts := make([]string, len(group.BlockerIDs))
		for j, b := range group.BlockerIDs {
			parts[j] = fmt.Sprintf("#%d", b)
		}
		fmt.Fprintf(&sb, "#%d→%s", group.TaskID, strings.Join(parts, ","))
	}
	return sb.String()
}

// truncateAgentHandle clamps long @assigned handles to the row
// budget so a verbose model id does not consume the full title
// line. Returns the original string when it already fits. The "…"
// terminator stays an ASCII safe alternative to U+2026 so width math
// survives the lipgloss render.
func truncateAgentHandle(handle string, max int) string {
	if max < 3 {
		return handle
	}
	if len([]rune(handle)) <= max {
		return handle
	}
	rs := []rune(handle)
	return string(rs[:max-1]) + "…"
}

// renderPlanGoalEditor draws the modePlanGoal overlay: a full-panel
// textarea pre-filled with the focused plan's goal_body, plus a kicker
// + key hint above it. The textarea content is sqlite-backed via
// PlanService.UpdateGoalBody; no tempfile / no $EDITOR shell-out.
// planGoalEditorHeader is the chrome above the goal textarea: the plan kicker,
// the key hint and the blank spacer under them.
func (m Model) planGoalEditorHeader() []string {
	return []string{
		m.styles.HintAccent.Render(fmt.Sprintf(m.t("tui.plans.goal.kicker_fmt"), screenkit.Sanitize(m.planNetworkShow.Plan.Slug))),
		m.formHint(m.t("tui.plans.goal.hint.ctrl_s"), m.t("tui.plans.goal.hint.alt_newline"), m.t("tui.plans.goal.hint.esc_cancel")),
		"",
	}
}

// planGoalEditorRows is the inner height the goal textarea gets: the host
// budget minus the panel border, the header above it, and the frame the
// multiline form draws around its own content — the last measured by rendering
// the form at one row.
//
// `m.height - 12` charged twelve rows for a chrome that is seventeen, so the
// editor painted five rows past the bottom of the terminal at every geometry.
func (m Model) planGoalEditorRows(width int, header []string) int {
	probe := field.RenderArea(m.goalInput, width, 1, true, m.multilineFormTheme())
	return m.kit.PanelChrome().Lines(header...).Around(probe).ViewportRows()
}

func (m Model) renderPlanGoalEditor(canvasWidth int) string {
	width := canvasWidth - 10
	if width < 32 {
		width = 32
	}
	header := m.planGoalEditorHeader()
	innerHeight := max(6, m.planGoalEditorRows(width, header))

	area := field.RenderArea(
		m.goalInput,
		width,
		innerHeight,
		true,
		m.multilineFormTheme(),
	)
	return m.renderPanel(strings.Join(append(header, area), "\n"))
}

func (m Model) renderPlanAssignEditor(canvasWidth int) string {
	width := canvasWidth - 10
	if width < 32 {
		width = 32
	}
	area := field.RenderArea(m.assignInput, width, 1, true, m.multilineFormTheme())
	return m.renderPanel(strings.Join([]string{m.styles.HintAccent.Render(fmt.Sprintf(m.t("tui.plans.assign.status_fmt"), m.assignTaskID)), "", area}, "\n"))
}

func (m Model) multilineFormTheme() field.Theme {
	return field.Multiline(m.kit.Styles)
}

func (m Model) formHint(tokens ...string) string {
	var kept []string
	for _, token := range tokens {
		if token != "" {
			kept = append(kept, token)
		}
	}
	return m.styles.Hint.Render(strings.Join(kept, " · "))
}

func (m Model) renderTone(tone networkTone, value string) string {
	switch tone {
	case toneMuted:
		return m.styles.Hint.Render(value)
	case toneHintAccent:
		return m.styles.HintAccent.Render(value)
	case toneSuccess:
		return m.styles.Success.Render(value)
	case toneInfo:
		return m.styles.Info.Render(value)
	case toneBadgeInfo:
		return m.styles.BadgeInfo.Render(value)
	case toneBadgeBlocker:
		return m.styles.BadgeBlocker.Render(value)
	default:
		return value
	}
}

func truncateText(value string, width int) string { return screenkit.Truncate(value, width) }

var _ screenhost.Screen = Screen{}
var _ screenhost.KeyOwner = Screen{}
var _ screenhost.InteractionBlocker = Screen{}
var _ screenhost.FooterOwner = Screen{}
