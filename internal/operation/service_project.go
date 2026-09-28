package operation

import (
	"context"

	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// ListProjects discovers registered projects before a caller selects a scope.
func (s *Service) ListProjects(ctx context.Context) ([]domain.Project, error) {
	if err := s.allow("project.list"); err != nil {
		return nil, err
	}
	return s.repo.ListProjects(ctx)
}

func (s *Service) Overview(ctx context.Context, input contract.OverviewInput) (contract.OverviewResponse, error) {
	if err := s.allow("project.overview"); err != nil {
		return contract.OverviewResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.OverviewResponse{}, err
	}

	tasks, workflow, err := s.projectState(ctx, project)
	if err != nil {
		return contract.OverviewResponse{}, err
	}

	return contract.OverviewResponse{
		Project:        projectSummary(project),
		Workflow:       workflowSummary(workflow),
		PendingCount:   pendingCount(workflow, tasks),
		TaskBuckets:    bucketCounts(workflow, tasks),
		NextStepPrompt: "Omakiten is ready. Ask for task details, continue the latest checkpoint, or create a new task intent.",
	}, nil
}

func (s *Service) ResumeProject(ctx context.Context, input contract.ResumeProjectInput) (contract.ResumeProjectResponse, error) {
	if err := s.allow("project.resume"); err != nil {
		return contract.ResumeProjectResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ResumeProjectResponse{}, err
	}

	tasks, workflow, err := s.projectState(ctx, project)
	if err != nil {
		return contract.ResumeProjectResponse{}, err
	}
	dependencies, err := app.NewDependencyService(s.repo).List(ctx, project, 0)
	if err != nil {
		return contract.ResumeProjectResponse{}, err
	}

	return contract.ResumeProjectResponse{
		Project:        projectSummary(project),
		Workflow:       workflowSummary(workflow),
		TaskBuckets:    bucketCounts(workflow, tasks),
		LikelyNextWork: likelyNextWork(workflow, tasks, s.settings.NextWorkLimit, s.registry),
		BlockedWork:    blockedWork(tasks, dependencies, s.registry),
		Dependencies:   dependencySummaries(dependencies),
		NextStepPrompt: "Choose a likely next task, inspect blocked work, or ask for `/okt-task-continue #<id>` context.",
	}, nil
}
