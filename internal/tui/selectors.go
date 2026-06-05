package tui

import (
	"sort"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

// priorityByID returns the configured priority definition for the given
// id, or false when the id is zero / not in the active table. Centralised
// here so renderers (board/table/comment/badge) and the form cycle all
// agree on the same lookup path — and so swapping config.priorities at
// runtime takes effect uniformly.
func (m Model) priorityByID(id domain.Priority) (config.PriorityDefinition, bool) {
	if id == domain.PriorityZero {
		return config.PriorityDefinition{}, false
	}
	for _, p := range m.priorities {
		if domain.Priority(p.ID) == id {
			return p, true
		}
	}
	return config.PriorityDefinition{}, false
}

// priorityLabel returns the human label for a priority id. Empty when
// the id is zero or not in the model's priority table.
func (m Model) priorityLabel(id domain.Priority) string {
	if def, ok := m.priorityByID(id); ok {
		return def.Value
	}
	return ""
}

// priorityBadge renders the colored pill badge for a priority id. Color
// comes from config.priorities[].color (mapped via styles.badgeForColor);
// label is uppercased for visual weight. Empty when the id is zero or
// unknown so callers can drop the badge entirely instead of rendering an
// empty pill.
func (m Model) priorityBadge(id domain.Priority) string {
	def, ok := m.priorityByID(id)
	if !ok {
		return ""
	}
	return m.styles.badgeForColor(def.Color).Render(strings.ToUpper(def.Value))
}

// severityByID / severityLabel / severityBadge mirror the priority
// helpers for the law-severity table. Same lookup semantics: TUI-side
// state-driven cache (m.severities) keeps renderers in sync with the
// active config without per-call store access.
func (m Model) severityByID(id domain.Severity) (config.SeverityDefinition, bool) {
	if id == domain.SeverityZero {
		return config.SeverityDefinition{}, false
	}
	for _, s := range m.severities {
		if domain.Severity(s.ID) == id {
			return s, true
		}
	}
	return config.SeverityDefinition{}, false
}

func (m Model) severityLabel(id domain.Severity) string {
	if def, ok := m.severityByID(id); ok {
		return def.Value
	}
	return ""
}

func (m Model) severityBadge(id domain.Severity) string {
	def, ok := m.severityByID(id)
	if !ok {
		return ""
	}
	return m.styles.badgeForColor(def.Color).Render(strings.ToUpper(def.Value))
}

// checkBucketPermission asks the workflow service whether (entity, op) is
// allowed in the bucket the given task currently sits in. Used by the TUI
// entry points (e/d shortcuts) to surface the policy hint at the moment
// the user presses the action button — much clearer than letting them
// type a whole edit and only failing at save time. Returns the hint
// string when the answer is "no" so the caller can drop it straight into
// the status badge.
func (m Model) checkBucketPermission(taskID int64, entity, op string) (bool, string) {
	if m.repos.Workflow == nil {
		return true, ""
	}
	allowed, hint, err := m.repos.Workflow.ResolveBucketPermissions(m.ctx, m.project, taskID, entity, op)
	if err != nil {
		return false, err.Error()
	}
	return allowed, hint
}

// canEditTask / canDeleteTask / canEditComment / canDeleteComment are
// thin wrappers that name the (entity, op) tuple at the call site so the
// e/d handlers read closer to English. Each returns (allowed, hint).
func (m Model) canEditTask(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, app.EntityTask, app.PermissionEdit)
}

func (m Model) canDeleteTask(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, app.EntityTask, app.PermissionDelete)
}

func (m Model) canEditComment(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, app.EntityComment, app.PermissionEdit)
}

func (m Model) canDeleteComment(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, app.EntityComment, app.PermissionDelete)
}

// selectedTask returns the task currently driven by the navigation cursor.
// In task-screen modes, the open task wins regardless of which view sits
// behind. On the board, it's the card at (colIdx, cardIdx); on the table,
// the row at m.selected within the visible (filtered/sorted) projection.
// Returns false when no selection exists.
func (m Model) selectedTask() (domain.Task, bool) {
	if m.taskScreen != taskScreenClosed && m.taskID > 0 {
		return m.taskByID(m.taskID)
	}
	if m.top == topTasks && m.sub == subBoard {
		if len(m.workflow.Buckets) == 0 || m.colIdx < 0 || m.colIdx >= len(m.workflow.Buckets) {
			return domain.Task{}, false
		}
		bucketTasks := m.tasksInCurrentBucket()
		if m.cardIdx < 0 || m.cardIdx >= len(bucketTasks) {
			return domain.Task{}, false
		}
		return bucketTasks[m.cardIdx], true
	}
	// Table cursor: m.selected indexes the visible (filtered/sorted)
	// projection, so Enter-open and move target the highlighted row
	// rather than a hidden raw task. See task #594.
	rows := m.tableRows()
	if m.selected < 0 || m.selected >= len(rows) {
		return domain.Task{}, false
	}
	return rows[m.selected], true
}

// activeTask returns the task currently being viewed/edited (m.taskID).
// Used by the task-screen handlers that already know they're in a task
// context — distinct from selectedTask, which falls back to the board/table
// cursor when no task screen is open.
func (m Model) activeTask() (domain.Task, bool) {
	if m.taskID <= 0 {
		return domain.Task{}, false
	}
	return m.taskByID(m.taskID)
}

func (m Model) taskByID(taskID int64) (domain.Task, bool) {
	for _, task := range m.tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.Task{}, false
}

// tasksByBucket groups m.tasks by bucket key after applying the board
// view's priority filter. Used by both the kanban renderer and the cursor
// math (tasksInCurrentBucket reads from this). Sub-tasks (rows with a
// non-nil ParentID) are hidden so the kanban columns stay focused on
// root work — the detail view's sub-tasks panel is the canonical place
// to inspect a parent's children.
func (m Model) tasksByBucket() map[string][]domain.Task {
	if m.cachedTasksByBucket != nil {
		return m.cachedTasksByBucket
	}
	return buildTasksByBucket(m.tasks, priorityAllowSet(m.views.Board.Filter.Priority), m.priorities)
}

// buildTasksByBucket filters m.tasks by the board view's priority allow
// list, drops sub-tasks (kanban columns show roots only), and groups
// by bucket key. Pulled out so the *Model cache populator and the
// value-receiver fallback share the same code path.
func buildTasksByBucket(tasks []domain.Task, allowed map[string]struct{}, priorities []config.PriorityDefinition) map[string][]domain.Task {
	out := map[string][]domain.Task{}
	for _, task := range tasks {
		if task.IsSubTask() {
			continue
		}
		if !priorityAllowedFromTable(allowed, task.Priority, priorities) {
			continue
		}
		out[task.BucketKey] = append(out[task.BucketKey], task)
	}
	return out
}

// priorityAllowedFromTable is the stateless flavour of
// (Model).priorityAllowed used by buildTasksByBucket so the cache
// populator does not need the full Model receiver.
func priorityAllowedFromTable(allowed map[string]struct{}, p domain.Priority, priorities []config.PriorityDefinition) bool {
	if allowed == nil {
		return true
	}
	for _, def := range priorities {
		if domain.Priority(def.ID) == p {
			_, ok := allowed[def.Value]
			return ok
		}
	}
	return false
}

// rebuildBoardCaches recomputes m.cachedTasksByBucket and
// m.cachedTableView from m.tasks + the current view filters. Called
// from refresh() and from any mutation handler that touches m.tasks
// (move, archive, edit) so the next render reads from a coherent
// snapshot.
func (m *Model) rebuildBoardCaches() {
	m.cachedTasksByBucket = buildTasksByBucket(m.tasks, priorityAllowSet(m.views.Board.Filter.Priority), m.priorities)
	m.cachedTableView = buildTableView(m.tasks, m.views.Table, m.priorities)
}

// invalidateBoardCaches drops the memoised board/table projections so
// the next access rebuilds. Cheaper than calling rebuildBoardCaches
// when the next refresh() will rebuild anyway.
func (m *Model) invalidateBoardCaches() {
	m.cachedTasksByBucket = nil
	m.cachedTableView = nil
}

func (m Model) tasksInCurrentBucket() []domain.Task {
	if len(m.workflow.Buckets) == 0 || m.colIdx < 0 || m.colIdx >= len(m.workflow.Buckets) {
		return nil
	}
	return m.tasksByBucket()[m.workflow.Buckets[m.colIdx].Key]
}

// syncSelectedFromBoard mirrors the board cursor (colIdx, cardIdx) into
// m.selected (the table cursor) so swapping views preserves the sense of
// "which task is in focus" even though the two views index differently.
//
// m.selected indexes the visible table projection, so the focused task is
// located within applyTableView() rather than raw m.tasks — that keeps the
// board→table handoff landing on the visible row. When the focused task is
// filtered out of the table the cursor falls back to the first visible row.
// See task #594.
func (m *Model) syncSelectedFromBoard() {
	task, ok := m.selectedTask()
	if !ok {
		return
	}
	for i, candidate := range m.tableRows() {
		if candidate.ID == task.ID {
			m.selected = i
			return
		}
	}
	m.selected = 0
}

// tableRowIndex returns the index of taskID within the visible table
// projection, or 0 when the task is filtered out of the table (so the
// cursor lands on the first visible row rather than a hidden task). The
// table cursor (m.selected) indexes this projection. See task #594.
func (m Model) tableRowIndex(taskID int64) int {
	for i, task := range m.tableRows() {
		if task.ID == taskID {
			return i
		}
	}
	return 0
}

// selectTaskByID positions every cursor (board col/card, table row) onto
// the given task id. Returns false when the id no longer exists in the
// loaded slice — caller can fall back to a default selection.
//
// The table row cursor (m.selected) indexes the visible (filtered/sorted)
// table projection, so it is resolved via tableRowIndex rather than the
// raw m.tasks position — keeping the table cursor on the visible row.
//
// Sub-tasks are not rendered on the board, so when the requested id is a
// child row the board cursor lands on its nearest visible ancestor (the
// root of its subtree) instead. Without this, cardIdx would index into
// the unfiltered m.tasks while the board renders the filtered
// tasksByBucket — the cursor would point past the visible cards and
// every subsequent `j` would chase a phantom row.
func (m *Model) selectTaskByID(taskID int64) bool {
	for _, task := range m.tasks {
		if task.ID != taskID {
			continue
		}

		m.selected = m.tableRowIndex(taskID)
		boardTask := task
		if task.IsSubTask() {
			if root, ok := m.boardAncestor(task); ok {
				boardTask = root
			}
		}
		for colIdx, bucket := range m.workflow.Buckets {
			if bucket.Key != boardTask.BucketKey {
				continue
			}

			cardIdx := 0
			for _, candidate := range m.tasksByBucket()[boardTask.BucketKey] {
				if candidate.ID == boardTask.ID {
					m.colIdx = colIdx
					m.cardIdx = cardIdx
					return true
				}
				cardIdx++
			}
		}
		return true
	}
	return false
}

// boardAncestor walks parent_id up from a sub-task until it hits a row
// the board actually renders (no ParentID). Returns false when the
// chain breaks before reaching a root — defensive against orphan FKs
// that the FK constraint should rule out, but the selector treats as a
// soft miss rather than panicking.
func (m Model) boardAncestor(task domain.Task) (domain.Task, bool) {
	for task.ParentID != nil {
		parent, ok := m.taskByID(*task.ParentID)
		if !ok {
			return domain.Task{}, false
		}
		task = parent
	}
	return task, true
}

func (m *Model) clampSelection() {
	// m.selected indexes the visible table projection, so clamp against
	// the filtered/sorted row count — not raw m.tasks. See task #594.
	rowCount := len(m.tableRows())
	if m.selected >= rowCount {
		m.selected = rowCount - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.colIdx >= len(m.workflow.Buckets) {
		m.colIdx = len(m.workflow.Buckets) - 1
	}
	if m.colIdx < 0 {
		m.colIdx = 0
	}
}

func (m *Model) clampCardIdx() {
	tasks := m.tasksInCurrentBucket()
	if len(tasks) == 0 {
		m.cardIdx = 0
		return
	}
	if m.cardIdx >= len(tasks) {
		m.cardIdx = len(tasks) - 1
	}
	if m.cardIdx < 0 {
		m.cardIdx = 0
	}
}

func (m Model) dependencyCount(taskID int64) int {
	count := 0
	for _, dependency := range m.dependencies {
		if dependency.TaskID == taskID {
			count++
		}
	}
	return count
}

func (m Model) blockersForTask(taskID int64) []domain.Task {
	blockers := make([]domain.Task, 0)
	for _, dependency := range m.dependencies {
		if dependency.TaskID != taskID {
			continue
		}
		if blocker, ok := m.taskByID(dependency.DependsOnTaskID); ok {
			blockers = append(blockers, blocker)
		}
	}
	sort.Slice(blockers, func(i, j int) bool { return blockers[i].ID < blockers[j].ID })
	return blockers
}

func (m Model) commentCount(taskID int64) int {
	return len(m.commentsForTask(taskID))
}

// subtaskCount returns the number of direct children of taskID in the
// loaded model snapshot. Cheap O(n) scan over m.tasks — the typical
// project has at most a few thousand tasks, well below any threshold
// that would justify a per-card index.
func (m Model) subtaskCount(taskID int64) int {
	count := 0
	for _, task := range m.tasks {
		if task.ParentID != nil && *task.ParentID == taskID {
			count++
		}
	}
	return count
}

// directChildren returns the immediate sub-tasks of taskID, sorted by
// id so the detail-view panel renders in stable insertion order. Pure
// in-memory walk over m.tasks — same source the badge count reads.
func (m Model) directChildren(taskID int64) []domain.Task {
	var out []domain.Task
	for _, task := range m.tasks {
		if task.ParentID != nil && *task.ParentID == taskID {
			out = append(out, task)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m Model) tagsForTask(taskID int64) []domain.Tag {
	if m.taskTagsMap == nil {
		return nil
	}
	return m.taskTagsMap[taskID]
}

func (m Model) commentsForTask(taskID int64) []domain.Comment {
	comments := make([]domain.Comment, 0)
	for _, comment := range m.comments {
		if comment.TaskID == taskID {
			comments = append(comments, comment)
		}
	}
	return comments
}
