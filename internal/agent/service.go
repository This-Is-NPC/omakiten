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
	recentCommentLimit = 5
	nextWorkLimit      = 5
	similarTaskLimit   = 5
)

type Repository interface {
	app.ProjectRepository
	app.ConfigRepository
	app.TaskRepository
	app.CommentRepository
	app.EventRepository
	app.DependencyRepository
	app.ContextEntryRepository
	app.TagRepository
	app.ErrorRepository
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
}

func NewService(repo Repository, selector ProjectSelector) *Service {
	return &Service{repo: repo, selector: selector, counter: token.NewCounter()}
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
	tasks, err := app.NewTaskService(s.repo).List(ctx, project, domain.TaskFilter{})
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
