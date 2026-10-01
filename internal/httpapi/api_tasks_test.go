package httpapi

import (
	"context"
	"net/http"
	"testing"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"getBoard", http.MethodGet, "/api/v1/projects/alpha/board", "", "TaskBoard", contract.TaskBoardInput{ProjectSelector: alpha}},
	{"listTasks", http.MethodGet, "/api/v1/projects/alpha/tasks?parent=12", "", "ListTasks", contract.ListTasksInput{
		ProjectSelector: alpha, ParentID: contract.OptionalInt64{Set: true, Value: ptr[int64](12)},
	}},
	{"createTask", http.MethodPost, "/api/v1/projects/alpha/tasks", `{"title":"T","description":"D","priority":"high","bucket_key":"dev","template_slug":"story","parent_id":3}`, "CreateTaskIntent", contract.CreateTaskInput{
		ProjectSelector: alpha, Title: "T", Description: "D", Priority: "high", BucketKey: "dev", TemplateSlug: "story", ParentID: ptr[int64](3),
	}},
	{"getTask", http.MethodGet, "/api/v1/projects/alpha/tasks/5", "", "ShowTask", contract.ShowTaskInput{ProjectSelector: alpha, TaskID: 5}},
	{"editTask", http.MethodPatch, "/api/v1/projects/alpha/tasks/5", `{"title":"New","parent_id":null}`, "EditTask", contract.EditTaskInput{
		ProjectSelector: alpha, TaskID: 5, Title: ptr("New"), ParentID: contract.OptionalInt64{Set: true},
	}},
	{"moveTask", http.MethodPost, "/api/v1/projects/alpha/tasks/5/transitions", `{"bucket_key":"review"}`, "MoveTask", contract.MoveTaskInput{ProjectSelector: alpha, TaskID: 5, BucketKey: "review"}},
	{"assignTask", http.MethodPut, "/api/v1/projects/alpha/tasks/5/assignee", `{"assignee":""}`, "AssignTask", contract.AssignTaskInput{ProjectSelector: alpha, TaskID: 5}},
	{"listTaskActivity", http.MethodGet, "/api/v1/projects/alpha/tasks/5/activity?order=desc", "", "ListTaskActivity", contract.ListTaskActivityInput{ProjectSelector: alpha, TaskID: 5, Order: "desc"}},
	{"deleteTask", http.MethodDelete, "/api/v1/projects/alpha/tasks/5?confirmed=true", "", "DeleteTask", contract.DeleteTaskInput{ProjectSelector: alpha, TaskID: 5, Confirmed: true}},
	{"archiveTask", http.MethodPost, "/api/v1/projects/alpha/tasks/5/archive", "", "ArchiveTask", contract.ArchiveTaskInput{ProjectSelector: alpha, TaskID: 5}},
	{"unarchiveTask", http.MethodPost, "/api/v1/projects/alpha/tasks/5/unarchive", "", "UnarchiveTask", contract.ArchiveTaskInput{ProjectSelector: alpha, TaskID: 5}},
	{"getTaskCheckpoint", http.MethodGet, "/api/v1/projects/alpha/tasks/5/checkpoint?include_workflow=false", "", "ContinueTask", contract.ContinueTaskInput{
		ProjectSelector: alpha, TaskID: 5, IncludeWorkflow: ptr(false),
	}},
})

// TestMalformedBooleanQueryNeverReachesOperations: a flag that is not a
// boolean is a 400 naming the parameter, and no operation runs.
func TestMalformedBooleanQueryNeverReachesOperations(t *testing.T) {
	ops := &fakeOps{}
	server, _ := newTestServer(t, ops, fakeLog{})
	rec := do(t, server, http.MethodDelete, "/api/v1/projects/alpha/tasks/5?confirmed=maybe", "", nil)
	env := decode(t, rec)
	if rec.Code != http.StatusBadRequest || env.Code != "invalid_parameter" || env.Details["parameter"] != "confirmed" {
		t.Fatalf("got %d %+v, want invalid_parameter naming confirmed", rec.Code, env)
	}
	if len(ops.calls) != 0 {
		t.Fatalf("operation ran: %+v", ops.calls)
	}
}

func (f *fakeOps) TaskBoard(_ context.Context, in contract.TaskBoardInput) (contract.TaskBoardResponse, error) {
	f.record("TaskBoard", in)
	return contract.TaskBoardResponse{}, nil
}

func (f *fakeOps) ListTasks(_ context.Context, in contract.ListTasksInput) (contract.ListTasksResponse, error) {
	if f.listTasks != nil {
		return f.listTasks(in)
	}
	f.record("ListTasks", in)
	return contract.ListTasksResponse{}, nil
}

func (f *fakeOps) ShowTask(_ context.Context, in contract.ShowTaskInput) (contract.ShowTaskResponse, error) {
	f.record("ShowTask", in)
	return contract.ShowTaskResponse{}, nil
}

func (f *fakeOps) CreateTaskIntent(_ context.Context, in contract.CreateTaskInput) (contract.CreateTaskResponse, error) {
	if f.createTask != nil {
		return f.createTask(in)
	}
	f.record("CreateTaskIntent", in)
	return contract.CreateTaskResponse{}, nil
}

func (f *fakeOps) EditTask(_ context.Context, in contract.EditTaskInput) (contract.EditTaskResponse, error) {
	f.record("EditTask", in)
	return contract.EditTaskResponse{}, nil
}

func (f *fakeOps) MoveTask(_ context.Context, in contract.MoveTaskInput) (contract.MoveTaskResponse, error) {
	f.record("MoveTask", in)
	return contract.MoveTaskResponse{}, nil
}

func (f *fakeOps) AssignTask(_ context.Context, in contract.AssignTaskInput) (contract.AssignTaskResponse, error) {
	f.record("AssignTask", in)
	return contract.AssignTaskResponse{}, nil
}

func (f *fakeOps) ListTaskActivity(_ context.Context, in contract.ListTaskActivityInput) (contract.ListTaskActivityResponse, error) {
	f.record("ListTaskActivity", in)
	return contract.ListTaskActivityResponse{}, nil
}

func (f *fakeOps) DeleteTask(_ context.Context, in contract.DeleteTaskInput) (contract.DeleteTaskResponse, error) {
	f.record("DeleteTask", in)
	return contract.DeleteTaskResponse{}, nil
}

func (f *fakeOps) ArchiveTask(_ context.Context, in contract.ArchiveTaskInput) (contract.ArchiveTaskResponse, error) {
	f.record("ArchiveTask", in)
	return contract.ArchiveTaskResponse{}, nil
}

func (f *fakeOps) UnarchiveTask(_ context.Context, in contract.ArchiveTaskInput) (contract.ArchiveTaskResponse, error) {
	f.record("UnarchiveTask", in)
	return contract.ArchiveTaskResponse{}, nil
}

func (f *fakeOps) ContinueTask(_ context.Context, in contract.ContinueTaskInput) (contract.ContinueTaskResponse, error) {
	f.record("ContinueTask", in)
	return contract.ContinueTaskResponse{}, nil
}
