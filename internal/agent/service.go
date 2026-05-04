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
}

type Service struct {
	repo     Repository
	selector ProjectSelector
	counter  token.Counter
}

func NewService(repo Repository, selector ProjectSelector) *Service {
	return &Service{repo: repo, selector: selector, counter: token.NewCounter()}
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
	return CreateTaskResponse{Project: projectSummary(project), Task: &summary}, nil
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
	comment, err := app.NewCommentService(s.repo).Add(ctx, project, input.TaskID, input.Body, input.AuthorType)
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
		comment, err := app.NewCommentService(s.repo).Add(ctx, project, input.TaskID, input.Comment, input.AuthorType)
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
