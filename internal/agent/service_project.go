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
