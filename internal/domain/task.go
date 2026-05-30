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
	// Depth is the materialised distance from the nearest root ancestor.
	// 0 for root rows, 1 for direct children, 2 for grandchildren, and so
	// on. Persisted via the `tasks.depth` column (migration 028) so event
	// payloads can carry the real value without paying for a recursive
	// parent-walk on every emission — see #297 review finding §B.5 and
	// the implementation in #299.
	Depth int `json:"depth,omitempty"`
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

// CommentScope names which entity a comment hangs off. It is derived from the
// events.entity_type column: a task comment carries the task id in entity_id, a
// project comment the project id, and a universal comment has no entity_id and
// no project_id. The three values mirror the EventEntity* constants reused as
// the events.entity_type for comment rows.
const (
	CommentScopeTask      = "task"
	CommentScopeProject   = "project"
	CommentScopeUniversal = "universal"
)

type Comment struct {
	ID        int64 `json:"id"`
	ProjectID int64 `json:"project_id"`
	TaskID    int64 `json:"task_id"`
	// Scope is derived from events.entity_type (task|project|universal).
	Scope      string `json:"scope,omitempty"`
	Body       string `json:"body"`
	Title      string `json:"title,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Pinned     bool   `json:"pinned,omitempty"`
	AuthorType string `json:"author_type"`
	CreatedAt  string `json:"created_at,omitempty"`
	UpdatedAt  string `json:"updated_at,omitempty"`
	Tags       []Tag  `json:"tags,omitempty"`
}

// CommentWrite is the scope-aware payload for creating a comment. Scope selects
// the events.entity_type and how ProjectID/TaskID map onto entity_id/project_id:
//
//   - task:      entity_type='task',      entity_id=TaskID,    project_id=ProjectID
//   - project:   entity_type='project',   entity_id=ProjectID, project_id=ProjectID
//   - universal: entity_type='universal', entity_id=NULL,      project_id=NULL
type CommentWrite struct {
	Scope      string
	ProjectID  int64
	TaskID     int64
	Body       string
	Title      string
	Kind       string
	Pinned     bool
	AuthorType string
	Tags       []Tag
}

// CommentEdit is the scope-agnostic patch applied to an existing comment.
// Body/Title/Kind/Pinned are tri-state pointers — a nil pointer leaves the
// stored column untouched, a non-nil pointer overwrites it. This prevents a
// metadata-only edit from silently wiping the body (and vice versa). A non-nil
// Body must be non-empty: you can overwrite a body but not blank it. Mirrors
// how EditPlanInput/UpdatePlan handle partial updates.
type CommentEdit struct {
	Body   *string
	Title  *string
	Kind   *string
	Pinned *bool
	Tags   []Tag
}

// CommentFilter narrows the cross-cutting comment query surface (the
// filterable handoff log). All fields are optional and AND together. A zero
// filter lists every comment the projection allows.
type CommentFilter struct {
	// CommentID narrows to a single comment by its id (events.id) when > 0.
	// Used by the get-by-id read path (okt-note-show) so the agent can fetch
	// exactly one comment through the filterable query surface.
	CommentID int64
	// Scope restricts to task|project|universal when non-empty.
	Scope string
	// ProjectID scopes to a single project. 0 means cross-project. Universal
	// comments (project_id NULL) only match when ProjectID is 0.
	ProjectID int64
	// TaskID further narrows task-scoped rows to a single task.
	TaskID int64
	// Kind restricts to a single comment kind when non-empty.
	Kind string
	// Tag restricts to comments carrying the named tag when non-empty.
	Tag string
	// PinnedOnly returns only pinned comments (the cover sheet).
	PinnedOnly bool
	// Search is an FTS5 MATCH expression run against body+title when non-empty.
	Search string
	// CreatedAfter / CreatedBefore bound created_at (RFC3339/SQLite datetime
	// strings) when non-empty — the time-window slice.
	CreatedAfter  string
	CreatedBefore string
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
