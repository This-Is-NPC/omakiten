package agent

import "context"

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
