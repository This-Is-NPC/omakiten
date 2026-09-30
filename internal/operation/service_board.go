package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/taskprojection"
)

// TaskBoard reads a project's workflow and active tasks with the counts the
// TUI board paints, from the same snapshot and projection. It lists tasks,
// so the task.list surface gates it.
func (s *Service) TaskBoard(ctx context.Context, input contract.TaskBoardInput) (contract.TaskBoardResponse, error) {
	if err := s.allow("task.list"); err != nil {
		return contract.TaskBoardResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.TaskBoardResponse{}, err
	}
	snap, err := app.NewTUIQueryService(s.repo, s.snapshot, s.repo, s.repo, s.repo).Snapshot(ctx, project, domain.TaskSort{})
	if err != nil {
		return contract.TaskBoardResponse{}, err
	}
	source := taskprojection.Input{
		Tasks:        snap.Tasks,
		Workflow:     snap.Workflow,
		Dependencies: snap.Dependencies,
		Comments:     snap.Comments,
	}
	if s.snapshot != nil {
		source.Priorities = s.snapshot.Priorities()
	}
	projection := taskprojection.Build(source)
	tasks := projection.Tasks(taskprojection.Query{})
	board := make([]contract.BoardTask, 0, len(tasks))
	for _, task := range tasks {
		counts := projection.Badges(task.ID)
		row := contract.BoardTask{
			TaskSummary: taskSummary(task, s.registry),
			Blockers:    counts.Blockers,
			Comments:    counts.Comments,
			Subtasks:    counts.Subtasks,
		}
		if definition, ok := projection.Priority(task.Priority); ok {
			row.PriorityColor = definition.Color
		}
		for _, tag := range snap.TaskTagsByID[task.ID] {
			row.Tags = append(row.Tags, tag.Label)
		}
		board = append(board, row)
	}
	priorities := make([]contract.BoardPriority, 0, len(source.Priorities))
	for _, priority := range source.Priorities {
		priorities = append(priorities, contract.BoardPriority{Value: priority.Value, Color: priority.Color, Default: priority.Default})
	}
	return contract.TaskBoardResponse{
		Project:    projectSummary(project),
		Workflow:   workflowSummary(snap.Workflow),
		Tasks:      board,
		Priorities: priorities,
	}, nil
}
