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

// ListTemplates returns the templates relevant for the requested filters.
//
// When `project` is set, the response is project-aware: per default kind we
// return the project-scoped template if one exists, otherwise the global
// fallback — never both. This lets the agent ask "which template does this
// project use for kind X?" with a single round-trip and a single result,
// avoiding the wasted tokens of receiving both candidates and filtering
// client-side. Templates without a `default:` are inactive — they are
// excluded from project-filtered responses but appear in unfiltered ones.
//
// When `project` is empty the call is non-resolving: every loaded template
// matching `kind` is returned (or every loaded template when `kind` is also
// empty). Useful for browsing the full catalog.
//
// Body is omitted by default to keep responses compact; callers set
// IncludeBody=true when they need the full scaffold.
func (s *Service) ListTemplates(_ context.Context, input ListTemplatesInput) (ListTemplatesResponse, error) {
	if s.templateCatalog == nil {
		return ListTemplatesResponse{Templates: []TemplateSummary{}}, nil
	}
	all := s.templateCatalog()

	if input.Project == "" {
		out := make([]TemplateSummary, 0, len(all))
		for _, t := range all {
			if input.Kind != "" && t.Default != input.Kind {
				continue
			}
			if !input.IncludeBody {
				t.Body = ""
			}
			out = append(out, t)
		}
		return ListTemplatesResponse{Templates: out}, nil
	}

	// project-filtered: resolve one template per kind. Iterate twice — first
	// pass collects the project-scoped winners, second pass fills gaps with
	// the global fallback. Single linear scan would also work but two passes
	// keep the precedence rule obvious.
	scoped := map[string]TemplateSummary{}
	global := map[string]TemplateSummary{}
	for _, t := range all {
		if t.Default == "" {
			continue
		}
		if input.Kind != "" && t.Default != input.Kind {
			continue
		}
		switch t.Project {
		case input.Project:
			scoped[t.Default] = t
		case "":
			global[t.Default] = t
		}
	}
	out := make([]TemplateSummary, 0, len(scoped)+len(global))
	for kind, t := range scoped {
		if !input.IncludeBody {
			t.Body = ""
		}
		out = append(out, t)
		delete(global, kind) // scoped wins; drop the global so it does not double up
	}
	for _, t := range global {
		if !input.IncludeBody {
			t.Body = ""
		}
		out = append(out, t)
	}
	return ListTemplatesResponse{Templates: out}, nil
}

// ShowTemplate returns one template by slug, with body included.
func (s *Service) ShowTemplate(_ context.Context, input ShowTemplateInput) (ShowTemplateResponse, error) {
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template slug is required", nil)
	}
	if s.templateCatalog == nil {
		return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template catalog not initialized", map[string]any{"slug": slug})
	}
	for _, t := range s.templateCatalog() {
		if t.Slug == slug {
			return ShowTemplateResponse{Template: t}, nil
		}
	}
	return ShowTemplateResponse{}, domain.NewError(domain.ErrValidation, "template not found", map[string]any{"slug": slug})
}

func (s *Service) Overview(ctx context.Context, input OverviewInput) (OverviewResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return OverviewResponse{}, err
	}

	tasks, workflow, entries, err := s.projectState(ctx, project)
	if err != nil {
		return OverviewResponse{}, err
	}

	return OverviewResponse{
		Project:        projectSummary(project),
		Workflow:       workflowSummary(workflow),
		PendingCount:   pendingCount(tasks),
		TaskBuckets:    bucketCounts(workflow, tasks),
		RecentContext:  contextSnippets(entries, recentContextLimit),
		NextStepPrompt: "Omakiten is ready. Ask for task details, continue the latest checkpoint, or create a new task intent.",
	}, nil
}

func (s *Service) ResumeProject(ctx context.Context, input ResumeProjectInput) (ResumeProjectResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ResumeProjectResponse{}, err
	}

	tasks, workflow, entries, err := s.projectState(ctx, project)
	if err != nil {
		return ResumeProjectResponse{}, err
	}
	dependencies, err := app.NewDependencyService(s.repo).List(ctx, project, 0)
	if err != nil {
		return ResumeProjectResponse{}, err
	}

	return ResumeProjectResponse{
		Project:        projectSummary(project),
		Workflow:       workflowSummary(workflow),
		TaskBuckets:    bucketCounts(workflow, tasks),
		LikelyNextWork: likelyNextWork(tasks),
		BlockedWork:    blockedWork(tasks, dependencies),
		Dependencies:   dependencySummaries(dependencies),
		RecentContext:  contextSnippets(entries, recentContextLimit),
		NextStepPrompt: "Choose a likely next task, inspect blocked work, or ask for `/okt-continue #<id>` context.",
	}, nil
}

func (s *Service) AddComment(ctx context.Context, input AddCommentInput) (CommentResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentResponse{}, err
	}
	comment, err := app.NewCommentService(s.repo).Add(ctx, project, input.TaskID, input.Body, input.AuthorType, input.Tags)
	if err != nil {
		return CommentResponse{}, err
	}
	return CommentResponse{Project: projectSummary(project), Comment: commentSummary(comment)}, nil
}

func (s *Service) ListComments(ctx context.Context, input ListCommentsInput) (CommentsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CommentsResponse{}, err
	}
	comments, err := app.NewCommentService(s.repo).List(ctx, project, input.TaskID)
	if err != nil {
		return CommentsResponse{}, err
	}
	return CommentsResponse{Project: projectSummary(project), Comments: commentSummaries(comments)}, nil
}

func (s *Service) ListTaskActivity(ctx context.Context, input ListTaskActivityInput) (ListTaskActivityResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListTaskActivityResponse{}, err
	}
	events, err := app.NewEventService(s.repo).ListTaskActivity(ctx, project, input.TaskID, input.Order)
	if err != nil {
		return ListTaskActivityResponse{}, err
	}
	resolvedOrder := strings.ToLower(strings.TrimSpace(input.Order))
	if resolvedOrder != "asc" && resolvedOrder != "desc" {
		resolvedOrder = "asc"
	}
	return ListTaskActivityResponse{
		Project: projectSummary(project),
		Events:  eventSummaries(events),
		Order:   resolvedOrder,
	}, nil
}

func (s *Service) AddDependency(ctx context.Context, input AddDependencyInput) (DependencyResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DependencyResponse{}, err
	}
	dependency, err := app.NewDependencyService(s.repo).Add(ctx, project, input.TaskID, input.DependsOnTaskID)
	if err != nil {
		return DependencyResponse{}, err
	}
	return DependencyResponse{Project: projectSummary(project), Dependency: dependencySummary(dependency)}, nil
}

func (s *Service) RemoveDependency(ctx context.Context, input RemoveDependencyInput) (RemoveDependencyResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RemoveDependencyResponse{}, err
	}
	if !input.Confirmed {
		return RemoveDependencyResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a dependency changes task ordering and requires explicit confirmation.",
				Options:              []ConfirmationOption{{Action: "remove_dependency", Label: "Retry with confirmed=true to remove it"}},
			},
		}, nil
	}
	if err := app.NewDependencyService(s.repo).Remove(ctx, project, input.TaskID, input.DependsOnTaskID); err != nil {
		return RemoveDependencyResponse{}, err
	}
	return RemoveDependencyResponse{Project: projectSummary(project), Removed: true}, nil
}

func (s *Service) ListDependencies(ctx context.Context, input ListDependenciesInput) (DependenciesResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DependenciesResponse{}, err
	}
	dependencies, err := app.NewDependencyService(s.repo).List(ctx, project, input.TaskID)
	if err != nil {
		return DependenciesResponse{}, err
	}
	return DependenciesResponse{Project: projectSummary(project), Dependencies: dependencySummaries(dependencies)}, nil
}

func (s *Service) AddContext(ctx context.Context, input AddContextInput) (ContextResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ContextResponse{}, err
	}
	entry, err := app.NewContextService(s.repo, s.repo, s.repo, s.repo, s.repo, s.counter).Add(ctx, project, input.Body)
	if err != nil {
		return ContextResponse{}, err
	}
	return ContextResponse{Project: projectSummary(project), Entry: contextSnippet(entry)}, nil
}

func (s *Service) DumpContext(ctx context.Context, input DumpContextInput) (DumpContextResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DumpContextResponse{}, err
	}
	level := input.Level
	if level == 0 {
		settings, err := s.repo.ContextSettings(ctx)
		if err != nil {
			return DumpContextResponse{}, err
		}
		level = settings.DefaultLevel
	}
	dump, err := app.NewContextService(s.repo, s.repo, s.repo, s.repo, s.repo, s.counter).Dump(ctx, project, level)
	if err != nil {
		return DumpContextResponse{}, err
	}
	return DumpContextResponse{
		Project:      projectSummary(project),
		Level:        dump.Level,
		TaskCount:    dump.TaskCount,
		TokenMetrics: dump.TokenMetrics,
		Context:      contextSnippets(dump.ContextEntries, 0),
		Workflow:     workflowSummary(dump.Workflow),
		Tasks:        taskSummaries(dump.Tasks),
		Dependencies: dependencySummaries(dump.Dependencies),
		Comments:     commentSummaries(dump.Comments),
	}, nil
}

func (s *Service) ShowWorkflow(ctx context.Context, input WorkflowInput) (WorkflowResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return WorkflowResponse{}, err
	}
	workflow, err := s.repo.ActiveWorkflow(ctx)
	if err != nil {
		return WorkflowResponse{}, err
	}
	return WorkflowResponse{Project: projectSummary(project), Workflow: workflowSummary(workflow)}, nil
}

func (s *Service) RecordProgress(ctx context.Context, input RecordProgressInput) (RecordProgressResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RecordProgressResponse{}, err
	}

	if input.TaskID <= 0 && (input.Title != nil || input.Description != nil || input.Priority != nil || strings.TrimSpace(input.MoveToBucket) != "" || strings.TrimSpace(input.Comment) != "") {
		return RecordProgressResponse{}, domain.NewError(domain.ErrValidation, "task_id is required for task edits, comments, and workflow moves", nil)
	}
	if input.TaskID <= 0 && strings.TrimSpace(input.Context) == "" {
		return RecordProgressResponse{}, domain.NewError(domain.ErrValidation, "at least one progress update is required", nil)
	}

	response := RecordProgressResponse{Project: projectSummary(project)}
	if input.TaskID > 0 && (input.Title != nil || input.Description != nil || input.Priority != nil || strings.TrimSpace(input.MoveToBucket) != "") {
		task, err := app.NewTaskService(s.repo).Edit(ctx, project, input.TaskID, domain.TaskUpdate{
			Title:       input.Title,
			Description: input.Description,
			Priority:    input.Priority,
			BucketKey:   input.MoveToBucket,
		})
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := taskSummary(task)
		response.Task = &summary
	}
	if strings.TrimSpace(input.Comment) != "" {
		comment, err := app.NewCommentService(s.repo).Add(ctx, project, input.TaskID, input.Comment, input.AuthorType, nil)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := commentSummary(comment)
		response.Comment = &summary
	}
	if strings.TrimSpace(input.Context) != "" {
		entry, err := app.NewContextService(s.repo, s.repo, s.repo, s.repo, s.repo, s.counter).Add(ctx, project, input.Context)
		if err != nil {
			return RecordProgressResponse{}, err
		}
		summary := contextSnippet(entry)
		response.ContextEntry = &summary
	}

	return response, nil
}

func (s *Service) AddTag(ctx context.Context, input AddTagInput) (TagResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return TagResponse{}, err
	}
	tag, err := app.NewTagService(s.repo).Add(ctx, project, input.EntityType, input.EntityID, input.TagName)
	if err != nil {
		return TagResponse{}, err
	}
	return TagResponse{Project: projectSummary(project), Tag: tagSummary(tag)}, nil
}

func (s *Service) RemoveTag(ctx context.Context, input RemoveTagInput) (RemoveTagResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return RemoveTagResponse{}, err
	}
	if !input.Confirmed {
		return RemoveTagResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Removing a tag is irreversible and requires explicit confirmation.",
				Options:              []ConfirmationOption{{Action: "remove_tag", Label: "Retry with confirmed=true to remove it"}},
			},
		}, nil
	}
	if err := app.NewTagService(s.repo).Remove(ctx, project, input.EntityType, input.EntityID, input.TagID); err != nil {
		return RemoveTagResponse{}, err
	}
	return RemoveTagResponse{Project: projectSummary(project), Removed: true}, nil
}

func (s *Service) ListTags(ctx context.Context, input ListTagsInput) (TagListResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return TagListResponse{}, err
	}
	tags, err := app.NewTagService(s.repo).List(ctx, project, input.EntityType, input.EntityID)
	if err != nil {
		return TagListResponse{}, err
	}
	return TagListResponse{Project: projectSummary(project), Tags: tagSummaries(tags)}, nil
}

func (s *Service) ListAllTags(ctx context.Context) (AllTagsResponse, error) {
	tags, err := app.NewTagService(s.repo).ListAll(ctx)
	if err != nil {
		return AllTagsResponse{}, err
	}
	return AllTagsResponse{Tags: tagSummaries(tags)}, nil
}

func (s *Service) MergeTags(ctx context.Context, input MergeTagsInput) (TagResponse, error) {
	tag, err := app.NewTagService(s.repo).Merge(ctx, input.SourceTagID, input.TargetTagID)
	if err != nil {
		return TagResponse{}, err
	}
	return TagResponse{Tag: tagSummary(tag)}, nil
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
