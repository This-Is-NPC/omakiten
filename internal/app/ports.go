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
}

type ConfigRepository interface {
	ImportBundle(ctx context.Context, bundle config.Bundle, sourcePath, sourceHash string) error
	ListActiveLaws(ctx context.Context) ([]domain.Law, error)
	ListActiveSkills(ctx context.Context) ([]domain.Skill, error)
	ListActivePersonas(ctx context.Context) ([]domain.Persona, error)
	ActiveWorkflow(ctx context.Context) (domain.Workflow, error)
	ContextSettings(ctx context.Context) (domain.ContextSettings, error)
}

// TaskRepository persists task rows. The methods are deliberately policy-free:
// CreateTask requires a non-empty bucket key (default-bucket selection lives in
// app.WorkflowService) and MoveTask is a pure persist + task.moved emission
// (transition allowed?, guards, and task.completed-on-final live in
// app.WorkflowService too).
type TaskRepository interface {
	CreateTask(ctx context.Context, projectID int64, title, description string, priority domain.Priority, bucketKey string) (domain.Task, error)
	ListTasks(ctx context.Context, projectID int64, filter domain.TaskFilter) ([]domain.Task, error)
	MoveTask(ctx context.Context, projectID, taskID int64, targetBucketKey string) (domain.Task, error)
	UpdateTask(ctx context.Context, projectID, taskID int64, update domain.TaskUpdate) (domain.Task, error)
	TaskCount(ctx context.Context, projectID int64) (int64, error)
	// HardDeleteTask removes a task and its dependent rows (events, event_tags,
	// task_dependencies, task_tags) and emits a task.removed system event with
	// a snapshot payload. Bucket policy/operation guards are enforced at the
	// service layer; the repository performs the cascade unconditionally.
	HardDeleteTask(ctx context.Context, projectID, taskID int64) (domain.Event, error)
	// SetTaskState flips active|archived. When targetBucketKey is non-empty
	// the row also moves into that bucket atomically (used by archive). Emits
	// task.archived / task.unarchived in the same transaction.
	SetTaskState(ctx context.Context, projectID, taskID int64, state domain.TaskState, targetBucketKey string) (domain.Task, domain.Event, error)
	// EmitTaskEditedEvent records a task.edited row with a payload describing
	// the changed fields. Service layer calls it after a successful UpdateTask.
	EmitTaskEditedEvent(ctx context.Context, projectID, taskID int64, before, after domain.Task) (domain.Event, error)
}

// WorkflowRepository exposes the workflow primitives the app's WorkflowService
// composes into the move/create policy. Each method is a pure read against the
// active workflow — no side effects, no policy decisions.
type WorkflowRepository interface {
	ResolveActiveBucket(ctx context.Context, key string) (domain.Bucket, error)
	IsFinalActiveBucket(ctx context.Context, bucketID int64) (bool, error)
	TransitionAllowed(ctx context.Context, fromBucketID, toBucketID int64) (bool, error)
	LoadTransitionGuards(ctx context.Context, fromBucketID, toBucketID int64) ([]domain.TransitionGuard, error)
	CurrentTaskBucket(ctx context.Context, projectID, taskID int64) (int64, string, error)
	// TaskState returns the active|archived flag for a task. Used by MoveTask
	// to reject moves on archived rows and by the guards engine to skip
	// transition-guard evaluation when the task sits in the archived lane.
	TaskState(ctx context.Context, projectID, taskID int64) (domain.TaskState, error)
}

// GuardEvaluationRepository exposes the read-only counts the workflow guards
// need. Split from WorkflowRepository so guard evaluation can be stubbed
// independently in tests.
type GuardEvaluationRepository interface {
	ListTaskBlockerBuckets(ctx context.Context, projectID, taskID int64) ([]domain.TaskBlocker, error)
	CountTaskComments(ctx context.Context, projectID, taskID int64) (int, error)
	CountTaskCommentsTagged(ctx context.Context, projectID, taskID int64, tagName string) (int, error)
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
	SearchErrors(ctx context.Context, query string, tagNames []string) ([]domain.ErrorRecord, error)
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
