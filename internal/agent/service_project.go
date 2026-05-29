package agent

import (
	"context"

	"omakiten/internal/app"
)

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
		PendingCount:   pendingCount(workflow, tasks),
		TaskBuckets:    bucketCounts(workflow, tasks),
		RecentContext:  contextSnippets(entries, s.settings.RecentContextLimit),
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
		LikelyNextWork: likelyNextWork(workflow, tasks, s.settings.NextWorkLimit, s.registry),
		BlockedWork:    blockedWork(tasks, dependencies, s.registry),
		Dependencies:   dependencySummaries(dependencies),
		RecentContext:  contextSnippets(entries, s.settings.RecentContextLimit),
		NextStepPrompt: "Choose a likely next task, inspect blocked work, or ask for `/okt-task-continue #<id>` context.",
	}, nil
}
