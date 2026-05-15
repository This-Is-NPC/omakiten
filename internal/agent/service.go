package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
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
	app.ConfigRepository
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
		// settings are zero-valued; SetSettings wires the actual knobs.
	}
}

// SetSynonyms installs the per-project tag-synonym table the wrapped
// app services (CommentService / ErrorService / TagService) thread
// into NormalizeTagName. Phase 3f replaced the process-global
// registry with this per-Service field. Tests that build a Service
// without going through the runtime can leave this unset; nil means
// "no substitution applied".
func (s *Service) SetSynonyms(synonyms map[string]string) {
	s.synonyms = synonyms
}

// Synonyms returns the per-project tag-synonym table installed via
// SetSynonyms. nil when the service was constructed without a runtime
// composition root that wires it.
func (s *Service) Synonyms() map[string]string {
	return s.synonyms
}

// newCommentService builds an app.CommentService wired with the
// per-project tag synonyms. Centralises the SetSynonyms call so the
// dto handler files (service_comments.go / service_progress.go /
// service_tasks.go) do not repeat the wiring on every inline
// construction.
func (s *Service) newCommentService() *app.CommentService {
	svc := app.NewCommentService(s.repo)
	svc.SetSynonyms(s.synonyms)
	return svc
}

// newCommentServiceWithWorkflow mirrors newCommentService for the
// edit/remove flows that additionally need workflow policy enforcement.
func (s *Service) newCommentServiceWithWorkflow(workflow *app.WorkflowService) *app.CommentService {
	svc := app.NewCommentServiceWithWorkflow(s.repo, workflow)
	svc.SetSynonyms(s.synonyms)
	return svc
}

// newErrorService builds an app.ErrorService wired with the per-project
// synonyms (and forwards the SolutionsDefaults the caller threads in
// separately when ListTopSolutions needs the bundle's limits).
func (s *Service) newErrorService() *app.ErrorService {
	svc := app.NewErrorService(s.repo)
	svc.SetSynonyms(s.synonyms)
	return svc
}

// newTagService builds an app.TagService (with event emission wired to
// the same repo, mirroring the existing inline NewTagServiceWithEvents
// shape) and applies the per-project synonym table.
func (s *Service) newTagService() *app.TagService {
	svc := app.NewTagServiceWithEvents(s.repo, s.repo)
	svc.SetSynonyms(s.synonyms)
	return svc
}

// SetStopwords installs the per-project stopword set the similar-task
// ranker reads from. Phase 3f replaced the process-global registry with
// this per-Service field so two projects' stopword tables stay
// disjoint in the same binary. Passing nil disables stopword filtering
// for this service.
func (s *Service) SetStopwords(words []string) {
	s.stopwords = stopwordsTable(words)
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

// SetRegistry wires the enum registry so priority/severity label lookups
// use the user-configured values rather than the deprecated global methods.
// The runtime composition root calls this once at startup after Import.
func (s *Service) SetRegistry(r *domain.EnumRegistry) {
	s.registry = r
}

// SettingsCachePrompts exposes the cache-prompts toggle for the MCP adapter.
// The agent service does not reach across packages to render PromptResult,
// so the adapter calls this when stamping `cache_control` hints.
func (s *Service) SettingsCachePrompts() bool {
	return s.settings.CachePrompts
}

// SetTaskTemplateLookup wires the active task template provider. The runtime
// calls this after constructing the service so that CreateTask responses can
// embed the configured scaffold.
func (s *Service) SetTaskTemplateLookup(lookup TaskTemplateLookup) {
	s.taskTemplateLookup = lookup
}

// SetTemplateCatalog wires the read-only catalog used by templates.list and
// templates.show. Without it the service still works but the MCP query
// endpoints return empty payloads.
func (s *Service) SetTemplateCatalog(catalog TemplateCatalog) {
	s.templateCatalog = catalog
}

// SetSkillCatalog, SetLawCatalog, SetPersonaCatalog and SetCommandCatalog wire
// the lookups ResolveCommand needs to assemble a prompt response. When any of
// them is missing, ResolveCommand still answers with whatever bindings can be
// resolved — empty catalogs degrade to a bare action prompt rather than an
// error so older runtimes that haven't been updated keep working.
func (s *Service) SetSkillCatalog(catalog SkillCatalog) { s.skillCatalog = catalog }
func (s *Service) SetLawCatalog(catalog LawCatalog)     { s.lawCatalog = catalog }
func (s *Service) SetPersonaCatalog(catalog PersonaCatalog) {
	s.personaCatalog = catalog
}
func (s *Service) SetCommandCatalog(catalog CommandCatalog) {
	s.commandCatalog = catalog
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

func (s *Service) projectState(ctx context.Context, project domain.ProjectContext) ([]domain.Task, domain.Workflow, []domain.ContextEntry, error) {
	tasks, err := app.NewTaskServiceFromStore(s.repo, s.registry).List(ctx, project, domain.TaskFilter{})
	if err != nil {
		return nil, domain.Workflow{}, nil, err
	}
	workflow, err := s.repo.ActiveWorkflow(ctx)
	if err != nil {
		return nil, domain.Workflow{}, nil, err
	}
	entries, err := s.repo.ListContextEntries(ctx, project.ID)
	if err != nil {
		return nil, domain.Workflow{}, nil, err
	}
	return tasks, workflow, entries, nil
}
