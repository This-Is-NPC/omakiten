package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

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
	repo             Repository
	selector         ProjectSelector
	counter          token.Counter
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
		if t.Project == input.Project {
			scoped[t.Default] = t
		} else if t.Project == "" {
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

func (s *Service) ContinueTask(ctx context.Context, input ContinueTaskInput) (ContinueTaskResponse, error) {
	if input.TaskID <= 0 {
		return ContinueTaskResponse{}, domain.NewError(domain.ErrValidation, "task id must be positive", map[string]any{"task_id": input.TaskID})
	}

	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ContinueTaskResponse{}, err
	}

	tasks, err := app.NewTaskService(s.repo).List(ctx, project, domain.TaskFilter{})
	if err != nil {
		return ContinueTaskResponse{}, err
	}
	task, ok := findTask(tasks, input.TaskID)
	if !ok {
		return ContinueTaskResponse{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": input.TaskID, "project_id": project.ID})
	}

	workflow, err := s.repo.ActiveWorkflow(ctx)
	if err != nil {
		return ContinueTaskResponse{}, err
	}
	dependencies, err := app.NewDependencyService(s.repo).List(ctx, project, input.TaskID)
	if err != nil {
		return ContinueTaskResponse{}, err
	}
	comments, err := app.NewCommentService(s.repo).List(ctx, project, input.TaskID)
	if err != nil {
		return ContinueTaskResponse{}, err
	}
	entries, err := s.repo.ListContextEntries(ctx, project.ID)
	if err != nil {
		return ContinueTaskResponse{}, err
	}

	return ContinueTaskResponse{
		Project:        projectSummary(project),
		Task:           taskSummary(task),
		Workflow:       workflowSummary(workflow),
		Dependencies:   dependencySummaries(dependencies),
		Comments:       recentComments(comments, recentCommentLimit),
		RecentContext:  contextSnippets(entries, recentContextLimit),
		NextStepPrompt: fmt.Sprintf("Continue task #%d from this checkpoint, then record material progress with `progress.record`.", task.ID),
	}, nil
}

func (s *Service) ListTasks(ctx context.Context, input ListTasksInput) (ListTasksResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListTasksResponse{}, err
	}
	tasks, err := app.NewTaskService(s.repo).List(ctx, project, domain.TaskFilter{BucketKey: strings.TrimSpace(input.BucketKey)})
	if err != nil {
		return ListTasksResponse{}, err
	}
	return ListTasksResponse{Project: projectSummary(project), Tasks: taskSummaries(tasks)}, nil
}

func (s *Service) CreateTaskIntent(ctx context.Context, input CreateTaskInput) (CreateTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return CreateTaskResponse{}, err
	}

	title, description := taskTitleAndDescription(input.Title, input.Description)
	if title == "" {
		return CreateTaskResponse{}, domain.NewError(domain.ErrValidation, "task title or description is required", nil)
	}

	template := s.activeTaskTemplate(project.Slug)

	if !input.SkipSimilarityCheck && !input.Confirmed {
		tasks, err := app.NewTaskService(s.repo).List(ctx, project, domain.TaskFilter{})
		if err != nil {
			return CreateTaskResponse{}, err
		}
		similar := similarTasks(title+" "+description, tasks, similarTaskLimit)
		if len(similar) > 0 {
			return CreateTaskResponse{
				Project:      projectSummary(project),
				SimilarTasks: similar,
				Template:     template,
				Confirmation: Confirmation{
					RequiresConfirmation: true,
					Reason:               "Likely duplicate or related work already exists in this project.",
					Options: []ConfirmationOption{
						{Action: "continue_existing", Label: "Continue one of the similar tasks"},
						{Action: "create_separate", Label: "Create a separate task with confirmed=true"},
					},
				},
			}, nil
		}
	}

	task, err := app.NewTaskService(s.repo).Add(ctx, project, title, description, strings.TrimSpace(input.Priority), strings.TrimSpace(input.BucketKey))
	if err != nil {
		return CreateTaskResponse{}, err
	}
	summary := taskSummary(task)
	return CreateTaskResponse{Project: projectSummary(project), Task: &summary, Template: template}, nil
}

func (s *Service) activeTaskTemplate(projectSlug string) *TaskTemplateSummary {
	if s.taskTemplateLookup == nil {
		return nil
	}
	return s.taskTemplateLookup(projectSlug)
}

func (s *Service) CreateTask(ctx context.Context, input CreateTaskInput) (CreateTaskResponse, error) {
	input.SkipSimilarityCheck = true
	return s.CreateTaskIntent(ctx, input)
}

func (s *Service) MoveTask(ctx context.Context, input MoveTaskInput) (MoveTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return MoveTaskResponse{}, err
	}
	task, err := app.NewTaskService(s.repo).Move(ctx, project, input.TaskID, input.BucketKey)
	if err != nil {
		return MoveTaskResponse{}, err
	}
	return MoveTaskResponse{Project: projectSummary(project), Task: taskSummary(task)}, nil
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

func (s *Service) RecordError(ctx context.Context, input RecordErrorInput) (ErrorRecordResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ErrorRecordResponse{}, err
	}
	record, err := app.NewErrorService(s.repo).Record(ctx, project, input.Description, input.Context, input.Tags)
	if err != nil {
		return ErrorRecordResponse{}, err
	}
	return ErrorRecordResponse{Project: projectSummary(project), Error: errorSummary(record)}, nil
}

func (s *Service) SearchErrors(ctx context.Context, input SearchErrorsInput) (SearchErrorsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return SearchErrorsResponse{}, err
	}
	records, err := app.NewErrorService(s.repo).Search(ctx, project, input.Query, input.Tags)
	if err != nil {
		return SearchErrorsResponse{}, err
	}
	out := make([]ErrorSummary, 0, len(records))
	for _, r := range records {
		out = append(out, errorSummary(r))
	}
	return SearchErrorsResponse{Project: projectSummary(project), Errors: out}, nil
}

func (s *Service) AddSolution(ctx context.Context, input AddSolutionInput) (SolutionResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return SolutionResponse{}, err
	}
	var taskID *int64
	if input.TaskID > 0 {
		v := input.TaskID
		taskID = &v
	}
	solution, err := app.NewErrorService(s.repo).AddSolution(ctx, project, input.ErrorID, input.Description, input.Steps, taskID)
	if err != nil {
		return SolutionResponse{}, err
	}
	return SolutionResponse{Project: projectSummary(project), Solution: solutionSummary(solution)}, nil
}

func (s *Service) ConfirmSolution(ctx context.Context, input ConfirmSolutionInput) (SolutionResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return SolutionResponse{}, err
	}
	solution, err := app.NewErrorService(s.repo).ConfirmSolution(ctx, project, input.SolutionID, input.Success)
	if err != nil {
		return SolutionResponse{}, err
	}
	return SolutionResponse{Project: projectSummary(project), Solution: solutionSummary(solution)}, nil
}

func (s *Service) ListTopSolutions(ctx context.Context, input ListTopSolutionsInput) (TopSolutionsResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return TopSolutionsResponse{}, err
	}
	solutions, err := app.NewErrorService(s.repo).ListTopSolutions(ctx, project, input.Limit)
	if err != nil {
		return TopSolutionsResponse{}, err
	}
	out := make([]SolutionSummary, 0, len(solutions))
	for _, sol := range solutions {
		out = append(out, solutionSummary(sol))
	}
	return TopSolutionsResponse{Project: projectSummary(project), Solutions: out}, nil
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

func taskSummaries(tasks []domain.Task) []TaskSummary {
	out := make([]TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, taskSummary(task))
	}
	return out
}

func dependencySummaries(dependencies []domain.TaskDependency) []DependencySummary {
	out := make([]DependencySummary, 0, len(dependencies))
	for _, dependency := range dependencies {
		out = append(out, dependencySummary(dependency))
	}
	return out
}

func commentSummaries(comments []domain.Comment) []CommentSummary {
	out := make([]CommentSummary, 0, len(comments))
	for _, comment := range comments {
		out = append(out, commentSummary(comment))
	}
	return out
}

func contextSnippets(entries []domain.ContextEntry, limit int) []ContextSnippet {
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	out := make([]ContextSnippet, 0, len(entries))
	for _, entry := range entries {
		out = append(out, contextSnippet(entry))
	}
	return out
}

func recentComments(comments []domain.Comment, limit int) []CommentSummary {
	if limit > 0 && len(comments) > limit {
		comments = comments[len(comments)-limit:]
	}
	out := commentSummaries(comments)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func findTask(tasks []domain.Task, taskID int64) (domain.Task, bool) {
	for _, task := range tasks {
		if task.ID == taskID {
			return task, true
		}
	}
	return domain.Task{}, false
}

func pendingCount(tasks []domain.Task) int {
	count := 0
	for _, task := range tasks {
		if task.BucketKey != "done" {
			count++
		}
	}
	return count
}

func bucketCounts(workflow domain.Workflow, tasks []domain.Task) []BucketCount {
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.BucketKey]++
	}
	out := make([]BucketCount, 0, len(workflow.Buckets))
	seen := map[string]struct{}{}
	for _, bucket := range workflow.Buckets {
		out = append(out, BucketCount{BucketKey: bucket.Key, Name: bucket.Name, Count: counts[bucket.Key]})
		seen[bucket.Key] = struct{}{}
	}
	for bucketKey, count := range counts {
		if _, ok := seen[bucketKey]; !ok {
			out = append(out, BucketCount{BucketKey: bucketKey, Count: count})
		}
	}
	return out
}

func likelyNextWork(tasks []domain.Task) []TaskSummary {
	candidates := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if task.BucketKey != "done" {
			candidates = append(candidates, task)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	if len(candidates) > nextWorkLimit {
		candidates = candidates[:nextWorkLimit]
	}
	return taskSummaries(candidates)
}

func blockedWork(tasks []domain.Task, dependencies []domain.TaskDependency) []TaskSummary {
	blocked := map[int64]struct{}{}
	for _, dependency := range dependencies {
		blocked[dependency.TaskID] = struct{}{}
	}
	out := []TaskSummary{}
	for _, task := range tasks {
		if _, ok := blocked[task.ID]; ok {
			out = append(out, taskSummary(task))
		}
	}
	return out
}

func taskTitleAndDescription(title, description string) (string, string) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title != "" {
		return title, description
	}
	if description == "" {
		return "", ""
	}
	line := strings.TrimSpace(strings.Split(description, "\n")[0])
	if len(line) > 90 {
		line = strings.TrimSpace(line[:90])
	}
	return line, description
}

func similarTasks(query string, tasks []domain.Task, limit int) []TaskSummary {
	queryWords := wordSet(query)
	if len(queryWords) == 0 {
		return nil
	}
	type match struct {
		task  domain.Task
		score float64
	}
	matches := []match{}
	queryLower := strings.ToLower(strings.TrimSpace(query))
	for _, task := range tasks {
		text := task.Title + " " + task.Description
		textLower := strings.ToLower(strings.TrimSpace(text))
		words := wordSet(text)
		score := overlapScore(queryWords, words)
		if textLower == queryLower || strings.Contains(textLower, queryLower) || strings.Contains(queryLower, strings.ToLower(task.Title)) {
			score = 1
		}
		if score >= 0.5 {
			matches = append(matches, match{task: task, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].task.ID < matches[j].task.ID
		}
		return matches[i].score > matches[j].score
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]TaskSummary, 0, len(matches))
	for _, match := range matches {
		out = append(out, taskSummary(match.task))
	}
	return out
}

func wordSet(value string) map[string]struct{} {
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := map[string]struct{}{}
	for _, word := range words {
		if len(word) < 3 || stopWords[word] {
			continue
		}
		out[word] = struct{}{}
	}
	return out
}

func overlapScore(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for word := range a {
		if _, ok := b[word]; ok {
			common++
		}
	}
	return float64(common) / float64(len(a))
}

var stopWords = map[string]bool{
	"and":  true,
	"are":  true,
	"for":  true,
	"from": true,
	"into": true,
	"the":  true,
	"this": true,
	"that": true,
	"with": true,
}
