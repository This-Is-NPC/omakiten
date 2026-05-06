package agent

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/domain"
)

func (s *Service) ContinueTask(ctx context.Context, input ContinueTaskInput) (ContinueTaskResponse, error) {
	if input.TaskID <= 0 {
		return ContinueTaskResponse{}, domain.NewError(domain.ErrValidation, "task id must be positive", map[string]any{"task_id": input.TaskID})
	}

	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ContinueTaskResponse{}, err
	}

	tasks, err := app.NewTaskServiceFromStore(s.repo).List(ctx, project, domain.TaskFilter{})
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
	tasks, err := app.NewTaskServiceFromStore(s.repo).List(ctx, project, domain.TaskFilter{BucketKey: strings.TrimSpace(input.BucketKey)})
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
		tasks, err := app.NewTaskServiceFromStore(s.repo).List(ctx, project, domain.TaskFilter{})
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

	task, err := app.NewTaskServiceFromStore(s.repo).Add(ctx, project, title, description, strings.TrimSpace(input.Priority), strings.TrimSpace(input.BucketKey))
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
	task, err := app.NewTaskServiceFromStore(s.repo).Move(ctx, project, input.TaskID, input.BucketKey)
	if err != nil {
		return MoveTaskResponse{}, err
	}
	return MoveTaskResponse{Project: projectSummary(project), Task: taskSummary(task)}, nil
}
