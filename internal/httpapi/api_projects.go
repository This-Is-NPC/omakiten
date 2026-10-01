package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

// ProjectOperations reads registered projects and their workflow.
type ProjectOperations interface {
	ListProjects(ctx context.Context) ([]domain.Project, error)
	Overview(ctx context.Context, input contract.OverviewInput) (contract.OverviewResponse, error)
	ResumeProject(ctx context.Context, input contract.ResumeProjectInput) (contract.ResumeProjectResponse, error)
	ShowWorkflow(ctx context.Context, input contract.WorkflowInput) (contract.WorkflowResponse, error)
}

func (s *Server) projectRoutes() []route {
	return []route{
		query("listProjects", http.MethodGet, projectsPath, "project.list", "Registered projects.", nil, s.listProjects),
		query("getProject", http.MethodGet, projectPath, "project.overview", "Project overview with bucket counts.", []param{projectParam}, s.projectOverview),
		query("resumeProject", http.MethodGet, projectPath+"/resume", "project.resume", "Context to resume work on a project.", []param{projectParam}, s.resumeProject),
		query("getWorkflow", http.MethodGet, projectPath+"/workflow", "workflow.show", "Buckets, transitions, and permissions.", []param{projectParam}, s.workflow),
	}
}

func (s *Server) listProjects(r *http.Request) ([]domain.Project, error) {
	ops, err := s.opts.Runtimes.Global(r.Context())
	if err != nil {
		return nil, err
	}
	return ops.ListProjects(r.Context())
}

func (s *Server) projectOverview(r *http.Request) (contract.OverviewResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.OverviewResponse{}, err
	}
	return ops.Overview(r.Context(), contract.OverviewInput{ProjectSelector: selector})
}

func (s *Server) resumeProject(r *http.Request) (contract.ResumeProjectResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ResumeProjectResponse{}, err
	}
	return ops.ResumeProject(r.Context(), contract.ResumeProjectInput{ProjectSelector: selector})
}

func (s *Server) workflow(r *http.Request) (contract.WorkflowResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.WorkflowResponse{}, err
	}
	return ops.ShowWorkflow(r.Context(), contract.WorkflowInput{ProjectSelector: selector})
}
