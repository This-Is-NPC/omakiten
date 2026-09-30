package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"omakiten/internal/contract"
)

func ptr[T any](v T) *T { return &v }

// TestRoutesMapRequestsOntoOperationInputs pins, for every JSON route, the
// operation it calls and the input built from the path, query, and body.
// The project always comes from the path.
func TestRoutesMapRequestsOntoOperationInputs(t *testing.T) {
	alpha := contract.ProjectSelector{ProjectID: 7}
	cases := []struct {
		id     string
		method string
		target string
		body   string
		call   string
		input  any
	}{
		{"listProjects", http.MethodGet, "/api/v1/projects", "", "ListProjects", nil},
		{"getProject", http.MethodGet, "/api/v1/projects/alpha", "", "Overview", contract.OverviewInput{ProjectSelector: alpha}},
		{"resumeProject", http.MethodGet, "/api/v1/projects/alpha/resume", "", "ResumeProject", contract.ResumeProjectInput{ProjectSelector: alpha}},
		{"getWorkflow", http.MethodGet, "/api/v1/projects/alpha/workflow", "", "ShowWorkflow", contract.WorkflowInput{ProjectSelector: alpha}},
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
		{"listTaskComments", http.MethodGet, "/api/v1/projects/alpha/tasks/5/comments", "", "ListComments", contract.ListCommentsInput{ProjectSelector: alpha, TaskID: 5}},
		{"addTaskComment", http.MethodPost, "/api/v1/projects/alpha/tasks/5/comments", `{"body":"B","title":"Ti","kind":"note","pinned":true,"tags":["a"],"template_slug":"tpl"}`, "AddComment", contract.AddCommentInput{
			ProjectSelector: alpha, TaskID: 5, Body: "B", Title: "Ti", Kind: "note", Pinned: true, AuthorType: "human", Tags: []string{"a"}, TemplateSlug: "tpl",
		}},
		{"listTaskActivity", http.MethodGet, "/api/v1/projects/alpha/tasks/5/activity?order=desc", "", "ListTaskActivity", contract.ListTaskActivityInput{ProjectSelector: alpha, TaskID: 5, Order: "desc"}},
		{"listDependencies", http.MethodGet, "/api/v1/projects/alpha/dependencies?task=5", "", "ListDependencies", contract.ListDependenciesInput{ProjectSelector: alpha, TaskID: 5}},
		{"listPlans", http.MethodGet, "/api/v1/projects/alpha/plans", "", "ListPlans", contract.ListPlansInput{ProjectSelector: alpha}},
		{"getPlan", http.MethodGet, "/api/v1/projects/alpha/plans/delivery", "", "ShowPlan", contract.ShowPlanInput{ProjectSelector: alpha, Slug: "delivery"}},
		{"search", http.MethodGet, "/api/v1/projects/alpha/search?q=login+bug&type=task,comment", "", "Search", contract.SearchInput{ProjectSelector: alpha, Query: "login bug", EntityTypes: []string{"task", "comment"}}},
		{"listLogs", http.MethodGet, "/api/v1/projects/alpha/logs?category=task&category=plan&since=7d&limit=10&order=asc", "", "ListLogs", contract.ListLogsInput{
			ProjectSelector: alpha, Categories: []string{"task", "plan"}, Since: "7d", Limit: 10, Order: "asc",
		}},
		{"getInsights", http.MethodGet, "/api/v1/projects/alpha/insights?stuck_days=3", "", "InsightsSummary", contract.InsightsSummaryInput{ProjectSelector: alpha, StuckDays: 3}},
		{"getMetrics", http.MethodGet, "/api/v1/projects/alpha/metrics?period=7d", "", "MetricsSummary", contract.MetricsSummaryInput{ProjectSelector: alpha, Period: "7d", ProjectID: 7}},
	}

	covered := map[string]bool{}
	for _, tc := range cases {
		covered[tc.id] = true
		t.Run(tc.id, func(t *testing.T) {
			ops := &fakeOps{}
			server, _ := newTestServer(t, ops, fakeLog{})
			rec := do(t, server, tc.method, tc.target, tc.body, nil)
			if env := decode(t, rec); rec.Code != http.StatusOK || !env.OK {
				t.Fatalf("status %d %s", rec.Code, rec.Body.String())
			}
			if len(ops.calls) != 1 || ops.calls[0].method != tc.call {
				t.Fatalf("calls = %+v, want one %s", ops.calls, tc.call)
			}
			if diff := cmp.Diff(tc.input, ops.calls[0].input); diff != "" {
				t.Fatalf("%s input (-want +got):\n%s", tc.call, diff)
			}
		})
	}
	assertEveryJSONRouteCovered(t, covered)
}

// assertEveryJSONRouteCovered fails when a route is added without a mapping case.
func assertEveryJSONRouteCovered(t *testing.T, covered map[string]bool) {
	t.Helper()
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	for _, rt := range server.routes() {
		if rt.stream == nil && !rt.public && rt.id != "getCatalog" && !covered[rt.id] {
			t.Errorf("route %s has no mapping case", rt.id)
		}
	}
}

// TestInvalidParametersNeverReachOperations covers every parser the routes
// share: a malformed id or number is a 400 and no operation runs.
func TestInvalidParametersNeverReachOperations(t *testing.T) {
	for _, target := range []string{
		"/api/v1/projects/alpha/tasks/abc",
		"/api/v1/projects/alpha/tasks/0",
		"/api/v1/projects/alpha/tasks/-4/comments",
		"/api/v1/projects/alpha/dependencies?task=x",
		"/api/v1/projects/alpha/logs?limit=ten",
		"/api/v1/projects/alpha/insights?stuck_days=1.5",
	} {
		t.Run(target, func(t *testing.T) {
			ops := &fakeOps{}
			server, _ := newTestServer(t, ops, fakeLog{})
			rec := do(t, server, http.MethodGet, target, "", nil)
			env := decode(t, rec)
			if rec.Code != http.StatusBadRequest || env.Code != "invalid_parameter" || env.Details["parameter"] == nil {
				t.Fatalf("got %d %+v, want invalid_parameter naming the parameter", rec.Code, env)
			}
			if len(ops.calls) != 0 {
				t.Fatalf("operation ran: %+v", ops.calls)
			}
		})
	}
}

// TestProjectRoutesResolveTheProjectFirst: every project route answers an
// unknown slug with project_not_found before any operation runs.
func TestProjectRoutesResolveTheProjectFirst(t *testing.T) {
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	fill := strings.NewReplacer("{project}", "ghost", "{task}", "5", "{plan}", "delivery")
	for _, rt := range server.routes() {
		if !strings.Contains(rt.path, "{project}") {
			continue
		}
		t.Run(rt.id, func(t *testing.T) {
			ops := &fakeOps{}
			server, _ := newTestServer(t, ops, fakeLog{})
			body := ""
			if rt.body != nil {
				body = "{}"
			}
			rec := do(t, server, rt.method, fill.Replace(rt.path), body, nil)
			if rec.Code != http.StatusNotFound || decode(t, rec).Code != "project_not_found" {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			if len(ops.calls) != 0 {
				t.Fatalf("operation ran: %+v", ops.calls)
			}
		})
	}
}

func TestCatalogReturnsGUILanguageEntries(t *testing.T) {
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	rec := do(t, server, http.MethodGet, "/api/v1/catalog", "", nil)
	env := decode(t, rec)
	if rec.Code != http.StatusOK || !strings.Contains(string(env.Data), `"language":"en"`) || !strings.Contains(string(env.Data), `"http.error.unauthorized":"A valid Bearer token is required."`) {
		t.Fatalf("catalog = %d %s", rec.Code, rec.Body.String())
	}
}
