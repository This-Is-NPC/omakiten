package agent

import (
	"context"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
	projectresolver "omakiten/internal/project"
	"omakiten/internal/token"
)

const (
	recentContextLimit = 3
	nextWorkLimit      = 5
	similarTaskLimit   = 5

	// Default cap on recent comments shipped per call. Mirrors
	// config.DefaultRecentCommentLimit so the agent layer keeps a sensible
	// floor when no settings have been wired (test fixtures, partially
	// initialized runtimes). Runtime overrides via Service.SetSettings.
	defaultRecentCommentLimit = 5
)

// ServiceSettings carries the runtime-tunable knobs that shape MCP responses.
// Mirrors `config.MCPSettings` but without importing internal/config — the
// agent layer is config-neutral, so the runtime resolves the effective values
// and pushes them in via SetSettings. Defaults are sensible: full bodies,
// 5 most-recent comments, workflow shipped per call, caching hint emitted.
type ServiceSettings struct {
	// RecentCommentLimit caps how many comments tools like tasks.continue
	// and project.overview ship per call. <=0 falls back to the canonical
	// default (5).
	RecentCommentLimit int

	// MaxCommentChars truncates comment bodies past this length with `…`
	// when shipped. <=0 keeps full bodies.
	MaxCommentChars int

	// IncludeWorkflow toggles the `workflow` block in tasks.continue
	// responses by default. Per-call `include_workflow` overrides this.
	IncludeWorkflow bool

	// CachePrompts toggles emitting the cache_control hint on prompts/get
	// content so Anthropic-aware MCP clients reuse the cached prompt.
	CachePrompts bool
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
}

func NewService(repo Repository, selector ProjectSelector) *Service {
	return &Service{
		repo:     repo,
		selector: selector,
		counter:  token.NewCounter(),
		settings: ServiceSettings{
			RecentCommentLimit: defaultRecentCommentLimit,
			MaxCommentChars:    0,
			IncludeWorkflow:    true,
			CachePrompts:       true,
		},
	}
}

// SetSettings replaces the service's runtime knobs. Wiring runs once at
// startup from the resolved config bundle; changing values mid-flight is
// allowed but not expected. Zero values fall through to defaults via the
// effective getters below.
func (s *Service) SetSettings(settings ServiceSettings) {
	if settings.RecentCommentLimit <= 0 {
		settings.RecentCommentLimit = defaultRecentCommentLimit
	}
	if settings.MaxCommentChars < 0 {
		settings.MaxCommentChars = 0
	}
	s.settings = settings
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
	tasks, err := app.NewTaskServiceFromStore(s.repo).List(ctx, project, domain.TaskFilter{})
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
