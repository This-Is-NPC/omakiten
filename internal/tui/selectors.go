package tui

import (
	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/tui/components/tokenstrip"
	"omakiten/internal/tui/screenhost"
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
	return tokenstrip.Pill(m.styles.screenStyles(), def.Color, def.Value)
}

// checkBucketPermission asks the workflow service whether (entity, op) is
// allowed in the bucket the given task currently sits in. Used by the TUI
// entry points (e/d shortcuts) to surface the policy hint at the moment
// the user presses the action button — much clearer than letting them
// type a whole edit and only failing at save time. Returns the hint
// string when the answer is "no" so the caller can drop it straight into
// the status badge.
func (m Model) checkBucketPermission(taskID int64, entity, op string) (bool, string) {
	svc := m.repos.operationService()
	if svc == nil {
		return true, ""
	}
	allowed, hint, err := svc.ResolveBucketPermissions(m.ctx, m.project, taskID, entity, op)
	if err != nil {
		return false, err.Error()
	}
	return allowed, hint
}

// canEditTask / canDeleteTask / canEditComment / canDeleteComment are
// thin wrappers that name the (entity, op) tuple at the call site so the
// e/d handlers read closer to English. Each returns (allowed, hint).
func (m Model) canEditTask(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, "task", domain.CommentOpEdit)
}

func (m Model) canDeleteTask(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, "task", domain.CommentOpDelete)
}

func (m Model) canEditComment(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, "comment", domain.CommentOpEdit)
}

func (m Model) canDeleteComment(taskID int64) (bool, string) {
	return m.checkBucketPermission(taskID, "comment", domain.CommentOpDelete)
}

// selectedTask returns the task currently driven by the active hosted surface.
func (m Model) selectedTask() (domain.Task, bool) {
	if screen, ok := m.activeHostedScreen(); ok {
		if selector, ok := screen.(screenhost.TaskSelector); ok {
			if taskID, ok := selector.SelectedTaskID(); ok {
				return m.taskByID(taskID)
			}
		}
	}
	return domain.Task{}, false
}

func (m Model) taskByID(taskID int64) (domain.Task, bool) {
	for _, task := range m.tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.Task{}, false
}

// selectTaskByID positions every cursor (board col/card, table row) onto
// the given task id. Returns false when the id no longer exists in the
// loaded slice — caller can fall back to a default selection.
//
// Extracted task lenses resolve the id against their own visible projections;
// the root only coordinates the cross-view selection handoff.
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

		m.tableScreen = m.boundTableScreen().SelectTaskID(taskID, m.screenFrame())
		m.graphScreen = m.boundGraphScreen().SelectTaskID(taskID, m.screenFrame())
		boardTask := task
		if task.IsSubTask() {
			if root, ok := m.boardAncestor(task); ok {
				boardTask = root
			}
		}
		m.boardScreen = m.boundBoardScreen().SelectTaskID(boardTask.ID, m.screenFrame())
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
	m.boardScreen = m.boundBoardScreen()
	m.tableScreen = m.boundTableScreen()
	m.graphScreen = m.boundGraphScreen()
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
