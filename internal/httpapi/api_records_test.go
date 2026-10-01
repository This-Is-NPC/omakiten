package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"recordError", http.MethodPost, "/api/v1/projects/alpha/errors", `{"description":"D","context":"C","tags":["a"]}`, "RecordError", contract.RecordErrorInput{
		ProjectSelector: alpha, Description: "D", Context: "C", Tags: []string{"a"},
	}},
	{"addSolution", http.MethodPost, "/api/v1/projects/alpha/errors/3/solutions", `{"description":"D","steps":"S","task_id":5}`, "AddSolution", contract.AddSolutionInput{
		ProjectSelector: alpha, ErrorID: 3, Description: "D", Steps: "S", TaskID: 5,
	}},
	{"confirmSolution", http.MethodPost, "/api/v1/projects/alpha/solutions/4/confirmations", `{"success":true}`, "ConfirmSolution", contract.ConfirmSolutionInput{ProjectSelector: alpha, SolutionID: 4, Success: true}},
	{"listTopSolutions", http.MethodGet, "/api/v1/projects/alpha/solutions?limit=3", "", "ListTopSolutions", contract.ListTopSolutionsInput{ProjectSelector: alpha, Limit: 3}},
	{"recordProgress", http.MethodPost, "/api/v1/projects/alpha/tasks/5/progress", `{"title":"T","priority":"high","move_to_bucket":"done","comment":"C"}`, "RecordProgress", contract.RecordProgressInput{
		ProjectSelector: alpha, TaskID: 5, Title: ptr("T"), Priority: ptr("high"), MoveToBucket: "done", Comment: "C", AuthorType: "human",
	}},
})

func (f *fakeOps) RecordError(_ context.Context, in contract.RecordErrorInput) (contract.ErrorRecordResponse, error) {
	f.record("RecordError", in)
	return contract.ErrorRecordResponse{}, nil
}

func (f *fakeOps) AddSolution(_ context.Context, in contract.AddSolutionInput) (contract.SolutionResponse, error) {
	f.record("AddSolution", in)
	return contract.SolutionResponse{}, nil
}

func (f *fakeOps) ConfirmSolution(_ context.Context, in contract.ConfirmSolutionInput) (contract.SolutionResponse, error) {
	f.record("ConfirmSolution", in)
	return contract.SolutionResponse{}, nil
}

func (f *fakeOps) ListTopSolutions(_ context.Context, in contract.ListTopSolutionsInput) (contract.TopSolutionsResponse, error) {
	f.record("ListTopSolutions", in)
	return contract.TopSolutionsResponse{}, nil
}

func (f *fakeOps) RecordProgress(_ context.Context, in contract.RecordProgressInput) (contract.RecordProgressResponse, error) {
	f.record("RecordProgress", in)
	return contract.RecordProgressResponse{}, nil
}
