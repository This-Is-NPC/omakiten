package studio

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/panel"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/selectlist"
)

// HookExecuted is the prepared hook history row the inspector paints.
type HookExecuted = studioprojection.HookExecuted

const (
	sectionHooks          = screenlayout.ID("hooks")
	sectionHooksList      = screenlayout.ID("list")
	sectionHooksInspector = screenlayout.ID("inspector")
	sectionHooksFields    = screenlayout.ID("fields")
	sectionHooksHistory   = screenlayout.ID("history")
	// hooksHistoryWideMin is the inspector body inner width at which HISTORY
	// switches from compact Logs-style rows to the five-column grid. Lower
	// than Stats › Logs' 92 because the inspector is a column, not the full
	// terminal — 120×40 side-by-side still has to show TIME/TYPE/ENTITY.
	hooksHistoryWideMin = 56
	// hooksHistoryDetailMin keeps "3001ms  exec bash timed out" visible before
	// Truncate's ellipsis on the exec fixture's DETAIL cell. The selectable list
	// spends two more cells on its cursor gutter than the former text body did.
	hooksHistoryDetailMin = 30
	hooksHistoryColGap    = 4
	// hooksHistoryHeaderRows is the column heading the wide table pins above its
	// rows, which the zone's floor has to pay for on top of a box's own. The
	// compact table below hooksHistoryWideMin has no heading and simply leaves the
	// row unspent, which is cheaper than a floor that changes with the width.
	hooksHistoryHeaderRows = 1
	// hooksHistoryMarkWidth is the two-column cursor gutter selectlist paints
	// before each row. HISTORY formats its table inside the remaining width and
	// indents the pinned heading to keep every column aligned.
	hooksHistoryMarkWidth = 2
)

// hooksZones names the inspector's two halves: the hook's fields, and the
// hook.executed history under them.
var hooksZones = screenlayout.InspectorIDs{
	Inspector: sectionHooksInspector, Fields: sectionHooksFields,
	Detail: sectionHooksHistory,
}

func (m Screen) hooksRoot() screengrid.Node {
	hooks := m.projection.Hooks
	fieldCount := 0
	if len(hooks) > 0 {
		m.studioHooksClamp(len(hooks))
		fieldCount = len(hookInspectorFields(m, hooks[m.studioHookIndex], nil))
	}
	return screengrid.ListInspector(sectionHooks,
		screengrid.Cell(screenlayout.List(sectionHooksList), m.hooksListBody),
		screengrid.InspectorColumn(hooksZones,
			screengrid.Cell(hooksZones.FieldsSpec(fieldCount), m.hooksFieldsBody),
			screengrid.Cell(hooksZones.DetailSpec(true, hooksHistoryHeaderRows), m.hooksHistoryBody),
			fieldCount > 0, true),
	)
}

func (m Screen) hooksBox() screenlayout.Box {
	return screenlayout.HostBox(m.kit)
}

func (m Screen) hooksGridResult() screengrid.Result {
	return screengrid.Render(m.kit, m.grid, m.hooksBox(), m.hooksRoot())
}

func (m Screen) viewStudioHooks() string {
	view := m.hooksGridResult().View
	if view == "" {
		return screenkit.Indent("\n", 2)
	}
	return screenkit.Indent("\n"+view, 2)
}

func (m Screen) hooksListBody(canvas screenlayout.Canvas) screenlayout.Block {
	held := canvas.Cursor()
	hooks := m.projection.Hooks
	m.studioHooksClamp(len(hooks))
	if held < 0 {
		held = m.studioHookIndex
	}

	notifyN, execN := studioHookShapeCounts(hooks)
	spec := selectlist.Spec{
		Kicker: m.hooksListChrome(len(hooks), notifyN, execN),
		Cursor: held,
		Empty:  m.tr("tui.studio.hooks.empty", "No hooks configured."),
	}
	spec.Width = canvas.Width()
	if len(hooks) == 0 {
		block := selectlist.Paint(m.kit, spec)
		block.Cursor = screenlayout.NoSelection()
		return block
	}
	spec.Rows = make([]selectlist.Row, len(hooks))
	for i, hook := range hooks {
		spec.Rows[i] = selectlist.Row{
			Left:  fmt.Sprintf("%02d // %s", i+1, screenkit.Sanitize(valueOrDash(hook.On))),
			Right: strings.TrimSpace(screenkit.Sanitize(hookWhenShort(hook.When)) + "  " + m.hookShapeLabel(hook)),
		}
	}
	return selectlist.Paint(m.kit, spec)
}

func (m Screen) hooksListChrome(n, notifyN, execN int) string {
	kicker := m.sectionKicker(m.tr("tui.studio.hooks.kicker", "HOOKS"), m.zoneFocused(sectionHooksList))
	trail := m.tr("tui.studio.hooks.summary", "%d  ·  %d notify · %d exec", n, notifyN, execN)
	return kicker + m.styles.Hint.Render(" · "+trail)
}

// hooksFieldsBody is the view-only zone: the selected hook's fields, trimmed to
// the rows the arranger gave this zone and never to a fraction of them. The
// HISTORY box below is its own zone now, so there is nothing left to reserve for
// it here — that reservation is what used to cost the table its rows.
func (m Screen) hooksFieldsBody(canvas screenlayout.Canvas) screenlayout.Block {
	hooks := m.projection.Hooks
	m.studioHooksClamp(len(hooks))
	if len(hooks) == 0 {
		return studioFieldsBlock(nil, nil)
	}
	index := m.studioHookIndex
	spec := hooks[index]
	title := fmt.Sprintf("%02d // %s", index+1, screenkit.Sanitize(valueOrDash(spec.On)))
	rows := m.studioFieldRows(title, m.zoneFocused(sectionHooksFields),
		hookInspectorFields(m, spec, m.hookHistoryFor(index))...)
	return studioFieldsBlock(m.studioFieldTable(canvas.Width(), studioFieldsBudget(canvas, nil), hooksFieldOpts, rows), nil)
}

// hooksFieldOpts is the field table's column policy, named so the zone and the
// test that measures it cannot drift apart.
var hooksFieldOpts = summaryTablesOpts{LabelWidth: 16, ValueWidth: 40, Auto: true}

// hooksHistoryBody is a selectable hook.executed list. The heading stays in
// framed-list chrome while the arranger windows only the execution rows.
func (m Screen) hooksHistoryBody(canvas screenlayout.Canvas) screenlayout.Block {
	held := canvas.Cursor()
	hooks := m.projection.Hooks
	m.studioHooksClamp(len(hooks))
	if len(hooks) == 0 {
		return screenlayout.Block{Cursor: screenlayout.NoSelection()}
	}

	index := m.studioHookIndex
	history := m.hookHistoryFor(index)
	m.studioHooksHistoryClamp(len(history))
	cursor := held
	if cursor < 0 {
		cursor = m.studioHookHistoryIndex
	}
	if len(history) > 0 && cursor >= len(history) {
		cursor = len(history) - 1
	}
	kicker, columns, body := m.hooksHistoryContentFor(index, canvas.Width()-hooksHistoryMarkWidth, history)
	if columns != "" {
		columns = strings.Repeat(" ", hooksHistoryMarkWidth) + columns
	}
	spec := selectlist.Spec{
		Kicker:       kicker,
		ColumnHeader: columns,
		Cursor:       cursor,
		Empty:        m.tr("tui.studio.hooks.history.empty", "No hook.executed rows for this index."),
	}
	if m.studioHookMsg != "" {
		spec.Subtitle = m.styles.Hint.Render(screenkit.Sanitize(m.studioHookMsg))
	}
	if len(body) > 0 {
		spec.Rows = make([]selectlist.Row, len(body))
		for i, row := range body {
			spec.Rows[i] = selectlist.Row{Left: row}
		}
	}
	spec.Width = canvas.Width()
	block := selectlist.Paint(m.kit, spec)
	if len(history) == 0 {
		block.Cursor = screenlayout.NoSelection()
	}
	return block
}

func (m Screen) hooksHistoryContent(index, width int) (kicker, columns string, body []string) {
	return m.hooksHistoryContentFor(index, width, m.hookHistoryFor(index))
}

func (m Screen) hooksHistoryContentFor(index, width int, history []HookExecuted) (kicker, columns string, body []string) {
	kicker = m.sectionKickerKeep(m.tr("tui.studio.hooks.history.kicker", "// HISTORY · hook.executed · #%02d · %d", index+1, len(history)), m.zoneFocused(sectionHooksHistory))
	if len(history) == 0 {
		return kicker, "", nil
	}
	inner := width - panel.Borders
	if inner < 1 {
		inner = 1
	}
	columns, rows := m.hookHistoryTable(history, index, inner)
	return kicker, columns, rows
}

func hookInspectorFields(m Screen, spec config.HookSpec, history []HookExecuted) [][2]string {
	fields := [][2]string{
		{m.tr("tui.studio.hooks.field.on", "on"), screenkit.Sanitize(valueOrDash(spec.On))},
		{m.tr("tui.studio.hooks.field.when", "when"), screenkit.Sanitize(hookWhenFull(spec.When))},
	}
	if studioHookIsNotify(spec) {
		fields = append(fields,
			[2]string{m.tr("tui.studio.hooks.field.notification", "notification"), screenkit.Sanitize(valueOrDash(spec.Notification))},
		)
		if spec.Message != "" {
			fields = append(fields, [2]string{m.tr("tui.studio.hooks.field.message", "message"), screenkit.Sanitize(spec.Message)})
		}
		if spec.MessageField != "" {
			fields = append(fields, [2]string{m.tr("tui.studio.hooks.field.message_field", "message_field"), screenkit.Sanitize(spec.MessageField)})
		}
		if spec.DetailMessage != "" {
			fields = append(fields, [2]string{m.tr("tui.studio.hooks.field.detail_message", "detail_message"), screenkit.Sanitize(spec.DetailMessage)})
		}
		if spec.DetailMessageField != "" {
			fields = append(fields, [2]string{m.tr("tui.studio.hooks.field.detail", "detail"), screenkit.Sanitize(spec.DetailMessageField)})
		}
		return fields
	}
	fields = append(fields,
		[2]string{m.tr("tui.studio.hooks.field.do", "do"), screenkit.Sanitize(valueOrDash(spec.Do))},
		[2]string{m.tr("tui.studio.hooks.field.argv", "argv"), screenkit.Sanitize(hookArgvText(spec.Args))},
		[2]string{m.tr("tui.studio.hooks.field.timeout_ms", "timeout_ms"), screenkit.Sanitize(hookTimeoutText(spec.Args))},
		[2]string{m.tr("tui.studio.hooks.field.last", "last"), screenkit.Sanitize(m.hookLastLabel(history))},
	)
	return fields
}

// hookHistoryTable is the column heading and the rows, returned APART.
//
// The heading used to be the first row, which meant the arranger windowed it:
// scroll two lines into a long history and the table stopped saying what its
// columns were. A heading is chrome, so it goes where chrome goes.
func (m Screen) hookHistoryTable(history []HookExecuted, index, width int) (columns string, rows []string) {
	if width >= hooksHistoryWideMin {
		return m.hookHistoryWide(history, index, width)
	}
	return "", m.hookHistoryCompact(history, index, width)
}

func (m Screen) hookHistoryWide(history []HookExecuted, index, width int) (columns string, rows []string) {
	widths := hookHistoryWidths(width)
	columns = m.styles.Info.Render(gridtable.FormatRow([]string{
		m.tr("tui.log.col.time", "TIME"),
		m.tr("tui.log.col.type", "TYPE"),
		m.tr("tui.log.col.entity", "ENTITY"),
		m.tr("tui.log.col.who", "WHO"),
		m.tr("tui.log.col.detail", "DETAIL"),
	}, widths))
	lines := make([]string, 0, len(history))
	for _, row := range history {
		lines = append(lines, gridtable.FormatRow([]string{
			hookHistoryTime(screenkit.Sanitize(row.CreatedAt), widths[0]),
			screenkit.Sanitize(valueOrDash(row.EventType)),
			hookHistoryEntity(row, index),
			m.hookHistoryStatus(row),
			m.hookHistoryDetail(row),
		}, widths))
	}
	return columns, lines
}

func (m Screen) hookHistoryCompact(history []HookExecuted, _, width int) []string {
	lines := make([]string, 0, len(history))
	for _, row := range history {
		timeStr := hookHistoryTime(screenkit.Sanitize(row.CreatedAt), 8)
		typeStr := screenkit.Sanitize(valueOrDash(row.EventType))
		prefix := fmt.Sprintf(" %s %s ", timeStr, typeStr)
		budget := screenkit.Clamp(width-screenkit.VisibleWidth(prefix), 8, width)
		detail := m.hookHistoryStatus(row) + " " + m.hookHistoryDetail(row)
		lines = append(lines, prefix+gridtable.Truncate(detail, budget))
	}
	return lines
}

// hookHistoryWidths is Logs' 12/20/16/8 natural budget, with DETAIL taking
// whatever remains. Columns shrink toward per-column floors so a long error
// in DETAIL stays scannable inside the inspector column (FitWidths would
// steal from DETAIL first because it is the widest leftover).
func hookHistoryWidths(width int) []int {
	cols := []int{12, 20, 16, 8}
	for {
		detailW := width - hookHistoryPrefixSpan(cols)
		if detailW >= hooksHistoryDetailMin {
			return hookHistoryRowWidths(cols, detailW)
		}
		idx := hookHistoryShrinkIndex(cols)
		if idx < 0 {
			if detailW < 1 {
				detailW = 1
			}
			return hookHistoryRowWidths(cols, detailW)
		}
		cols[idx]--
	}
}

func hookHistoryPrefixSpan(cols []int) int {
	used := hooksHistoryColGap
	for _, w := range cols {
		used += w
	}
	return used
}

func hookHistoryRowWidths(cols []int, detailW int) []int {
	return []int{cols[0], cols[1], cols[2], cols[3], detailW}
}

func hookHistoryShrinkIndex(cols []int) int {
	// TYPE yields first at the narrowest wide-table width so the cursor gutter
	// does not erase the actionable tail of DETAIL. TIME keeps HH:MM:SS, ENTITY
	// keeps a useful id, and WHO keeps the full status token.
	mins := []int{8, 6, 6, 6}
	idx, widest := -1, 0
	for i, w := range cols {
		if w > mins[i] && w > widest {
			widest, idx = w, i
		}
	}
	return idx
}

func (m Screen) hookHistoryStatus(row HookExecuted) string {
	return studioprojection.HookStatus(row, m.tr("tui.studio.hooks.history.ok", "[ok]"), m.tr("tui.studio.hooks.history.fail", "[fail]"))
}

func hookHistoryEntity(row HookExecuted, index int) string {
	return studioprojection.HookEntity(row, index)
}

func (m Screen) hookHistoryDetail(row HookExecuted) string {
	return screenkit.Sanitize(studioprojection.HookDetail(row, m.tr("tui.studio.hooks.history.duration", "%dms", row.DurationMs)))
}

// hookHistoryTime trims a SQLite "YYYY-MM-DD HH:MM:SS" stamp to its trailing
// `width` bytes — the same rightmost-component rule Stats › Logs uses, copied
// here so Studio does not import that screen.
func hookHistoryTime(ts string, width int) string {
	return studioprojection.HookHistoryTime(ts, width)
}

func (m Screen) hookHistoryFor(index int) []HookExecuted {
	return m.hookHistory[index]
}

func (m *Screen) handleStudioHooksKey(msg tea.KeyMsg) tea.Cmd {
	if studioHorizontalNavKey(msg.String()) {
		return nil
	}
	if msg.String() == "ctrl+s" {
		if m.studioDraft == nil {
			m.studioHookMsg = m.tr("tui.studio.hooks.msg.no_changes_to_save", "no Studio Hooks changes to save")
			return nil
		}
		m.openStudioApplyOverlay()
		return nil
	}
	hooks := m.projection.Hooks
	if len(hooks) == 0 {
		m.handleStudioHooksGridKey(msg.String())
		return nil
	}
	m.studioHooksClamp(len(hooks))
	// One owner for the cursor: the GRID. It routes every key to the focused zone
	// and handleStudioHooksGridKey reads the answer back, so there is no branch
	// here on which zone holds the focus and no resync to tell the grid what the
	// screen decided behind its back.
	m.handleStudioHooksGridKey(msg.String())
	return nil
}

func (m *Screen) handleStudioHooksGridKey(key string) {
	root := m.hooksRoot()
	box := m.hooksBox()
	next, handled := m.grid.HandleKey(m.kit, box, key, root)
	if !handled {
		return
	}
	m.grid = next
	if cur := m.grid.Layout().Cursor(sectionHooksList); cur >= 0 {
		previous := m.studioHookIndex
		m.studioHookIndex = cur
		m.studioHooksClamp(len(m.projection.Hooks))
		if m.studioHookIndex != previous {
			m.studioHookHistoryIndex = 0
			m.grid = m.grid.WithCursor(sectionHooksHistory, 0).Resync(m.kit, box, m.hooksRoot())
		}
	}
	if cur := m.grid.Layout().Cursor(sectionHooksHistory); cur >= 0 {
		m.studioHookHistoryIndex = cur
		m.studioHooksHistoryClamp(len(m.hookHistoryFor(m.studioHookIndex)))
	}
}

// resyncHooksGrid seeds both selectable zones from the persisted screen state
// and lets the grid clamp them against the rows currently available.
func (m *Screen) resyncHooksGrid() {
	hooks := m.projection.Hooks
	m.studioHooksClamp(len(hooks))
	historyCount := 0
	if len(hooks) > 0 {
		historyCount = len(m.hookHistoryFor(m.studioHookIndex))
	}
	m.studioHooksHistoryClamp(historyCount)
	m.grid = m.grid.WithCursor(sectionHooksList, m.studioHookIndex).
		WithCursor(sectionHooksHistory, m.studioHookHistoryIndex).
		Resync(m.kit, m.hooksBox(), m.hooksRoot())
}

func (m Screen) studioHookSpecs() []config.HookSpec {
	return append([]config.HookSpec(nil), m.projection.Hooks...)
}

// studioHooksCandidate is the bundle alone, for the read-only paint paths. Building the
// report to throw it away is two deep clones and a diff per body — see
// [StudioDraft.candidateBundle].
func (m *Screen) studioHooksClamp(n int) {
	if n <= 0 {
		m.studioHookIndex = 0
		return
	}
	if m.studioHookIndex < 0 {
		m.studioHookIndex = 0
	}
	if m.studioHookIndex >= n {
		m.studioHookIndex = n - 1
	}
}

func (m *Screen) studioHooksHistoryClamp(n int) {
	if n <= 0 {
		m.studioHookHistoryIndex = 0
		return
	}
	if m.studioHookHistoryIndex < 0 {
		m.studioHookHistoryIndex = 0
	}
	if m.studioHookHistoryIndex >= n {
		m.studioHookHistoryIndex = n - 1
	}
}

func (m Screen) hookShapeLabel(spec config.HookSpec) string {
	if studioprojection.HookIsNotify(spec) {
		return m.tr("tui.studio.hooks.shape.notify", "notify")
	}
	if strings.TrimSpace(spec.Do) == "exec" {
		return m.tr("tui.studio.hooks.shape.exec", "exec")
	}
	if do := strings.TrimSpace(spec.Do); do != "" {
		return screenkit.Sanitize(do)
	}
	return "-"
}

func studioHookIsNotify(spec config.HookSpec) bool {
	return studioprojection.HookIsNotify(spec)
}

func studioHookShapeCounts(hooks []config.HookSpec) (notifyN, execN int) {
	return studioprojection.HookShapeCounts(hooks)
}

func hookWhenShort(when map[string]string) string {
	return studioprojection.HookWhenShort(when)
}

func hookWhenFull(when map[string]string) string {
	return studioprojection.HookWhenFull(when)
}

func hookArgvText(args map[string]interface{}) string {
	return studioprojection.HookArgvText(args)
}

func hookTimeoutText(args map[string]interface{}) string {
	return studioprojection.HookTimeoutText(args)
}

func (m Screen) hookLastLabel(history []HookExecuted) string {
	return studioprojection.HookLastLabel(history, m.tr("tui.studio.hooks.last.fail_timeout", "fail timeout"))
}

// HookIndexFor returns the first matching hook index, or 0.
func HookIndexFor(hooks []config.HookSpec, pred func(config.HookSpec) bool) int {
	for i, spec := range hooks {
		if pred(spec) {
			return i
		}
	}
	return 0
}
