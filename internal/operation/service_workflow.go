package operation

import "context"

func (s *Service) ShowWorkflow(ctx context.Context, input WorkflowInput) (WorkflowResponse, error) {
	if err := s.allow("workflow.show"); err != nil {
		return WorkflowResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return WorkflowResponse{}, err
	}
	workflow := s.snapshot.Workflow()
	return WorkflowResponse{Project: projectSummary(project), Workflow: workflowSummary(workflow)}, nil
}
