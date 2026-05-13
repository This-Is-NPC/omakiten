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

	tasks, err := app.NewTaskServiceFromStore(s.repo, s.registry).List(ctx, project, domain.TaskFilter{})
	if err != nil {
		return ContinueTaskResponse{}, err
	}
	task, ok := findTask(tasks, input.TaskID)
	if !ok {
		return ContinueTaskResponse{}, domain.NewError(domain.ErrTaskNotFound, "task not found in active project", map[string]any{"task_id": input.TaskID, "project_id": project.ID})
	}

	// Workflow shape is heavy (~150 tokens) and rarely changes mid-session.
	// Skip the lookup when the caller has opted out via include_workflow=false
	// or when config.mcp.include_workflow_in_continue is false and the caller
	// did not override.
	includeWorkflow := s.settings.IncludeWorkflow
	if input.IncludeWorkflow != nil {
		includeWorkflow = *input.IncludeWorkflow
	}
	var workflowSum WorkflowSummary
	if includeWorkflow {
		workflow, err := s.repo.ActiveWorkflow(ctx)
		if err != nil {
			return ContinueTaskResponse{}, err
		}
		workflowSum = workflowSummary(workflow)
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
		Task:           taskSummary(task, s.registry),
		Workflow:       workflowSum,
		Dependencies:   dependencySummaries(dependencies),
		Comments:       s.shapedRecentComments(comments),
		RecentContext:  contextSnippets(entries, s.settings.RecentContextLimit),
		NextStepPrompt: fmt.Sprintf("Continue task #%d from this checkpoint, then record material progress with `progress.record`.", task.ID),
	}, nil
}

// shapedRecentComments applies the configured recent-comment cap and the
// per-comment body truncation in one place so every endpoint that ships
// comments uses the same shaping rules.
func (s *Service) shapedRecentComments(comments []domain.Comment) []CommentSummary {
	out := recentComments(comments, s.settings.RecentCommentLimit)
	if s.settings.MaxCommentChars > 0 {
		for i := range out {
			out[i].Body = truncateBody(out[i].Body, s.settings.MaxCommentChars)
		}
	}
	return out
}

func (s *Service) ListTasks(ctx context.Context, input ListTasksInput) (ListTasksResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ListTasksResponse{}, err
	}
	tasks, err := app.NewTaskServiceFromStore(s.repo, s.registry).List(ctx, project, domain.TaskFilter{BucketKey: strings.TrimSpace(input.BucketKey)})
	if err != nil {
		return ListTasksResponse{}, err
	}
	return ListTasksResponse{Project: projectSummary(project), Tasks: taskSummaries(tasks, s.registry)}, nil
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
	if input.TemplateSlug != "" {
		merged, applied, err := s.applyTemplateBody(input.TemplateSlug, description, "task")
		if err != nil {
			return CreateTaskResponse{}, err
		}
		description = merged
		template = applied
	}

	if !input.SkipSimilarityCheck && !input.Confirmed {
		tasks, err := app.NewTaskServiceFromStore(s.repo, s.registry).List(ctx, project, domain.TaskFilter{})
		if err != nil {
			return CreateTaskResponse{}, err
		}
		similar := similarTasks(title+" "+description, tasks, s.settings.SimilarTaskLimit, s.registry)
		if len(similar) > 0 {
			return CreateTaskResponse{
				Project:      projectSummary(project),
				SimilarTasks: similar,
				Template:     template,
				Confirmation: Confirmation{
					RequiresConfirmation: true,
					// The Reason text is the load-bearing instruction the agent
					// acts on — it is intentionally self-explanatory so prompt
					// resolution does not need an `if returns requires_confirmation`
					// branch in its action text. Keep it imperative, name the
					// next-step tools, and surface the choice to the user.
					Reason: "Similar tasks already exist in this project. Surface them to the user verbatim and ask " +
						"whether to continue an existing one (call `tasks.continue` with the chosen id) or create a " +
						"separate task (call `tasks.create_intent` again with the same description and `confirmed=true`).",
					Options: []ConfirmationOption{
						{Action: "continue_existing", Label: "Continue one of the similar tasks"},
						{Action: "create_separate", Label: "Create a separate task with confirmed=true"},
					},
				},
			}, nil
		}
	}

	task, err := app.NewTaskServiceFromStore(s.repo, s.registry).Add(ctx, project, title, description, strings.TrimSpace(input.Priority), strings.TrimSpace(input.BucketKey))
	if err != nil {
		return CreateTaskResponse{}, err
	}
	summary := taskSummary(task, s.registry)
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

// EditTask exposes app.TaskService.Edit through MCP. The handler does
// nothing more than serialize input → invoke the service → serialize
// output: every policy decision (bucket permissions, archive gate,
// priority registry lookup) lives in the service so the MCP surface
// carries no canonical defaults of its own.
func (s *Service) EditTask(ctx context.Context, input EditTaskInput) (EditTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return EditTaskResponse{}, err
	}
	update := domain.TaskUpdate{}
	if input.Title != nil {
		update.Title = input.Title
	}
	if input.Description != nil {
		update.Description = input.Description
	}
	if input.Priority != nil {
		label := strings.TrimSpace(*input.Priority)
		if label == "" {
			return EditTaskResponse{}, domain.NewError(domain.ErrValidation,
				"priority must be a non-empty label when provided; omit the field to leave it unchanged",
				map[string]any{"priority": *input.Priority})
		}
		p, ok := s.registry.PriorityFromLabel(label)
		if !ok {
			return EditTaskResponse{}, domain.NewError(domain.ErrValidation,
				"unknown priority label; must match a value in config.priorities",
				map[string]any{"priority": label})
		}
		update.Priority = &p
	}
	task, err := app.NewTaskServiceFromStore(s.repo, s.registry).Edit(ctx, project, input.TaskID, update)
	if err != nil {
		return EditTaskResponse{}, err
	}
	return EditTaskResponse{Project: projectSummary(project), Task: taskSummary(task, s.registry)}, nil
}

func (s *Service) MoveTask(ctx context.Context, input MoveTaskInput) (MoveTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return MoveTaskResponse{}, err
	}
	task, err := app.NewTaskServiceFromStore(s.repo, s.registry).Move(ctx, project, input.TaskID, input.BucketKey)
	if err != nil {
		return MoveTaskResponse{}, err
	}
	return MoveTaskResponse{Project: projectSummary(project), Task: taskSummary(task, s.registry)}, nil
}

func (s *Service) DeleteTask(ctx context.Context, input DeleteTaskInput) (DeleteTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return DeleteTaskResponse{}, err
	}
	if !input.Confirmed {
		return DeleteTaskResponse{
			Project: projectSummary(project),
			Confirmation: Confirmation{
				RequiresConfirmation: true,
				Reason:               "Deleting a task is destructive and cascades to comments, tags, dependencies, and events. Confirm with confirmed=true to proceed; consider tasks.archive instead for a reversible alternative.",
				Options: []ConfirmationOption{
					{Action: "archive_instead", Label: "Call tasks.archive(task_id) — reversible escape hatch"},
					{Action: "confirm_delete", Label: "Retry tasks.delete with confirmed=true to hard-delete"},
				},
			},
		}, nil
	}
	event, err := app.NewTaskServiceFromStore(s.repo, s.registry).Delete(ctx, project, input.TaskID)
	if err != nil {
		return DeleteTaskResponse{}, err
	}
	snapshot := eventSummary(event)
	return DeleteTaskResponse{Project: projectSummary(project), Snapshot: &snapshot}, nil
}

func (s *Service) ArchiveTask(ctx context.Context, input ArchiveTaskInput) (ArchiveTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ArchiveTaskResponse{}, err
	}
	task, _, err := app.NewTaskServiceFromStore(s.repo, s.registry).Archive(ctx, project, input.TaskID)
	if err != nil {
		return ArchiveTaskResponse{}, err
	}
	return ArchiveTaskResponse{Project: projectSummary(project), Task: taskSummary(task, s.registry)}, nil
}

func (s *Service) UnarchiveTask(ctx context.Context, input ArchiveTaskInput) (ArchiveTaskResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ArchiveTaskResponse{}, err
	}
	task, _, err := app.NewTaskServiceFromStore(s.repo, s.registry).Unarchive(ctx, project, input.TaskID)
	if err != nil {
		return ArchiveTaskResponse{}, err
	}
	return ArchiveTaskResponse{Project: projectSummary(project), Task: taskSummary(task, s.registry)}, nil
}
