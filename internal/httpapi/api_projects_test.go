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
	{"editProject", http.MethodPatch, "/api/v1/projects/alpha", `{"description":"Board for agents"}`, "EditProject", contract.EditProjectInput{ProjectSelector: alpha, Description: "Board for agents"}},
	{"deleteProject", http.MethodDelete, "/api/v1/projects/alpha?confirmed=true", "", "RemoveProject", contract.RemoveProjectInput{ProjectSelector: alpha, Confirmed: true}},
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

func (f *fakeOps) EditProject(_ context.Context, in contract.EditProjectInput) (contract.EditProjectResponse, error) {
	f.record("EditProject", in)
	return contract.EditProjectResponse{}, nil
}

func (f *fakeOps) ResumeProject(_ context.Context, in contract.ResumeProjectInput) (contract.ResumeProjectResponse, error) {
	f.record("ResumeProject", in)
	return contract.ResumeProjectResponse{}, nil
}

func (f *fakeOps) ShowWorkflow(_ context.Context, in contract.WorkflowInput) (contract.WorkflowResponse, error) {
	f.record("ShowWorkflow", in)
	return contract.WorkflowResponse{}, nil
}

func (f *fakeOps) RemoveProject(_ context.Context, in contract.RemoveProjectInput) (contract.RemoveProjectResponse, error) {
	f.record("RemoveProject", in)
	return contract.RemoveProjectResponse{}, nil
}
