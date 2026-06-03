package agent

import (
	"context"
	"strings"
	"time"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/domain"
	projectresolver "omakiten/internal/project"
	"omakiten/internal/token"
)

// ServiceSettings carries the runtime-tunable knobs that shape MCP responses.
// Mirrors `config.MCPSettings` but without importing internal/config — the
// agent layer is config-neutral, so the runtime resolves the values from
// the loaded bundle and pushes them in via SetSettings. The composition
// root MUST call SetSettings before the service handles any request;
// validator-required fields in `config.mcp.*` mean the bundle is
// guaranteed complete by the time it reaches here.
type ServiceSettings struct {
	// RecentCommentLimit caps how many comments tools like tasks.continue
	// and project.overview ship per call. Sourced from
	// config.mcp.recent_comment_limit (validator-required > 0).
	RecentCommentLimit int

	// MaxCommentChars truncates comment bodies past this length with `…`
	// when shipped. Sourced from config.mcp.max_comment_chars (validator-
	// required >= 0; 0 keeps full bodies).
	MaxCommentChars int

	// IncludeWorkflow toggles the `workflow` block in tasks.continue
	// responses by default. Per-call `include_workflow` overrides this.
	// Sourced from config.mcp.include_workflow_in_continue.
	IncludeWorkflow bool

	// CachePrompts toggles emitting the cache_control hint on prompts/get
	// content so Anthropic-aware MCP clients reuse the cached prompt.
	// Sourced from config.mcp.cache_prompts.
	CachePrompts bool

	// RecentContextLimit caps how many recent context entries flow into
	// tasks.continue / project.overview / project.resume responses.
	// Sourced from config.mcp.recent_context_limit (validator-required > 0).
	RecentContextLimit int

	// NextWorkLimit caps the "likely next work" suggestion list shipped
	// in project.resume. Sourced from config.mcp.next_work_limit
	// (validator-required > 0).
	NextWorkLimit int

	// SimilarTaskLimit caps how many similar-task hints flow into
	// tasks.create_intent / tasks.continue. Sourced from
	// config.mcp.similar_task_limit (validator-required > 0).
	SimilarTaskLimit int

	// SolutionsTopLimitDefault and SolutionsTopLimitMax mirror
	// config.solutions.{default_top_limit, max_top_limit}. Used by
	// ListTopSolutions to clamp caller-supplied limits so MCP
	// responses stay bounded; the agent constructs an ErrorService
	// per call and writes these values via SetSolutionsDefaults.
	SolutionsTopLimitDefault int
	SolutionsTopLimitMax     int
}

type Repository interface {
	app.ProjectRepository
	app.TaskRepository
	app.WorkflowRepository
	app.GuardEvaluationRepository
	app.CommentRepository
	app.EventRepository
	app.DependencyRepository
	app.ContextEntryRepository
	app.TagRepository
	app.ErrorRepository
	app.MetricsRepository
	app.OrphanRepository
	app.SearchRepository
	app.PlanRepository
}

// TaskTemplateLookup returns the active task template scaffold to embed in
// task-creation responses, scoped by project. The lookup prefers a template
// declaring `default: task` and `project: <slug>` matching the current
// project; it falls back to the global default (no project) when none
// matches. Returns nil when neither is configured.
type TaskTemplateLookup func(projectSlug string) *TaskTemplateSummary

// TemplateCatalog returns every loaded template so the read-only MCP
// endpoints (templates.list / templates.show) can browse the bundle without
// reaching for the BundleEditor directly. The agent never mutates these
// records — assignment happens in the TUI via direct file edits.
type TemplateCatalog func() []TemplateSummary

type Service struct {
	repo               Repository
	selector           ProjectSelector
	counter            token.Counter
	taskTemplateLookup TaskTemplateLookup
	templateCatalog    TemplateCatalog
	skillCatalog       SkillCatalog
	lawCatalog         LawCatalog
	personaCatalog     PersonaCatalog
	commandCatalog     CommandCatalog
	settings           ServiceSettings
	registry           *domain.EnumRegistry
	stopwords          map[string]bool
	synonyms           map[string]string
	// snapshot is the immutable per-project view of the active bundle.
	// SetSnapshot installs it and derives every derived field
	// (catalogs, synonyms, stopwords, registry) from the snap so
	// production composition collapses to a single call. The
	// per-field setters survive as test-only overrides for cases
	// where callers want to stub one closure without building a
	// full Bundle/Snapshot pair.
	snapshot *config.Snapshot
	// workflow is the per-project app.WorkflowService captured against
	// the installed Snapshot. SetSnapshot derives it once so the
	// comment edit/remove paths reuse the same instance instead of
	// allocating a fresh service per call — Phase 2-bis Invariant 3
	// (app services capture *config.Snapshot at construction) applied
	// to the agent layer.
	workflow *app.WorkflowService
	// orphanSvc is the pre-built orphan service injected by the runtime
	// composition root. The runtime knows both the current and previous
	// snapshot at rotation time, so it builds the OrphanService with
	// both pointers and hands it in via SetOrphanService — the agent
	// service no longer carries a previousSnapshot pointer of its own.
	orphanSvc *app.OrphanService
	// now is the wall-clock source clock-dependent helpers (logs
	// `since` resolution today, additional surfaces as the audit
	// extends) call instead of time.Now directly. NewService seeds it
	// with time.Now so production behaviour is unchanged; tests that
	// need deterministic timestamps call SetNow with a
	// testfakes/clock.Fake.Now closure.
	now func() time.Time
}

// NewService constructs the agent service with zero-value settings.
// The composition root MUST call SetSettings with values resolved from
// the user's config.mcp block before the service handles any request —
// the agent layer no longer carries hardcoded defaults. Tests that
// construct a service without going through the runtime composition
// root must call SetSettings explicitly.
func NewService(repo Repository, selector ProjectSelector) *Service {
	return &Service{
		repo:     repo,
		selector: selector,
		counter:  token.NewCounter(),
		now:      time.Now,
		// settings are zero-valued; SetSettings wires the actual knobs.
	}
}

// SetNow overrides the wall-clock source the service hands to
// clock-dependent helpers (resolveLogsSince today). Production
// composition leaves this untouched so the time.Now default seeded by
// NewService stays in effect; tests pass internal/testfakes/clock.Fake.Now
// to retire wall-clock jitter tolerance windows.
//
// Passing nil is a no-op so callers can guard the override behind a
// nil check at the test setup site without an extra branch here.
func (s *Service) SetNow(now func() time.Time) {
	if now == nil {
		return
	}
	s.now = now
}

// nowFunc returns the active wall-clock source. Falls back to
// time.Now when SetNow has not yet been called (defensive against
// tests that construct a Service without the runtime composition
// root); production NewService seeds the field eagerly so this branch
// is exercised by tests only.
func (s *Service) nowFunc() func() time.Time {
	if s.now == nil {
		return time.Now
	}
	return s.now
}

// Synonyms returns the per-project tag-synonym table derived from the
// installed Snapshot. nil when the service was constructed without a
// runtime composition root that wires it via SetSnapshot.
func (s *Service) Synonyms() map[string]string {
	return s.synonyms
}

// newCommentService builds an app.CommentService wired with the
// per-project Snapshot so NormalizeTagName resolves the bundle's
// alias table without any post-construction setter call.
func (s *Service) newCommentService() *app.CommentService {
	return app.NewCommentService(s.repo, s.snapshot)
}

// newCommentServiceWithWorkflow mirrors newCommentService for the
// edit/remove flows that additionally need workflow policy enforcement.
func (s *Service) newCommentServiceWithWorkflow(workflow *app.WorkflowService) *app.CommentService {
	return app.NewCommentServiceWithWorkflow(s.repo, workflow, s.snapshot)
}

// newErrorService builds an app.ErrorService wired with the per-project
// Snapshot. Solutions defaults still flow through the agent service's
// SetSolutionsDefaults pathway because the limits live on
// ServiceSettings rather than the Snapshot.
func (s *Service) newErrorService() *app.ErrorService {
	return app.NewErrorService(s.repo, s.snapshot)
}

// newTagService builds an app.TagService (with event emission wired to
// the same repo, mirroring the existing inline NewTagServiceWithEvents
// shape) and captures the per-project Snapshot for synonym lookups.
func (s *Service) newTagService() *app.TagService {
	return app.NewTagServiceWithEvents(s.repo, s.repo, s.snapshot)
}

// SetProjectSelector replaces the service's default project selector.
// agentruntime constructs the service with a zero selector and calls
// this once boot has resolved the project from --project/--cwd; this
// matches the Phase 3a runtime pattern where the BundleCache builds
// services first and the boot path threads the selector after.
func (s *Service) SetProjectSelector(selector ProjectSelector) {
	s.selector = selector
}

// Selector returns the service's default ProjectSelector. Exposed for
// tests that assert the boot-resolved selector survives BundleCache
// rebuilds.
func (s *Service) Selector() ProjectSelector {
	return s.selector
}

// SetSettings replaces the service's runtime knobs with values from the
// active bundle. The runtime composition root invokes this exactly once
// at startup; the values flow from `bundle.Config.MCP.*`. The agent
// layer cannot import internal/config (hexagonal rule), so the runtime
// is the single point that bridges the two. Validator guarantees every
// MCP field is set in the bundle, so settings here is always complete.
func (s *Service) SetSettings(settings ServiceSettings) {
	s.settings = settings
}

// SetSnapshot installs the per-project *config.Snapshot the service
// reads workflow / catalog / synonym / stopword / registry state from.
// The production composition root (agentruntime.buildProjectRuntime)
// calls this once per ProjectRuntime; it is the sole wiring entry point
// because every per-field state is derivable from the snapshot. Tests
// that want to stub catalogs build a Snapshot via the snapshotWith*
// helpers and pass it here.
//
// SetSnapshot derives every closure-shaped field
// (taskTemplateLookup / templateCatalog / skillCatalog / lawCatalog /
// personaCatalog / commandCatalog) plus the synonyms map, the stopwords
// set, and the bundle-scoped EnumRegistry. Two projects holding two
// snapshots see two independent catalog views; cache.Reload rotates the
// pointer atomically through a fresh ProjectRuntime.
func (s *Service) SetSnapshot(snap *config.Snapshot) {
	s.snapshot = snap
	if snap == nil {
		s.workflow = nil
		return
	}
	s.taskTemplateLookup = snapshotTaskTemplateLookup(snap)
	s.templateCatalog = snapshotTemplateCatalog(snap)
	s.skillCatalog = snapshotSkillCatalog(snap)
	s.lawCatalog = snapshotLawCatalog(snap)
	s.personaCatalog = snapshotPersonaCatalog(snap)
	s.commandCatalog = snapshotCommandCatalog(snap)
	s.synonyms = snap.Synonyms()
	s.stopwords = stopwordsTable(snap.Stopwords())
	s.registry = snap.Registry()
	s.workflow = app.NewWorkflowServiceFromStore(s.repo, s.registry, snap)
}

// Snapshot returns the per-project *config.Snapshot wired via
// SetSnapshot, or nil when no runtime composition has installed one
// (e.g. unit tests that drive the service directly).
func (s *Service) Snapshot() *config.Snapshot {
	return s.snapshot
}

// SetOrphanService installs the pre-built orphan service that knows the
// current and previous per-project Snapshot pair. The runtime composition
// root (agentruntime.buildProjectRuntime / BundleCache.rebuild) is the
// single caller — it owns both snapshot pointers across cache rotations
// so the agent service does not have to. nil is permitted for tests that
// do not exercise the orphan flow.
func (s *Service) SetOrphanService(svc *app.OrphanService) {
	s.orphanSvc = svc
}

// SettingsCachePrompts exposes the cache-prompts toggle for the MCP adapter.
// The agent service does not reach across packages to render PromptResult,
// so the adapter calls this when stamping `cache_control` hints.
func (s *Service) SettingsCachePrompts() bool {
	return s.settings.CachePrompts
}

func (s *Service) resolveProject(ctx context.Context, selector ProjectSelector) (domain.ProjectContext, error) {
	effective := s.selector
	if selector.ProjectID > 0 || strings.TrimSpace(selector.Project) != "" || strings.TrimSpace(selector.CWD) != "" {
		effective = selector
		if strings.TrimSpace(effective.CWD) == "" {
			effective.CWD = s.selector.CWD
		}
		if strings.TrimSpace(effective.Project) != "" {
			effective.Project = strings.TrimSpace(effective.Project)
		}
	}
	return projectresolver.NewResolver(s.repo).Resolve(ctx, projectresolver.ResolveOptions{ProjectID: effective.ProjectID, Project: effective.Project, CWD: effective.CWD})
}

func (s *Service) projectState(ctx context.Context, project domain.ProjectContext) ([]domain.Task, domain.Workflow, error) {
	tasks, err := app.NewTaskServiceFromStore(s.repo, s.registry, s.snapshot).List(ctx, project, domain.TaskFilter{})
	if err != nil {
		return nil, domain.Workflow{}, err
	}
	workflow := s.snapshot.Workflow()
	return tasks, workflow, nil
}
