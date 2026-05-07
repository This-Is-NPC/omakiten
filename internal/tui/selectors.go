package tui

import (
	"sort"

	"omakiten/internal/domain"
)

// selectedTask returns the task currently driven by the navigation cursor.
// In task-screen modes, the open task wins regardless of which view sits
// behind. On the board, it's the card at (colIdx, cardIdx); on the table,
// the row at m.selected. Returns false when no selection exists.
func (m Model) selectedTask() (domain.Task, bool) {
	if m.taskScreen != taskScreenClosed && m.taskID > 0 {
		return m.taskByID(m.taskID)
	}
	if m.top == topTasks && m.sub == subBoard {
		if len(m.workflow.Buckets) == 0 || m.colIdx >= len(m.workflow.Buckets) {
			return domain.Task{}, false
		}
		bucketTasks := m.tasksInCurrentBucket()
		if m.cardIdx < 0 || m.cardIdx >= len(bucketTasks) {
			return domain.Task{}, false
		}
		return bucketTasks[m.cardIdx], true
	}
	if m.selected < 0 || m.selected >= len(m.tasks) {
		return domain.Task{}, false
	}
	return m.tasks[m.selected], true
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
// math (tasksInCurrentBucket reads from this).
func (m Model) tasksByBucket() map[string][]domain.Task {
	tasksByBucket := map[string][]domain.Task{}
	allowed := priorityAllowSet(m.views.Board.Filter.Priority)
	for _, task := range m.tasks {
		if !priorityAllowed(allowed, task.Priority) {
			continue
		}
		tasksByBucket[task.BucketKey] = append(tasksByBucket[task.BucketKey], task)
	}
	return tasksByBucket
}

func (m Model) tasksInCurrentBucket() []domain.Task {
	if len(m.workflow.Buckets) == 0 || m.colIdx >= len(m.workflow.Buckets) {
		return nil
	}
	return m.tasksByBucket()[m.workflow.Buckets[m.colIdx].Key]
}

// syncSelectedFromBoard mirrors the board cursor (colIdx, cardIdx) into
// m.selected (the table cursor) so swapping views preserves the sense of
// "which task is in focus" even though the two views index differently.
func (m *Model) syncSelectedFromBoard() {
	task, ok := m.selectedTask()
	if !ok {
		return
	}
	for i, candidate := range m.tasks {
		if candidate.ID == task.ID {
			m.selected = i
			return
		}
	}
}

// selectTaskByID positions every cursor (board col/card, table row) onto
// the given task id. Returns false when the id no longer exists in the
// loaded slice — caller can fall back to a default selection.
func (m *Model) selectTaskByID(taskID int64) bool {
	for i, task := range m.tasks {
		if task.ID != taskID {
			continue
		}

		m.selected = i
		for colIdx, bucket := range m.workflow.Buckets {
			if bucket.Key != task.BucketKey {
				continue
			}

			cardIdx := 0
			for _, candidate := range m.tasks {
				if candidate.BucketKey != task.BucketKey {
					continue
				}
				if candidate.ID == taskID {
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

func (m *Model) clampSelection() {
	if m.selected >= len(m.tasks) {
		m.selected = len(m.tasks) - 1
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
