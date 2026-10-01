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
	{"deletePlan", http.MethodDelete, "/api/v1/projects/alpha/plans/delivery?confirmed=true", "", "DeletePlan", contract.DeletePlanInput{ProjectSelector: alpha, Slug: "delivery", Confirmed: true}},
	{"getPlanContinuation", http.MethodGet, "/api/v1/projects/alpha/plans/delivery/continuation", "", "ContinuePlan", contract.ContinuePlanInput{ProjectSelector: alpha, Slug: "delivery"}},
	{"addPlanWave", http.MethodPost, "/api/v1/projects/alpha/plans/delivery/waves", `{"name":"Build","position":2}`, "AddPlanWave", contract.AddPlanWaveInput{ProjectSelector: alpha, Slug: "delivery", Name: "Build", Position: 2}},
	{"removePlanWave", http.MethodDelete, "/api/v1/projects/alpha/waves/3?confirmed=true", "", "RemovePlanWave", contract.RemovePlanWaveInput{ProjectSelector: alpha, WaveID: 3, Confirmed: true}},
	{"renamePlanWave", http.MethodPut, "/api/v1/projects/alpha/waves/3/name", `{"name":"Ship"}`, "RenamePlanWave", contract.RenamePlanWaveInput{ProjectSelector: alpha, WaveID: 3, Name: "Ship"}},
	{"reorderPlanWave", http.MethodPut, "/api/v1/projects/alpha/waves/3/position", `{"position":1}`, "ReorderPlanWave", contract.ReorderPlanWaveInput{ProjectSelector: alpha, WaveID: 3, Position: 1}},
	{"assignPlanTask", http.MethodPut, "/api/v1/projects/alpha/plans/delivery/tasks/5", `{"wave_id":3}`, "AssignPlanTask", contract.AssignPlanTaskInput{ProjectSelector: alpha, TaskID: 5, Slug: "delivery", WaveID: 3}},
	{"unassignPlanTask", http.MethodDelete, "/api/v1/projects/alpha/tasks/5/plan", "", "UnassignPlanTask", contract.UnassignPlanTaskInput{ProjectSelector: alpha, TaskID: 5}},
	{"claimNextPlanTask", http.MethodPost, "/api/v1/projects/alpha/plans/delivery/claims", "", "ClaimNextPlanTask", contract.ClaimNextPlanTaskInput{ProjectSelector: alpha, Slug: "delivery"}},
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

func (f *fakeOps) DeletePlan(_ context.Context, in contract.DeletePlanInput) (contract.DeletePlanResponse, error) {
	f.record("DeletePlan", in)
	return contract.DeletePlanResponse{}, nil
}

func (f *fakeOps) ContinuePlan(_ context.Context, in contract.ContinuePlanInput) (contract.ContinuePlanResponse, error) {
	f.record("ContinuePlan", in)
	return contract.ContinuePlanResponse{}, nil
}

func (f *fakeOps) AddPlanWave(_ context.Context, in contract.AddPlanWaveInput) (contract.AddPlanWaveResponse, error) {
	f.record("AddPlanWave", in)
	return contract.AddPlanWaveResponse{}, nil
}

func (f *fakeOps) RemovePlanWave(_ context.Context, in contract.RemovePlanWaveInput) (contract.RemovePlanWaveResponse, error) {
	f.record("RemovePlanWave", in)
	return contract.RemovePlanWaveResponse{}, nil
}

func (f *fakeOps) RenamePlanWave(_ context.Context, in contract.RenamePlanWaveInput) (contract.RenamePlanWaveResponse, error) {
	f.record("RenamePlanWave", in)
	return contract.RenamePlanWaveResponse{}, nil
}

func (f *fakeOps) ReorderPlanWave(_ context.Context, in contract.ReorderPlanWaveInput) (contract.ReorderPlanWaveResponse, error) {
	f.record("ReorderPlanWave", in)
	return contract.ReorderPlanWaveResponse{}, nil
}

func (f *fakeOps) AssignPlanTask(_ context.Context, in contract.AssignPlanTaskInput) (contract.AssignPlanTaskResponse, error) {
	f.record("AssignPlanTask", in)
	return contract.AssignPlanTaskResponse{}, nil
}

func (f *fakeOps) UnassignPlanTask(_ context.Context, in contract.UnassignPlanTaskInput) (contract.UnassignPlanTaskResponse, error) {
	f.record("UnassignPlanTask", in)
	return contract.UnassignPlanTaskResponse{}, nil
}

func (f *fakeOps) ClaimNextPlanTask(_ context.Context, in contract.ClaimNextPlanTaskInput) (contract.ClaimNextPlanTaskResponse, error) {
	f.record("ClaimNextPlanTask", in)
	return contract.ClaimNextPlanTaskResponse{}, nil
}
