package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
)

// Priority is the id of a configured priority entry. The human label
// (e.g. "low", "high") and the optional color are not part of the
// domain — they live in `config.priorities` and are resolved at the
// rendering boundary via the process-global priority registry below.
//
// JSON marshaling emits the resolved label string (backwards-compat
// with consumers that match `priority == "high"`); JSON unmarshaling
// accepts either an integer id or a label string. When the registry
// has not been wired the marshaler emits the raw integer so partially
// initialised tests still serialize unambiguously.
type Priority int

// PriorityZero is the sentinel "no priority" id. Production code should
// always resolve a real id from the active priorities table before
// persisting; this exists so zero-value Task structs are still valid.
const PriorityZero Priority = 0

// priorityRegistry holds the active id↔value mapping plus the configured
// default. Populated by RegisterPriorities at runtime startup from the
// loaded config bundle. Atomic-pointer storage keeps the lookup race-free
// without a per-call mutex; readers grab a snapshot pointer and read from
// a frozen map.
//
// Architectural note (accepted debt): this is process-global state in
// the domain package, which is unusual for a hexagonal codebase that
// otherwise threads dependencies via constructor injection. The trade-
// off is deliberate: stdlib `json.Marshaler` has no per-call context,
// so MarshalJSON has nowhere to receive an injected resolver. Any
// alternative (resolve at the adapter boundary, drop MarshalJSON in
// favor of explicit render hooks at every callsite) costs more in
// boilerplate than the global-state-in-domain rule saves in purity.
// The registry is read-only after Store, the lookup is O(1), and tests
// reset between scenarios via testfixtures helpers — the failure modes
// are bounded.
type priorityRegistry struct {
	byID      map[int]string
	byLabel   map[string]int
	defaultID Priority
}

var activePriorities atomic.Pointer[priorityRegistry]

// RegisterPriorities replaces the active priority registry with the
// supplied id↔value pairs. Order doesn't matter for lookup; the
// rendering layer pulls labels by id and the input layer pulls ids by
// label. Exactly one entry should set Default=true (validator-enforced
// at the config layer); when none is flagged, the registry picks the
// middle entry by index so the writer path always has a non-zero
// fallback. Tests reset between scenarios by calling this with the
// canonical kit table or an empty slice.
func RegisterPriorities(pairs []PriorityPair) {
	reg := &priorityRegistry{
		byID:    make(map[int]string, len(pairs)),
		byLabel: make(map[string]int, len(pairs)),
	}
	for _, p := range pairs {
		reg.byID[p.ID] = p.Value
		reg.byLabel[p.Value] = p.ID
		if p.Default {
			reg.defaultID = Priority(p.ID)
		}
	}
	if reg.defaultID == PriorityZero && len(pairs) > 0 {
		reg.defaultID = Priority(pairs[len(pairs)/2].ID)
	}
	activePriorities.Store(reg)
}

// PriorityPair is the wire shape RegisterPriorities accepts — duplicates
// just enough of config.PriorityDefinition to keep the domain layer free
// of an internal/config import. Default flags the entry that writers
// substitute when the user creates a task without naming a priority;
// validator at the config layer rejects more than one entry with the
// flag set.
type PriorityPair struct {
	ID      int
	Value   string
	Default bool
}

// DefaultPriority returns the priority id flagged `default: true` in
// the active registry, falling back to the middle entry's id when no
// entry is flagged. PriorityZero when the registry has not been wired —
// callers should treat that as "let the storage layer pick" so
// uninitialised tests still write rows.
//
// Deprecated: Use EnumRegistry.DefaultPriority instead.
func DefaultPriority() Priority {
	if reg := activePriorities.Load(); reg != nil {
		return reg.defaultID
	}
	return PriorityZero
}

// Label returns the configured label for this priority id, or "" when
// the registry is empty or the id is unknown. Callers that need a
// fallback string should branch on Label() == "".
//
// Deprecated: Use EnumRegistry.PriorityLabel instead. This method relies
// on process-global state and will be removed in a future version.
func (p Priority) Label() string {
	if reg := activePriorities.Load(); reg != nil {
		return reg.byID[int(p)]
	}
	return ""
}

// String returns Label when known, otherwise the numeric id as a
// fallback so log lines and error messages never read empty.
func (p Priority) String() string {
	if label := p.Label(); label != "" {
		return label
	}
	return strconv.Itoa(int(p))
}

// MarshalJSON renders the priority as its label string when registered,
// preserving the historical wire format ("low" / "normal" / "high").
// When no registry is wired (tests, partially-initialised runtimes)
// falls back to the numeric id so consumers still get unambiguous data.
func (p Priority) MarshalJSON() ([]byte, error) {
	if label := p.Label(); label != "" {
		return json.Marshal(label)
	}
	return json.Marshal(int(p))
}

// UnmarshalJSON accepts either an integer id or a string label. Strings
// are resolved against the active registry; unknown labels error out so
// typos surface immediately instead of silently landing as id 0. When
// the registry is uninitialised (test fixture forgot to call
// RegisterPriorities) the error message names that explicit cause so
// the failure points the writer at the wiring problem, not a typo.
func (p *Priority) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		*p = PriorityZero
		return nil
	}
	if data[0] == '"' {
		var label string
		if err := json.Unmarshal(data, &label); err != nil {
			return err
		}
		if label == "" {
			*p = PriorityZero
			return nil
		}
		reg := activePriorities.Load()
		if reg == nil || len(reg.byLabel) == 0 {
			return fmt.Errorf("priority registry not initialised; call RegisterPriorities first (received label %q)", label)
		}
		if id, ok := reg.byLabel[label]; ok {
			*p = Priority(id)
			return nil
		}
		return fmt.Errorf("unknown priority label %q (must match a value in config.priorities)", label)
	}
	var id int
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}
	*p = Priority(id)
	return nil
}

// PriorityFromLabel looks up a priority id by its configured label.
// Returns PriorityZero, false when the registry is empty or the label
// is not configured. Used by CLI/MCP boundary layers to translate
// user-supplied strings into ids before crossing the domain boundary.
//
// Deprecated: Use EnumRegistry.PriorityFromLabel instead.
func PriorityFromLabel(label string) (Priority, bool) {
	if label == "" {
		return PriorityZero, false
	}
	if reg := activePriorities.Load(); reg != nil {
		if id, ok := reg.byLabel[label]; ok {
			return Priority(id), true
		}
	}
	return PriorityZero, false
}

// IsRegistered reports whether the given id corresponds to an entry in
// the active priority table. Validator and app services use this to
// reject IDs that refer to deleted priority entries.
//
// Deprecated: Use EnumRegistry.IsPriorityRegistered instead.
func (p Priority) IsRegistered() bool {
	if reg := activePriorities.Load(); reg != nil {
		_, ok := reg.byID[int(p)]
		return ok
	}
	return false
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
}

// TaskSort drives the ORDER BY clause applied by ListTasks. Field is one of
// "id", "title", "priority", "created_at"; Order is "asc" or "desc". An empty
// Field falls back to id ascending — the legacy default.
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
}

type TaskUpdate struct {
	Title       *string
	Description *string
	Priority    *Priority
	BucketKey   string
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
