package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
)

var _ = mapping([]mappingCase{
	{"listProjects", http.MethodGet, "/api/v1/projects", "", "ListProjects", nil},
	{"getProject", http.MethodGet, "/api/v1/projects/alpha", "", "Overview", contract.OverviewInput{ProjectSelector: alpha}},
	{"resumeProject", http.MethodGet, "/api/v1/projects/alpha/resume", "", "ResumeProject", contract.ResumeProjectInput{ProjectSelector: alpha}},
	{"getWorkflow", http.MethodGet, "/api/v1/projects/alpha/workflow", "", "ShowWorkflow", contract.WorkflowInput{ProjectSelector: alpha}},
})

func (f *fakeOps) ListProjects(context.Context) ([]domain.Project, error) {
	f.record("ListProjects", nil)
	return []domain.Project{{ID: 7, Name: "Alpha", Slug: "alpha"}}, nil
}

func (f *fakeOps) Overview(_ context.Context, in contract.OverviewInput) (contract.OverviewResponse, error) {
	f.record("Overview", in)
	return contract.OverviewResponse{}, nil
}

func (f *fakeOps) ResumeProject(_ context.Context, in contract.ResumeProjectInput) (contract.ResumeProjectResponse, error) {
	f.record("ResumeProject", in)
	return contract.ResumeProjectResponse{}, nil
}

func (f *fakeOps) ShowWorkflow(_ context.Context, in contract.WorkflowInput) (contract.WorkflowResponse, error) {
	f.record("ShowWorkflow", in)
	return contract.WorkflowResponse{}, nil
}
