package agent

import (
	"context"

	"omakiten/internal/app"
)

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
