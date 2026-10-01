package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listPlans", http.MethodGet, "/api/v1/projects/alpha/plans", "", "ListPlans", contract.ListPlansInput{ProjectSelector: alpha}},
	{"createPlan", http.MethodPost, "/api/v1/projects/alpha/plans", `{"slug":"delivery","name":"Delivery","goal_body":"Ship it."}`, "CreatePlan", contract.CreatePlanInput{ProjectSelector: alpha, Slug: "delivery", Name: "Delivery", GoalBody: "Ship it."}},
	{"getPlan", http.MethodGet, "/api/v1/projects/alpha/plans/delivery", "", "ShowPlan", contract.ShowPlanInput{ProjectSelector: alpha, Slug: "delivery"}},
	{"editPlan", http.MethodPatch, "/api/v1/projects/alpha/plans/delivery", `{"slug":"shipping","status":"done"}`, "EditPlan", contract.EditPlanInput{ProjectSelector: alpha, Slug: "delivery", NewSlug: ptr("shipping"), Status: ptr("done")}},
})

func (f *fakeOps) ListPlans(_ context.Context, in contract.ListPlansInput) (contract.ListPlansResponse, error) {
	f.record("ListPlans", in)
	return contract.ListPlansResponse{}, nil
}

func (f *fakeOps) ShowPlan(_ context.Context, in contract.ShowPlanInput) (contract.ShowPlanResponse, error) {
	f.record("ShowPlan", in)
	return contract.ShowPlanResponse{}, nil
}

func (f *fakeOps) CreatePlan(_ context.Context, in contract.CreatePlanInput) (contract.CreatePlanResponse, error) {
	f.record("CreatePlan", in)
	return contract.CreatePlanResponse{}, nil
}

func (f *fakeOps) EditPlan(_ context.Context, in contract.EditPlanInput) (contract.EditPlanResponse, error) {
	f.record("EditPlan", in)
	return contract.EditPlanResponse{}, nil
}
