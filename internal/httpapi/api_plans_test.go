package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"listPlans", http.MethodGet, "/api/v1/projects/alpha/plans", "", "ListPlans", contract.ListPlansInput{ProjectSelector: alpha}},
	{"getPlan", http.MethodGet, "/api/v1/projects/alpha/plans/delivery", "", "ShowPlan", contract.ShowPlanInput{ProjectSelector: alpha, Slug: "delivery"}},
})

func (f *fakeOps) ListPlans(_ context.Context, in contract.ListPlansInput) (contract.ListPlansResponse, error) {
	f.record("ListPlans", in)
	return contract.ListPlansResponse{}, nil
}

func (f *fakeOps) ShowPlan(_ context.Context, in contract.ShowPlanInput) (contract.ShowPlanResponse, error) {
	f.record("ShowPlan", in)
	return contract.ShowPlanResponse{}, nil
}
