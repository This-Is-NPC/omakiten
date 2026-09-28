package studio

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/config/bundledraft"
	"omakiten/internal/studioprojection"
	"omakiten/internal/tui/components/gridtable"
	"omakiten/internal/tui/components/screengrid"
	"omakiten/internal/tui/components/screenkit"
	"omakiten/internal/tui/components/screenlayout"
	"omakiten/internal/tui/components/selectlist"
)

// Workflow is the production replacement for the Overview / Buckets / Flow /
// Guards dump tabs: one list of buckets with guards (and open transitions)
// as rows between them, plus an inspector for the focused row. Same
// ListInspector body as Commands — the breakpoint is the vocabulary's.
const (
	sectionWorkflow          = screenlayout.ID("workflow")
	sectionWorkflowList      = screenlayout.ID("list")
	sectionWorkflowInspector = screenlayout.ID("inspector")
	sectionWorkflowFields    = screenlayout.ID("fields")
	sectionWorkflowDetail    = screenlayout.ID("detail")
)

const (
	workflowRowBucket = studioprojection.WorkflowBucket
	workflowRowGuard  = studioprojection.WorkflowGuard
	workflowRowOpen   = studioprojection.WorkflowOpen
	workflowRowAdd    = studioprojection.WorkflowAdd
	workflowRowOp     = studioprojection.WorkflowOperation
)

var studioWorkflowBucketFields = []string{
	"key", "position", "task_edit", "task_delete", "comment_create", "comment_edit", "comment_delete",
}

// workflowZones names the inspector's two halves. Which of them a row HAS is
// [Screen.workflowInspectorShape]'s answer, not a constant.
var workflowZones = screenlayout.InspectorIDs{
	Inspector: sectionWorkflowInspector, Fields: sectionWorkflowFields,
	Detail: sectionWorkflowDetail,
}

func (m Screen) workflowRoot() screengrid.Node {
	fields, detail := m.workflowInspectorShape()
	fieldCount := m.workflowFieldCount()
	return screengrid.ListInspector(sectionWorkflow,
		screengrid.Cell(screenlayout.List(sectionWorkflowList), m.workflowListBody),
		screengrid.InspectorColumn(workflowZones,
			screengrid.Cell(workflowZones.FieldsSpec(fieldCount), m.workflowFieldsBody),
			screengrid.Cell(workflowZones.DetailSpec(false, 0), m.workflowDetailBody),
			fields, detail),
	)
}

func (m Screen) workflowBox() screenlayout.Box {
	return screenlayout.HostBox(m.kit)
}

func (m Screen) workflowGridResult() screengrid.Result {
	return screengrid.Render(m.kit, m.grid, m.workflowBox(), m.workflowRoot())
}

func (m Screen) viewStudioWorkflow() string {
	view := m.workflowGridResult().View
	if view == "" {
		return screenkit.Indent("\n", 2)
	}
	return screenkit.Indent("\n"+view, 2)
}

func (m Screen) workflowListBody(canvas screenlayout.Canvas) screenlayout.Block {
	held := canvas.Cursor()
	workflow := m.projection.Workflow
	rows := m.projection.WorkflowRows
	m.studioWorkflowClamp(len(rows), 0)
	if held < 0 {
		held = m.studioWorkflowIndex
	}

	kicker, subtitle := m.workflowListChrome(workflow, m.zoneFocused(sectionWorkflowList))
	spec := selectlist.Spec{
		Kicker:   kicker,
		Subtitle: subtitle,
		Cursor:   held,
		Empty:    m.tr("tui.studio.workflow.empty", "No workflow buckets configured."),
	}
	spec.Width = canvas.Width()
	if len(rows) == 0 {
		block := selectlist.Paint(m.kit, spec)
		block.Cursor = screenlayout.NoSelection()
		return block
	}
	spec.Rows = make([]selectlist.Row, len(rows))
	for i, row := range rows {
		left, right := screenkit.Sanitize(row.Left), screenkit.Sanitize(row.Right)
		if i == held {
			spec.Rows[i] = selectlist.Row{Left: left, Right: right}
			continue
		}
		row.Left, row.Right = left, right
		spec.Rows[i] = paintWorkflowListRow(m.styles, row)
	}
	return selectlist.Paint(m.kit, spec)
}

// workflowListChrome is the SVG header: `// WORKFLOW · {key} · {clean|dirty}`
// then `{n} buckets · {m} guards · {state}` in Hint, without a second kicker.
// Guard count is transition guards only — operation guards stay off this line.
func (m Screen) workflowListChrome(workflow config.Workflow, focused bool) (kicker, subtitle string) {
	state := m.studioDirtyStateLabel()
	kicker = m.sectionKicker(m.tr("tui.studio.workflow.kicker", "WORKFLOW"), focused)
	if workflow.Key != "" {
		kicker += m.styles.Hint.Render(" · " + screenkit.Sanitize(workflow.Key) + " · " + state)
	}
	subtitle = m.styles.Hint.Render(m.tr(
		"tui.studio.workflow.summary",
		"%d buckets · %d guards · %s",
		len(workflow.Buckets),
		workflowTransitionGuardCount(workflow),
		state,
	))
	return kicker, subtitle
}

func workflowTransitionGuardCount(workflow config.Workflow) int {
	n := 0
	for _, t := range workflow.Transitions {
		n += len(t.Guards)
	}
	return n
}

// workflowSelectedRow is the row the inspector is describing, and whether there
// is one at all.
func (m Screen) workflowSelectedRow() (studioprojection.WorkflowRow, config.Workflow, bool) {
	workflow := m.projection.Workflow
	rows := m.projection.WorkflowRows
	m.studioWorkflowClamp(len(rows), 0)
	if len(rows) == 0 {
		return studioprojection.WorkflowRow{}, workflow, false
	}
	return rows[m.studioWorkflowIndex], workflow, true
}

// workflowInspectorShape is which of the inspector's two zones THIS row has.
//
// Workflow is the sub-screen whose inspector changes shape as the selection
// moves: a bucket is a field table with no box under it, an open transition is a
// box with no table above it, and a tagged guard is both. The tree is rebuilt
// every frame from the model, so the answer is declared rather than painted
// around — and because the ring is what the layout resolved, `tab` offers
// exactly the zones that are on screen for the row you are on.
func (m Screen) workflowInspectorShape() (fields, detail bool) {
	row, workflow, ok := m.workflowSelectedRow()
	if !ok {
		return false, false
	}
	switch row.Kind {
	case workflowRowBucket:
		fields = true
	case workflowRowGuard, workflowRowOp:
		fields = true
		detail = workflowGuardHasExample(row.Guard)
	case workflowRowOpen, workflowRowAdd:
		detail = true
	}
	return fields, detail || len(m.workflowDraftNotes(workflow)) > 0
}

// workflowFieldCount is how many fields the selected row's table has, which is
// the height its zone declares. Buckets are the fixed nine; a guard's table
// depends on its type, so it is counted from the fields themselves rather than
// from a constant that would have to be kept in step with them.
func (m Screen) workflowFieldCount() int {
	row, workflow, ok := m.workflowSelectedRow()
	if !ok {
		return 0
	}
	switch row.Kind {
	case workflowRowBucket:
		return len(m.workflowBucketFields(row.Bucket, workflow))
	case workflowRowGuard, workflowRowOp:
		return len(m.workflowGuardFields(row))
	}
	return 0
}

// workflowGuardHasExample reports whether this guard paints a preview or example
// box under its fields.
func workflowGuardHasExample(guard config.TransitionGuard) bool {
	return guard.Type == "blockers_in" || (guard.Type == "comments_tagged" && guard.Tag == "resume")
}

// workflowDraftNotes is what the draft has to say about the candidate: the
// screen message, the validation error, the diff and the flow warnings. They
// belong to the detail zone, and they are what gives a bucket row a detail zone
// it would not otherwise have.
func (m Screen) workflowDraftNotes(workflow config.Workflow) []string {
	var notes []string
	report := m.studioPreviewReport()
	if m.studioWorkflowMsg != "" {
		notes = append(notes, m.styles.Hint.Render(screenkit.Sanitize(m.studioWorkflowMsg)))
	}
	if report.ValidationError != nil {
		notes = append(notes, m.styles.Hint.Render(m.tr("tui.studio.flow.validation", "Validation: %s", screenkit.Sanitize(report.ValidationError.Error()))))
	}
	if len(report.DiffSummary) > 0 && report.Dirty {
		notes = append(notes, m.tr("tui.studio.flow.candidate_diff", "Candidate diff:"))
		notes = append(notes, prefixLines(report.DiffSummary, "- ")...)
	}
	if warnings := m.projection.FlowWarnings; len(warnings) > 0 {
		notes = append(notes, m.tr("tui.studio.flow.warnings_header", "Flow warnings:"))
		notes = append(notes, prefixLines(warnings, "- ")...)
	}
	return notes
}

// workflowFieldsBody is the view-only zone: the selected row's fields, with the
// bucket key hint as unscrolled footer chrome under them.
func (m Screen) workflowFieldsBody(canvas screenlayout.Canvas) screenlayout.Block {
	row, workflow, ok := m.workflowSelectedRow()
	if !ok {
		return studioFieldsBlock(nil, nil)
	}
	var table, footer []string
	switch row.Kind {
	case workflowRowBucket:
		// The footer is measured BEFORE the table, because it is what the table
		// has to fit around: it is chrome the arranger never windows, so a table
		// sized against the whole zone would push it off the bottom.
		footer = m.workflowBucketFooter()
		table = m.workflowInspectorBucket(row.Bucket, workflow, canvas.Width(), studioFieldsBudget(canvas, footer))
	case workflowRowGuard, workflowRowOp:
		table = m.workflowInspectorGuardFields(row, canvas.Width(), studioFieldsBudget(canvas, nil))
	}
	return studioFieldsBlock(table, footer)
}

// workflowDetailBody is the box under the fields: a template preview, a pending
// blockers example, the help for an open transition, or the draft's notes.
func (m Screen) workflowDetailBody(canvas screenlayout.Canvas) screenlayout.Block {
	_ = canvas.Cursor()
	row, workflow, ok := m.workflowSelectedRow()
	var kicker string
	var body []string
	if ok {
		kicker, body = m.workflowDetailContent(row)
	}
	body = append(body, m.workflowDraftNotes(workflow)...)
	return m.inspectorBox(canvas, kicker, "", body, m.zoneFocused(sectionWorkflowDetail))
}

func (m Screen) workflowDetailContent(row studioprojection.WorkflowRow) (kicker string, body []string) {
	switch row.Kind {
	case workflowRowGuard, workflowRowOp:
		return m.workflowInspectorGuardExample(row)
	case workflowRowOpen:
		return m.workflowInspectorOpen(row)
	case workflowRowAdd:
		return m.sectionKickerKeep(m.tr("tui.studio.workflow.add_home", "TRANSITION ADD/REMOVE"), m.zoneFocused(sectionWorkflowDetail)),
			splitNonEmpty(m.tr("tui.studio.workflow.add_help", "Named home for adding or removing an outbound transition.\nPress a to add the next missing edge; d removes the selected open edge."))
	default:
		return "", nil
	}
}

// workflowBucketFooter is the bucket zone's key hint, painted as unscrolled
// chrome under the table.
func (m Screen) workflowBucketFooter() []string {
	return []string{m.styles.Hint.Render(m.tr("tui.studio.workflow.bucket_hint", "j/k selects a guard between buckets · [ ] reorder · K key · e/X/C permissions"))}
}

func (m Screen) workflowInspectorBucket(bucket config.Bucket, workflow config.Workflow, width, rows int) []string {
	fields := m.workflowBucketFields(bucket, workflow)
	m.studioWorkflowClamp(0, len(studioWorkflowBucketFields))
	title := fmt.Sprintf("%02d // %s", bucket.Position, screenkit.Sanitize(bucket.Key))
	cells := make([][]gridtable.Cell, 0, len(fields)+1)
	cells = append(cells, []gridtable.Cell{gridtable.Styled(m.studioFieldsTitle(title, m.zoneFocused(sectionWorkflowFields)))})
	for i, field := range fields {
		label := field[0]
		if i > 0 && i-1 == m.studioWorkflowField && i-1 < len(studioWorkflowBucketFields) {
			label = "> " + label
		}
		cells = append(cells, []gridtable.Cell{gridtable.Raw(label), gridtable.Raw(field[1])})
	}
	return m.studioFieldTable(width, rows, workflowBucketFieldOpts, cells)
}

// workflowBucketFieldOpts is the bucket table's column policy, named so the zone
// and anything that measures it cannot drift apart.
var workflowBucketFieldOpts = summaryTablesOpts{LabelWidth: 16, ValueWidth: 32, Auto: true}

func (m Screen) workflowBucketFields(bucket config.Bucket, workflow config.Workflow) [][2]string {
	counts := m.projection.TaskCounts
	return [][2]string{
		{m.tr("tui.studio.workflow.field.bucket", "bucket"), screenkit.Sanitize(strings.ToLower(bucket.Name))},
		{m.tr("tui.studio.buckets.label.key", "key"), screenkit.Sanitize(bucket.Key)},
		{m.tr("tui.studio.buckets.label.position", "position"), fmt.Sprintf("%d", bucket.Position)},
		{m.tr("tui.studio.buckets.label.active_tasks", "active tasks"), fmt.Sprintf("%d", counts[bucket.Key])},
		{m.tr("tui.studio.buckets.label.task_edit", "task edit"), workflowPermissionLabel(m.t, bucket, workflow.Defaults, StudioBucketPermissionTask, StudioBucketPermissionEdit)},
		{m.tr("tui.studio.buckets.label.task_delete", "task delete"), workflowPermissionLabel(m.t, bucket, workflow.Defaults, StudioBucketPermissionTask, StudioBucketPermissionDelete)},
		{m.tr("tui.studio.buckets.label.comment_create", "comment create"), workflowPermissionLabel(m.t, bucket, workflow.Defaults, StudioBucketPermissionComment, StudioBucketPermissionCreate)},
		{m.tr("tui.studio.buckets.label.comment_edit", "comment edit"), workflowPermissionLabel(m.t, bucket, workflow.Defaults, StudioBucketPermissionComment, StudioBucketPermissionEdit)},
		{m.tr("tui.studio.buckets.label.comment_delete", "comment delete"), workflowPermissionLabel(m.t, bucket, workflow.Defaults, StudioBucketPermissionComment, StudioBucketPermissionDelete)},
	}
}

func (m Screen) workflowInspectorGuardFields(row studioprojection.WorkflowRow, width, rows int) []string {
	guard := row.Guard
	title := strings.TrimSpace(screenkit.Sanitize(row.Left))
	if title == "" {
		title = screenkit.Sanitize(guard.Type)
	}
	cells := m.studioFieldRows(title, m.zoneFocused(sectionWorkflowFields), m.workflowGuardFields(row)...)
	return m.studioFieldTable(width, rows, workflowGuardFieldOpts, cells)
}

// workflowGuardFieldOpts is the guard table's column policy.
var workflowGuardFieldOpts = summaryTablesOpts{LabelWidth: 12, ValueWidth: 40, Auto: true}

func (m Screen) workflowGuardFields(row studioprojection.WorkflowRow) [][2]string {
	guard := row.Guard
	on := ""
	if row.Kind != workflowRowOp && row.Bucket.Key != "" && row.To.Key != "" {
		on = row.Bucket.Key + " -> " + row.To.Key
	} else if row.OpKind != "" {
		on = string(row.OpKind)
	}
	fields := [][2]string{
		{m.tr("tui.studio.workflow.field.type", "type"), screenkit.Sanitize(valueOrDash(guard.Type))},
	}
	switch guard.Type {
	case "comments_tagged":
		fields = append(fields,
			[2]string{m.tr("tui.studio.workflow.field.tag", "tag"), screenkit.Sanitize(guard.Tag)},
			[2]string{m.tr("tui.studio.workflow.field.count", "count"), fmt.Sprintf("%d", guard.Count)},
		)
	case "comments_min":
		fields = append(fields, [2]string{m.tr("tui.studio.workflow.field.count", "count"), fmt.Sprintf("%d", guard.Count)})
	case "blockers_in":
		fields = append(fields, [2]string{m.tr("tui.studio.workflow.field.buckets", "buckets"), screenkit.Sanitize(strings.Join(guard.Buckets, ", "))})
	}
	if on != "" {
		fields = append(fields, [2]string{m.tr("tui.studio.workflow.field.on", "on"), screenkit.Sanitize(on)})
	}
	if guard.Type == "comments_tagged" && guard.Tag != "" {
		fields = append(fields, [2]string{m.tr("tui.studio.workflow.field.template", "template"), "comment-" + screenkit.Sanitize(guard.Tag)})
	}
	if guard.Hint != "" {
		hint := m.resolveStudioGuardHint(guard.Hint)
		if hint == "" {
			hint = guard.Hint
		}
		fields = append(fields, [2]string{m.tr("tui.studio.workflow.field.hint", "hint"), screenkit.Sanitize(hint)})
	}
	return fields
}

func (m Screen) workflowInspectorGuardExample(row studioprojection.WorkflowRow) (kicker string, body []string) {
	guard := row.Guard
	switch {
	case guard.Type == "comments_tagged" && guard.Tag == "resume":
		return m.sectionKicker(m.tr("tui.studio.workflow.preview_kicker", "PREVIEW · comment-resume  (agent fills this)"), m.zoneFocused(sectionWorkflowDetail)),
			splitNonEmpty(m.workflowCommentResumePreview())
	case guard.Type == "blockers_in":
		return m.sectionKicker(m.tr("tui.studio.workflow.blockers_kicker", "EXAMPLE · pending blockers (guard fires)"), m.zoneFocused(sectionWorkflowDetail)),
			m.workflowPendingBlockers(guard)
	}
	return "", nil
}

func (m Screen) workflowInspectorOpen(row studioprojection.WorkflowRow) (kicker string, body []string) {
	kicker = m.sectionKickerKeep(m.tr("tui.studio.workflow.open_selected", "OPEN: %s -> %s", screenkit.Sanitize(row.Bucket.Key), screenkit.Sanitize(row.To.Key)), m.zoneFocused(sectionWorkflowDetail))
	body = splitNonEmpty(m.tr("tui.studio.workflow.open_help", "Unguarded transition. Press d to remove it, a to add another outbound edge from this bucket."))
	return kicker, body
}

func (m Screen) workflowCommentResumePreview() string {
	body := m.projection.TemplateBodies["comment-resume"]
	if strings.TrimSpace(body) == "" {
		return m.tr("tui.studio.workflow.preview_missing", "(comment-resume template is not in this catalog)")
	}
	return screenkit.SanitizeMultiline(strings.TrimSpace(body))
}

func (m Screen) workflowPendingBlockers(guard config.TransitionGuard) []string {
	allowed := map[string]struct{}{}
	for _, key := range guard.Buckets {
		allowed[key] = struct{}{}
	}
	var pending []string
	for _, task := range m.tasks {
		if task.BucketKey == "" {
			continue
		}
		if _, ok := allowed[task.BucketKey]; ok {
			continue
		}
		pending = append(pending, fmt.Sprintf("#%-4d %-22s  %s", task.ID, screenkit.Sanitize(task.Title), screenkit.Sanitize(task.BucketKey)))
		if len(pending) >= 8 {
			break
		}
	}
	if len(pending) == 0 {
		return []string{m.tr("tui.studio.workflow.blockers_empty", "No pending blockers in disallowed buckets.")}
	}
	pending = append(pending, "", m.tr("tui.studio.workflow.blockers_footer", "%d pending · move these to done to pass", len(pending)))
	return pending
}

func (m *Screen) handleStudioWorkflowKey(msg tea.KeyMsg) tea.Cmd {
	if studioHorizontalNavKey(msg.String()) {
		return nil
	}
	workflow := m.projection.Workflow
	rows := m.projection.WorkflowRows
	if len(rows) == 0 {
		m.handleStudioWorkflowGridKey(msg.String())
		return nil
	}
	m.studioWorkflowClamp(len(rows), 0)
	row := rows[m.studioWorkflowIndex]
	focus := m.grid.Focus()
	if cmd, handled := m.applyStudioWorkflowKey(msg.String(), workflow, rows, row, focus); handled {
		return cmd
	}
	m.syncWorkflowListCursor()
	return nil
}

func (m *Screen) applyStudioWorkflowKey(key string, workflow config.Workflow, rows []studioprojection.WorkflowRow, row studioprojection.WorkflowRow, focus screenlayout.ID) (tea.Cmd, bool) {
	if handled := m.applyStudioWorkflowNavigation(key, rows, focus); handled {
		return nil, true
	}
	switch key {
	case "a":
		m.addWorkflowTransition(row, workflow)
	case "d":
		m.removeWorkflowEdge(row)
	case "[":
		m.moveWorkflowBucket(row, -1)
	case "]":
		m.moveWorkflowBucket(row, 1)
	case "K":
		m.cycleWorkflowBucketKey(row)
	case "e":
		m.toggleWorkflowPermission(row, StudioBucketPermissionTask, StudioBucketPermissionEdit)
	case "X":
		m.toggleWorkflowPermission(row, StudioBucketPermissionTask, StudioBucketPermissionDelete)
	case "C":
		m.toggleWorkflowPermission(row, StudioBucketPermissionComment, StudioBucketPermissionCreate)
	case "ctrl+s":
		if m.studioDraft == nil {
			m.studioWorkflowMsg = m.tr("tui.studio.workflow.msg.no_changes_to_save", "no Studio Workflow changes to save")
			return nil, true
		}
		m.openStudioApplyOverlay()
		return nil, true
	default:
		m.handleStudioWorkflowGridKey(key)
		return nil, true
	}
	return nil, false
}

func (m *Screen) applyStudioWorkflowNavigation(key string, rows []studioprojection.WorkflowRow, focus screenlayout.ID) bool {
	switch key {
	case "up", "k":
		if studioZoneIsInspector(focus, sectionWorkflowList) {
			if m.studioWorkflowField > 0 {
				m.studioWorkflowField--
			}
			return true
		}
		if m.studioWorkflowIndex > 0 {
			m.studioWorkflowIndex--
		}
		m.syncWorkflowListCursor()
		return true
	case "down", "j":
		if studioZoneIsInspector(focus, sectionWorkflowList) {
			if m.studioWorkflowField < len(studioWorkflowBucketFields)-1 {
				m.studioWorkflowField++
			}
			return true
		}
		if m.studioWorkflowIndex < len(rows)-1 {
			m.studioWorkflowIndex++
		}
		m.syncWorkflowListCursor()
		return true
	}
	return false
}

func (m *Screen) handleStudioWorkflowGridKey(key string) {
	root := m.workflowRoot()
	box := m.workflowBox()
	next, handled := m.grid.HandleKey(m.kit, box, key, root)
	if !handled {
		return
	}
	m.grid = next
	if cur := m.grid.Layout().Cursor(sectionWorkflowList); cur >= 0 {
		m.studioWorkflowIndex = cur
		m.studioWorkflowClamp(len(m.projection.WorkflowRows), 0)
	}
}

func (m *Screen) syncWorkflowListCursor() {
	root := m.workflowRoot()
	box := m.workflowBox()
	m.grid = m.grid.WithCursor(sectionWorkflowList, m.studioWorkflowIndex).Resync(m.kit, box, root)
}

func (m *Screen) resyncWorkflowGrid() {
	m.grid = m.grid.Resync(m.kit, m.workflowBox(), m.workflowRoot())
}

func (m Screen) studioWorkflowOnly() config.Workflow {
	workflow := m.studioWorkflowCandidate()
	return workflow
}

// studioWorkflowCandidate is the active workflow alone, for the read-only paint
// paths. Building the report to throw it away is two deep clones and a diff per
// body — see [StudioDraft.candidateBundle].
func (m Screen) studioWorkflowCandidate() config.Workflow {
	if m.studioDraft != nil {
		if workflow, ok := bundledraft.ActiveWorkflow(m.studioDraft.Candidate()); ok {
			return workflow
		}
	}
	return configWorkflowFromDomain(m.workflow, m.repos.activeSnapshot())
}

func (m *Screen) studioWorkflowClamp(n, fields int) {
	if n > 0 {
		if m.studioWorkflowIndex < 0 {
			m.studioWorkflowIndex = 0
		}
		if m.studioWorkflowIndex >= n {
			m.studioWorkflowIndex = n - 1
		}
	} else if n == 0 && fields == 0 {
		m.studioWorkflowIndex, m.studioWorkflowField = 0, 0
	}
	if fields <= 0 {
		fields = len(studioWorkflowBucketFields)
	}
	if m.studioWorkflowField < 0 {
		m.studioWorkflowField = 0
	}
	if m.studioWorkflowField >= fields {
		m.studioWorkflowField = fields - 1
	}
}

func (m *Screen) addWorkflowTransition(row studioprojection.WorkflowRow, workflow config.Workflow) {
	if err := m.ensureStudioDraft(); err != nil {
		m.studioWorkflowMsg = err.Error()
		return
	}
	fromID, toID, ok := workflowAddTarget(row, workflow)
	if !ok {
		m.studioWorkflowMsg = m.tr("tui.studio.workflow.msg.no_destination", "no remaining destination for a new transition")
		return
	}
	report := m.studioDraft.AddTransition(fromID, toID)
	m.studioWorkflowMsg = studioDraftReportMessage(report, m.tr("tui.studio.flow.msg.transition_added", "transition added"))
	m.refreshProjection()
}

func (m *Screen) removeWorkflowEdge(row studioprojection.WorkflowRow) {
	if err := m.ensureStudioDraft(); err != nil {
		m.studioWorkflowMsg = err.Error()
		return
	}
	switch row.Kind {
	case workflowRowOpen:
		report := m.studioDraft.RemoveTransition(row.Bucket.ID, row.To.ID)
		m.studioWorkflowMsg = studioDraftReportMessage(report, m.tr("tui.studio.flow.msg.transition_removed", "transition removed"))
		m.refreshProjection()
	case workflowRowGuard:
		kind := StudioGuardSetTransition
		if row.OpKind != "" {
			kind = row.OpKind
		}
		report := m.studioDraft.RemoveGuard(kind, row.Bucket.ID, row.To.ID, row.GuardIndex)
		m.studioWorkflowMsg = studioDraftReportMessage(report, m.tr("tui.studio.guards.msg.guard_removed", "guard removed"))
		m.refreshProjection()
	default:
		m.studioWorkflowMsg = m.tr("tui.studio.workflow.msg.nothing_to_remove", "select an open transition or a guard to remove")
	}
}

func (m *Screen) moveWorkflowBucket(row studioprojection.WorkflowRow, delta int) {
	if row.Kind != workflowRowBucket {
		return
	}
	if err := m.ensureStudioDraft(); err != nil {
		m.studioWorkflowMsg = err.Error()
		return
	}
	report := m.studioDraft.MoveBucket(row.Bucket.ID, delta)
	m.studioWorkflowMsg = studioDraftReportMessage(report, m.tr("tui.studio.workflow.msg.bucket_moved", "bucket order changed"))
	m.refreshProjection()
}

func (m *Screen) cycleWorkflowBucketKey(row studioprojection.WorkflowRow) {
	if row.Kind != workflowRowBucket {
		return
	}
	if err := m.ensureStudioDraft(); err != nil {
		m.studioWorkflowMsg = err.Error()
		return
	}
	key := row.Bucket.Key
	if strings.HasSuffix(key, "-x") {
		key = strings.TrimSuffix(key, "-x")
	} else {
		key = key + "-x"
	}
	report := m.studioDraft.ChangeBucketKey(row.Bucket.ID, key)
	m.studioWorkflowMsg = studioDraftReportMessage(report, m.tr("tui.studio.workflow.msg.bucket_key_changed", "bucket key changed"))
	m.refreshProjection()
}

func (m *Screen) toggleWorkflowPermission(row studioprojection.WorkflowRow, entity bundledraft.BucketPermissionEntity, op bundledraft.BucketPermissionOp) {
	if row.Kind != workflowRowBucket {
		return
	}
	if err := m.ensureStudioDraft(); err != nil {
		m.studioWorkflowMsg = err.Error()
		return
	}
	workflow := m.studioWorkflowCandidate()
	allowed, _ := workflowPermissionAllowed(row.Bucket, workflow.Defaults, entity, op)
	report := m.studioDraft.SetBucketPermission(row.Bucket.ID, entity, op, !allowed)
	m.studioWorkflowMsg = studioDraftReportMessage(report, m.tr("tui.studio.workflow.msg.permission_changed", "bucket permission changed"))
	m.refreshProjection()
}

func workflowAddTarget(row studioprojection.WorkflowRow, workflow config.Workflow) (fromID, toID int, ok bool) {
	buckets := bundledraft.OrderedBuckets(workflow.Buckets)
	if len(buckets) < 2 {
		return 0, 0, false
	}
	from := row.Bucket
	if row.Kind == workflowRowAdd || from.ID == 0 {
		missingFrom, missingTo, found := firstMissingTransition(workflow, buckets)
		return missingFrom, missingTo, found
	}
	existing := bundledraft.TransitionSet(workflow.Transitions)
	for _, dest := range buckets {
		if dest.ID == from.ID {
			continue
		}
		if _, present := existing[bundledraft.TransitionKey(config.Transition{From: from.ID, To: dest.ID})]; !present {
			return from.ID, dest.ID, true
		}
	}
	return 0, 0, false
}

func firstMissingTransition(workflow config.Workflow, buckets []config.Bucket) (fromID, toID int, ok bool) {
	return studioprojection.FirstMissingTransition(workflow, buckets)
}

func studioWorkflowRows(workflow config.Workflow, counts map[string]int, text bundledraft.Text) []studioprojection.WorkflowRow {
	rows := studioprojection.WorkflowRows(workflow, counts, projectionText(text))
	for i := range rows {
		rows[i].Left = screenkit.Sanitize(rows[i].Left)
		rows[i].Right = screenkit.Sanitize(rows[i].Right)
	}
	return rows
}

// WorkflowIndexFor returns the list index of the first row matching pred, or 0.
func WorkflowIndexFor(workflow config.Workflow, pred func(studioprojection.WorkflowRow) bool) int {
	for i, row := range studioWorkflowRows(workflow, nil, nil) {
		if pred(row) {
			return i
		}
	}
	return 0
}

// paintWorkflowListRow applies theme tokens to one workflow list line.
// comments_tagged and blockers_in use Warning; open transitions use Success;
// muted structural rows use Hint. The selected row is left unstyled so
// selectlist's Cursor (primary) owns the focus paint.
func paintWorkflowListRow(s screenkit.Styles, row studioprojection.WorkflowRow) selectlist.Row {
	left, right := row.Left, row.Right
	switch row.Kind {
	case workflowRowOpen:
		left, right = s.Success.Render(left), s.Success.Render(right)
	case workflowRowGuard:
		tone := s.Hint
		if row.Guard.Type == "comments_tagged" || row.Guard.Type == "blockers_in" {
			tone = s.Warning
		}
		left, right = tone.Render(left), tone.Render(right)
	case workflowRowAdd, workflowRowOp:
		left, right = s.Hint.Render(left), s.Hint.Render(right)
	case workflowRowBucket:
		if row.Mute {
			left = s.Hint.Render(left)
		}
	}
	if row.Right == "" {
		right = ""
	}
	return selectlist.Row{Left: left, Right: right}
}

func workflowPermissionLabel(text bundledraft.Text, bucket config.Bucket, defaults *config.WorkflowDefaults, entity bundledraft.BucketPermissionEntity, op bundledraft.BucketPermissionOp) string {
	allowed, explicit := workflowPermissionAllowed(bucket, defaults, entity, op)
	return permissionLabel(text, allowed, explicit)
}

func workflowPermissionAllowed(bucket config.Bucket, defaults *config.WorkflowDefaults, entity bundledraft.BucketPermissionEntity, op bundledraft.BucketPermissionOp) (allowed, explicit bool) {
	return studioprojection.WorkflowPermissionAllowed(bucket, defaults, string(entity), string(op))
}
