package agent

import (
	"context"

	"omakiten/internal/app"
)

// contextRepoSet collects the four repository ports
// app.ContextService composes. The agent's single s.repo handle
// satisfies all four — the param object keeps the wiring readable
// instead of threading the same value in 4× at every callsite.
func (s *Service) contextRepoSet() app.ContextRepoSet {
	return app.ContextRepoSet{
		Tasks:        s.repo,
		Comments:     s.repo,
		Dependencies: s.repo,
		Entries:      s.repo,
	}
}

func (s *Service) AddContext(ctx context.Context, input AddContextInput) (ContextResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return ContextResponse{}, err
	}
	entry, err := app.NewContextServiceFromRepos(s.contextRepoSet(), s.snapshot, s.counter, s.registry).Add(ctx, project, input.Body)
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
		level = s.snapshot.ContextSettings().DefaultLevel
	}
	dump, err := app.NewContextServiceFromRepos(s.contextRepoSet(), s.snapshot, s.counter, s.registry).Dump(ctx, project, level)
	if err != nil {
		return DumpContextResponse{}, err
	}
	return DumpContextResponse{
		Project:             projectSummary(project),
		Level:               dump.Level,
		TaskCount:           dump.TaskCount,
		TokenMetrics:        dump.TokenMetrics,
		Context:             contextSnippets(dump.ContextEntries, 0),
		Workflow:            workflowSummary(dump.Workflow),
		Tasks:               taskSummaries(dump.Tasks, s.registry),
		Dependencies:        dependencySummaries(dump.Dependencies),
		Comments:            commentSummaries(dump.Comments),
		AgentOutputLanguage: dump.AgentOutputLanguage,
	}, nil
}
