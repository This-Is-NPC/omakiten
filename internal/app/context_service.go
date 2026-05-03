package app

import (
	"context"

	"omakiten/internal/domain"
)

type ContextService struct {
	tasks  TaskRepository
	config ConfigRepository
}

func NewContextService(tasks TaskRepository, config ConfigRepository) *ContextService {
	return &ContextService{tasks: tasks, config: config}
}

func (s *ContextService) Dump(ctx context.Context, project domain.ProjectContext, level int) (domain.ContextDump, error) {
	if level < 1 || level > 3 {
		return domain.ContextDump{}, domain.NewError(domain.ErrValidation, "context level must be 1, 2, or 3", nil)
	}

	taskCount, err := s.tasks.TaskCount(ctx, project.ID)
	if err != nil {
		return domain.ContextDump{}, err
	}

	dump := domain.ContextDump{Project: project, Level: level, TaskCount: taskCount}

	if level >= 2 {
		tasks, err := s.tasks.ListTasks(ctx, project.ID, domain.TaskFilter{})
		if err != nil {
			return domain.ContextDump{}, err
		}
		dump.Tasks = tasks
	}

	if level >= 3 {
		laws, err := s.config.ListActiveLaws(ctx)
		if err != nil {
			return domain.ContextDump{}, err
		}
		dump.Laws = laws
	}

	return dump, nil
}
