package operation

import (
	"context"

	"omakiten/internal/contract"
)

func (s *Service) ShowWorkflow(ctx context.Context, input contract.WorkflowInput) (contract.WorkflowResponse, error) {
	if err := s.allow("workflow.show"); err != nil {
		return contract.WorkflowResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.WorkflowResponse{}, err
	}
	workflow := s.snapshot.Workflow()
	return contract.WorkflowResponse{Project: projectSummary(project), Workflow: workflowSummary(workflow)}, nil
}
