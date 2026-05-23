package app

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type ProjectRepository interface {
	UpsertProject(ctx context.Context, name, slug, rootPath string) (domain.Project, error)
	FindProjectByID(ctx context.Context, id int64) (domain.Project, error)
	FindProjectBySlug(ctx context.Context, slug string) (domain.Project, error)
	FindProjectsContainingPath(ctx context.Context, path string) ([]domain.Project, error)
	// ListProjects returns every non-archived project ordered by name.
	// Used by the TUI Home view to render the multi-project picker.
	ListProjects(ctx context.Context) ([]domain.Project, error)
	// ProjectDeleteCounts resolves the per-table row count snapshot
	// rendered to the user before a destructive Delete (counters
	// surface in the CLI prompt + TUI confirmation overlay). Counters
	// are point-in-time; concurrent writes between this read and the
	// subsequent DeleteProject call are accepted — the reported
	// numbers are an estimate, not a contract.
	ProjectDeleteCounts(ctx context.Context, projectID int64) (domain.ProjectDeleteCounters, error)
	// DeleteProject hard-deletes a project row and every cascading
	// dependent row (tasks → task_tags / task_dependencies, plans →
	// plan_waves, errors → solutions / error_tags, project_tags,
	// context_entries) in a single transaction. Project-scoped event
	// rows (activity log, comments, task system events with
	// project_id set) are explicitly removed because events has no FK
	// to projects — leaving them would orphan the activity feed.
	DeleteProject(ctx context.Context, projectID int64) error
}

// BackupRunner is the narrow port ProjectService uses to capture a
// pre-delete snapshot. *app.BackupService satisfies it; tests pass a
// fake that records the call or returns a pinned error to exercise
// the "backup failure aborts delete" invariant.
type BackupRunner interface {
	Run(ctx context.Context) (string, error)
}

// Checkpointer is the narrow port destructive flows invoke right
// before BackupService.Run so the on-disk .db file reflects every
// committed WAL frame from this process. *sqlite.Store satisfies it
// via Checkpoint(ctx). Optional collaborator — ProjectService skips
// the checkpoint when nil, matching the standalone `okt db backup`
// flow that has no live store handle to checkpoint.
type Checkpointer interface {
	Checkpoint(ctx context.Context) error
}

// EventRecorder is the narrow port ProjectService uses to emit the
// project.removed audit event after a successful delete. *sqlite.Store
// satisfies it via RecordEntityEvent.
type EventRecorder interface {
	RecordEntityEvent(ctx context.Context, entityType string, entityID int64, projectID int64, eventType string, payload string) error
}

// SnapshotSource exposes the active per-project *config.Snapshot. The
// Phase 2-bis app services capture this pointer at construction time
// and read every config knob through it; the pointer is stable for the
// service's lifetime. *sqlite.Store satisfies it transitionally by
// projecting its in-memory providers through config.BuildSnapshot;
// agentruntime.ProjectRuntime will become the canonical implementor
// once the Store stops carrying config.
type SnapshotSource interface {
	Snapshot() *config.Snapshot
}

// TaskRepository persists task rows. The methods are deliberately policy-free:
// CreateTask requires a non-empty bucket key (default-bucket selection lives in
// app.WorkflowService) and MoveTask is a pure persist + task.moved emission
// (transition allowed?, guards, and task.completed-on-final live in
// app.WorkflowService too). Every method that needs to translate
// bucket key↔id reads through a caller-supplied domain.BucketResolver
// so the adapter never imports the config package.
type TaskRepository interface {
	CreateTask(ctx context.Context, projectID int64, title, description string, priority domain.Priority, bucketKey string, parentID *int64, buckets domain.BucketResolver) (domain.Task, error)
	ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter, buckets domain.BucketResolver) ([]domain.Task, error)
	MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string, buckets domain.BucketResolver) (domain.Task, error)
	UpdateTask(ctx context.Context, projectID, taskID int64, update domain.TaskUpdate, buckets domain.BucketResolver) (domain.Task, error)
	TaskCount(ctx context.Context, projectID int64) (int64, error)
	// HardDeleteTask removes a task and its dependent rows (events, event_tags,
	// task_dependencies, task_tags) and emits a task.removed system event with
	// a snapshot payload. Bucket policy/operation guards are enforced at the
	// service layer; the repository performs the cascade unconditionally.
	HardDeleteTask(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (domain.Event, error)
	// SetTaskState flips active|archived. When targetBucketKey is non-empty
	// the row also moves into that bucket atomically (used by archive). Emits
	// task.archived / task.unarchived in the same transaction.
	SetTaskState(ctx context.Context, projectID, taskID int64, state domain.TaskState, targetBucketKey string, buckets domain.BucketResolver) (domain.Task, domain.Event, error)
	// EmitTaskEditedEvent records a task.edited row with a payload describing
	// the changed fields. Service layer calls it after a successful UpdateTask.
	EmitTaskEditedEvent(ctx context.Context, projectID, taskID int64, before, after domain.Task) (domain.Event, error)
	// AssignTask sets tasks.assigned_to to the trimmed value (empty string
	// clears the column to NULL) and emits task.assigned / task.unassigned
	// in the same transaction. No-ops when the new assignee equals the
	// current one (no event emitted). source labels the call site
	// ("cli.assign", "plans.claim_next", etc.) so consumers can attribute
	// the change in the payload.
	AssignTask(ctx context.Context, projectID, taskID int64, assignee, source string, buckets domain.BucketResolver) (domain.Task, domain.Event, error)
	// SetTaskParent updates tasks.parent_id. parentID nil clears the
	// column (the task becomes a root); a non-nil pointer sets the FK.
	// The repository rejects self-parent inserts but does not walk the
	// tree — anti-cycle is the service-layer caller's responsibility
	// via IsDescendantOf.
	SetTaskParent(ctx context.Context, projectID, taskID int64, parentID *int64) error
	// IsDescendantOf reports whether candidateID has ancestorID in its
	// parent chain. Used by re-parent flows to reject moves that would
	// create a cycle (T.parent = P is unsafe iff P descends from T).
	IsDescendantOf(ctx context.Context, projectID, candidateID, ancestorID int64) (bool, error)
	// ListDirectChildren returns the immediate sub-tasks of parentID,
	// ordered by id. Detail-view sub-tasks panel renders this list; the
	// guard rule reaches for FirstChildNotInBucket instead.
	ListDirectChildren(ctx context.Context, projectID, parentID int64, buckets domain.BucketResolver) ([]domain.Task, error)
	// CountDirectChildren is the cheap variant for board badge slots.
	CountDirectChildren(ctx context.Context, projectID, parentID int64) (int, error)
	// CountDescendants walks the whole subtree — used by the cascade
	// delete confirmation prompt to surface the total row count.
	CountDescendants(ctx context.Context, projectID, parentID int64) (int, error)
}

// WorkflowRepository exposes the state-side primitives the app's
// WorkflowService composes into the move/create policy. Every workflow
// shape read (bucket-by-key, transition-allowed, guards, is-final) now
// flows through the per-project *config.Snapshot the service captures
// at construction — the SQL adapter only answers state questions, and
// resolves bucket key↔id through the caller-supplied BucketResolver.
type WorkflowRepository interface {
	CurrentTaskBucket(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (int64, string, error)
	// TaskState returns the active|archived flag for a task. Used by MoveTask
	// to reject moves on archived rows and by the guards engine to skip
	// transition-guard evaluation when the task sits in the archived lane.
	TaskState(ctx context.Context, projectID, taskID int64) (domain.TaskState, error)
}

// GuardEvaluationRepository exposes the read-only counts the workflow guards
// need. Split from WorkflowRepository so guard evaluation can be stubbed
// independently in tests.
type GuardEvaluationRepository interface {
	ListTaskBlockerBuckets(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) ([]domain.TaskBlocker, error)
	CountTaskComments(ctx context.Context, projectID, taskID int64) (int, error)
	CountTaskCommentsTagged(ctx context.Context, projectID, taskID int64, tagName string) (int, error)
	CountPriorWavesPending(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (int, error)
	// FirstChildNotInBucket gates the subtasks_complete guard. The
	// boolean reports whether a direct child still sits outside
	// finalBucketID; the returned task names the first offender for
	// the human-readable hint.
	FirstChildNotInBucket(ctx context.Context, projectID, parentID, finalBucketID int64, buckets domain.BucketResolver) (domain.Task, bool, error)
}

type CommentRepository interface {
	AddComment(ctx context.Context, projectID, taskID int64, body, authorType string, tags []domain.Tag) (domain.Comment, error)
	ListComments(ctx context.Context, projectID, taskID int64) ([]domain.Comment, error)
	UpdateComment(ctx context.Context, projectID, commentID int64, body string, tags []domain.Tag) (domain.Comment, domain.Event, error)
	DeleteComment(ctx context.Context, projectID, commentID int64) (domain.Event, error)
	CommentByID(ctx context.Context, projectID, commentID int64) (domain.Comment, error)
}

// EventRepository exposes the unified events log. Both the activity feed
// (per-task) and the system event recorders write through this interface,
// so the service layer never has to know the underlying table layout.
type EventRepository interface {
	RecordTaskEvent(ctx context.Context, projectID, taskID int64, eventType, body, payload string) (domain.Event, error)
	// RecordEntityEvent persists a domain event with the agent attribution
	// carried in ctx. Used by workflow/guard emission paths and by the
	// ErrorService domain events. entityID may be 0 for events whose
	// subject is the project as a whole.
	RecordEntityEvent(ctx context.Context, entityType string, entityID int64, projectID int64, eventType string, payload string) error
	ListTaskActivity(ctx context.Context, projectID, taskID int64, order string) ([]domain.Event, error)
}

type DependencyRepository interface {
	AddTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) (domain.TaskDependency, error)
	RemoveTaskDependency(ctx context.Context, projectID, taskID, dependsOnTaskID int64) error
	ListTaskDependencies(ctx context.Context, projectID, taskID int64) ([]domain.TaskDependency, error)
}

type ContextEntryRepository interface {
	AddContextEntry(ctx context.Context, projectID int64, body string, tokenEstimate int) (domain.ContextEntry, error)
	ListContextEntries(ctx context.Context, projectID int64) ([]domain.ContextEntry, error)
}

type TagRepository interface {
	FindOrCreateTag(ctx context.Context, name, label string) (domain.Tag, error)
	ListAllTags(ctx context.Context) ([]domain.Tag, error)
	RenameTag(ctx context.Context, tagID int64, newLabel string) (domain.Tag, error)
	MergeTags(ctx context.Context, sourceTagID, targetTagID int64) (domain.Tag, error)
	DeleteOrphanTags(ctx context.Context) (int64, error)
	AddTaskTag(ctx context.Context, projectID, taskID, tagID int64) error
	RemoveTaskTag(ctx context.Context, projectID, taskID, tagID int64) error
	ListTaskTags(ctx context.Context, projectID, taskID int64) ([]domain.Tag, error)
	ListTaskTagsByProject(ctx context.Context, projectID int64) (map[int64][]domain.Tag, error)
	AddProjectTag(ctx context.Context, projectID, tagID int64) error
	RemoveProjectTag(ctx context.Context, projectID, tagID int64) error
	ListProjectTags(ctx context.Context, projectID int64) ([]domain.Tag, error)
	AddErrorTag(ctx context.Context, errorID, tagID int64) error
	RemoveErrorTag(ctx context.Context, errorID, tagID int64) error
	ListErrorTags(ctx context.Context, errorID int64) ([]domain.Tag, error)
}

type ErrorRepository interface {
	RecordError(ctx context.Context, projectID int64, description, context string, tags []domain.Tag) (domain.ErrorRecord, error)
	AddSolution(ctx context.Context, errorID int64, description, steps string, taskID *int64) (domain.Solution, error)
	ConfirmSolution(ctx context.Context, solutionID int64, success bool) (domain.Solution, error)
	ListTopSolutions(ctx context.Context, limit int) ([]domain.Solution, error)
	// RecordEntityEvent persists a domain event tied to an entity (error,
	// solution, system) with the agent attribution carried in ctx. ErrorService
	// calls it after each successful canonical write so /metrics.summary can
	// reconstruct a per-model timeline (errors recorded, searches run,
	// solutions added, likes given) without joining against the live tables.
	RecordEntityEvent(ctx context.Context, entityType string, entityID int64, projectID int64, eventType string, payload string) error
}

// MetricsRepository computes per-agent-model aggregations over the unified
// events log. Used by /metrics.summary to benchmark how different AI models
// behave (do they search before recording? do their solutions get liked?).
type MetricsRepository interface {
	AgentMetricsSummary(ctx context.Context, period string, projectID int64) ([]domain.AgentMetrics, string, error)
}

// SearchRepository exposes the unified FTS5 index that spans tasks,
// comments, errors, solutions, and context entries. The adapter
// implements ranking (BM25), snippet rendering, and the implicit
// `tasks.state='active'` filter; the service layer validates input and
// resolves the project filter into a numeric id.
type SearchRepository interface {
	// Search runs the FTS5 MATCH expression against `search_index` and
	// returns hits ordered by descending score. projectID == 0 disables
	// the project filter (cross-project). entityTypes restricts the row
	// set; an empty slice means "all five types".
	Search(ctx context.Context, query string, projectID int64, entityTypes []domain.SearchEntityType, limit int) ([]domain.SearchHit, error)
}

// PlanRepository persists plans, waves, and task↔plan attachment. Plans
// are scoped per project; every method takes projectID and rejects rows
// belonging to a different project via the ErrPlanNotFound /
// ErrPlanWaveNotFound codes instead of leaking data across snapshots.
//
// Only the methods the current slice of MCP wiring needs live here; the
// add-wave / assign-task / claim-next surfaces land alongside their
// respective tool dispatches.
type PlanRepository interface {
	CreatePlan(ctx context.Context, projectID int64, slug, name, goalBody string) (domain.Plan, error)
	GetPlanBySlug(ctx context.Context, projectID int64, slug string) (domain.Plan, error)
	GetPlanByID(ctx context.Context, projectID, planID int64) (domain.Plan, error)
	ListPlans(ctx context.Context, projectID int64) ([]domain.Plan, error)
	UpdatePlanGoalBody(ctx context.Context, projectID, planID int64, goalBody string) (domain.Plan, error)
	AddPlanWave(ctx context.Context, projectID, planID int64, name string, position int) (domain.PlanWave, error)
	ListPlanWaves(ctx context.Context, projectID, planID int64) ([]domain.PlanWave, error)
	ListPlanTasks(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) ([]domain.PlanTaskRow, error)
	AssignTaskToPlan(ctx context.Context, projectID, taskID, planID, waveID int64) error
	// ClaimNextPlanTask atomically picks the next unblocked task in the
	// plan's active wave (lowest-position wave with pending tasks),
	// moves it from the workflow's first bucket into the second
	// ("dev"), and stamps tasks.assigned_to with the caller's
	// _agent_model (resolved from ctx). Returns (task, true) on a
	// successful claim, (zero, false) when no task is claimable, or
	// (zero, false, err) on storage failures. Race safety comes from
	// BEGIN IMMEDIATE on a pinned connection — concurrent claims
	// serialise behind the write lock.
	ClaimNextPlanTask(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) (domain.Task, bool, error)
	// PeekNextClaimable returns the next task plans.claim_next would
	// reserve, without mutating anything. Powers plans.continue so a
	// downstream agent can preview the candidate.
	PeekNextClaimable(ctx context.Context, projectID, planID int64, buckets domain.BucketResolver) (domain.PlanTaskRow, bool, error)
	// ListPlanTaskDependencies returns task→task edges where both
	// endpoints belong to the same plan; powers the network
	// diagram's in-plan arrows.
	ListPlanTaskDependencies(ctx context.Context, projectID, planID int64) ([]domain.TaskDependency, error)
	// MaybeFinalizePlanForTask transitions the task's owning plan to
	// status='done' when every other task in the plan already sits in
	// the workflow's final bucket. No-op when the task has no plan,
	// the plan is already terminal, or pending tasks remain. Returns
	// (true, nil) when the plan was finalised. Called from
	// WorkflowService.MoveTask after a successful move into the
	// final bucket.
	MaybeFinalizePlanForTask(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (bool, error)
}

// PlanFinalizer is the narrow port WorkflowService consults after a task
// moves into the workflow's final bucket: if the task belongs to a plan
// and was the last pending one, the plan transitions to status='done'.
// Defined separately from PlanRepository so the workflow layer does not
// have to drag the full repository surface in just to call one method —
// the production store satisfies both, the type assertion in
// NewWorkflowServiceFromStore wires it up.
type PlanFinalizer interface {
	MaybeFinalizePlanForTask(ctx context.Context, projectID, taskID int64, buckets domain.BucketResolver) (bool, error)
}

// BundleStore is the adapter port for reading/writing the bundled config and
// the generic atomic-write helper. The app layer talks to this instead of
// reaching into `internal/config`'s I/O functions directly so that the
// hexagonal direction stays inward (app → port → adapter → disk).
type BundleStore interface {
	LoadBundle(path string) (config.Bundle, error)
	SaveBundle(path string, bundle config.Bundle) error
	HashFile(path string) (string, error)
	WriteAtomic(path string, data []byte) error
	EnsureDefaultFiles(rootDir string) error
	MigrateLayout(rootDir string) error
	ConfigRootFromYAMLPath(path string) string
}

// EntityFileWriter renders per-entity (.md) file payloads and resolves their
// canonical disk paths. Used by the law/persona/skill services to stage
// FileOps that the BundleEditor then writes through atomically.
type EntityFileWriter interface {
	LawFileBytes(law config.Law) ([]byte, error)
	PersonaFileBytes(persona config.Persona) ([]byte, error)
	SkillFileBytes(skill config.Skill) ([]byte, error)
	EntityFilePath(rootDir string, kind config.EntityKind, slug string) string
	CustomEntityFilePath(rootDir string, kind config.EntityKind, slug string) string
}

// Slugifier normalizes user-supplied identifiers into the kebab-case
// filename convention shared by every entity kind. Lives behind a port so
// the slug-policy isn't owned by a specific config-package import.
type Slugifier interface {
	Slugify(value string) string
}
