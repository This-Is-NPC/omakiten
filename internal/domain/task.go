package domain

// Priority is the id of a configured priority entry. The human label
// (e.g. "low", "high") and the optional color are not part of the
// domain — they live in `config.priorities` and are resolved at the
// rendering boundary via an `EnumRegistry` injected into the service
// that needs the lookup. The domain layer itself stays free of
// process-global registries; JSON marshaling of Priority always emits
// the raw int id, and label resolution is the caller's responsibility
// (typically via a DTO projection at the adapter boundary).
type Priority int

// PriorityZero is the sentinel "no priority" id. Production code should
// always resolve a real id from the active priorities table before
// persisting; this exists so zero-value Task structs are still valid.
const PriorityZero Priority = 0

// priorityRegistry holds the active id↔value mapping plus the configured
// default. Used internally by EnumRegistry; not exported.
type priorityRegistry struct {
	byID      map[int]string
	byLabel   map[string]int
	defaultID Priority
}

// PriorityPair is the wire shape EnumRegistry consumes — duplicates just
// enough of config.PriorityDefinition to keep the domain layer free of an
// internal/config import. Default flags the entry that writers substitute
// when the user creates a task without naming a priority; the config-layer
// validator rejects more than one entry with the flag set.
type PriorityPair struct {
	ID      int
	Value   string
	Default bool
}

// TaskState toggles a task between the active workflow and the archived
// escape-hatch lane. Archive bypasses bucket-policy and transition guards but
// still respects the declared operations.archive.guards.
type TaskState string

const (
	TaskStateActive   TaskState = "active"
	TaskStateArchived TaskState = "archived"
)

type Task struct {
	ID          int64     `json:"id"`
	ProjectID   int64     `json:"project_id"`
	BucketID    int64     `json:"bucket_id,omitempty"`
	BucketKey   string    `json:"bucket_key,omitempty"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Priority    Priority  `json:"priority"`
	State       TaskState `json:"state,omitempty"`
	CreatedAt   string    `json:"created_at,omitempty"`
	// ParentID points at the task this row is a sub-task of, or nil for
	// root tasks. The board hides non-roots; transitions still flow
	// through the same workflow as the parent, but the subtasks_complete
	// guard reads this column to gate parent promotion on child status.
	ParentID *int64 `json:"parent_id,omitempty"`
}

// IsSubTask reports whether the task is attached to a parent. Root tasks
// (ParentID nil) return false; any non-nil pointer — including a pointer
// to zero, which the DB does not produce — returns true.
func (t Task) IsSubTask() bool {
	return t.ParentID != nil
}

// ParentIDEquals compares two parent FK pointers by value, treating nil
// as "root". Returns true when both pointers are nil or both non-nil and
// point at the same id. Used by re-parent flows to short-circuit no-op
// edits before they bump updated_at.
func ParentIDEquals(a, b *int64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// TaskSort drives the ORDER BY clause applied by ListTasks. Field is one of
// "id", "title", "priority", "created_at"; Order is "asc" or "desc". An empty
// Field falls back to id ascending — the default ordering for unsorted lists.
type TaskSort struct {
	Field string
	Order string
}

type TaskFilter struct {
	BucketKey  string
	BucketKeys []string
	Priorities []Priority
	Sort       TaskSort
	// IncludeArchived flips the default active-only filter so callers can opt
	// into seeing archived tasks. Defaults to false: every list view (board,
	// table, graph, logs, MCP) hides archived rows unless the toggle is on.
	IncludeArchived bool
	// ParentMode scopes the list by tasks.parent_id. The zero value
	// disables the filter (every task surfaces). Board callers set
	// ParentRoots to hide sub-tasks; the detail-view sub-tasks panel
	// sets ParentChildren with ParentValue=parent.ID.
	ParentMode  ParentFilterMode
	ParentValue int64
}

// ParentFilterMode selects how TaskFilter restricts on tasks.parent_id.
// Defined as a named uint8 so the zero value (ParentAny) preserves the
// pre-sub-task behaviour of every existing caller that doesn't touch
// the new field.
type ParentFilterMode uint8

const (
	// ParentAny applies no parent_id filter (zero value).
	ParentAny ParentFilterMode = iota
	// ParentRoots restricts to tasks.parent_id IS NULL — used by the
	// board so sub-tasks don't pollute the kanban columns.
	ParentRoots
	// ParentChildren restricts to tasks.parent_id = ParentValue — used
	// by the detail-view sub-tasks panel and any direct-children read.
	ParentChildren
)

type TaskUpdate struct {
	Title       *string
	Description *string
	Priority    *Priority
	BucketKey   string
	// ChangeParent flags this update as a re-parent. When true,
	// NewParentID names the desired value: nil clears to root and a
	// non-nil value sets the FK. The flag distinguishes "don't touch
	// parent_id" (the zero value) from "set parent_id to nil".
	ChangeParent bool
	NewParentID  *int64
}

type Comment struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"project_id"`
	TaskID     int64  `json:"task_id"`
	Body       string `json:"body"`
	AuthorType string `json:"author_type"`
	CreatedAt  string `json:"created_at,omitempty"`
	Tags       []Tag  `json:"tags,omitempty"`
}

type TaskDependency struct {
	ProjectID       int64 `json:"project_id"`
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

// TaskBlocker is a denormalized view of a single task this task depends on,
// used by the blockers_in workflow guard to report which blockers still sit
// in disallowed buckets. BucketKey is "" when the blocker has no active
// bucket (legacy data only).
type TaskBlocker struct {
	TaskID    int64
	Title     string
	BucketKey string
}
