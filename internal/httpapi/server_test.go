package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/testutil"
)

const testToken = "secret-token"

type fakeOps struct {
	Operations
	listTasks  func(contract.ListTasksInput) (contract.ListTasksResponse, error)
	createTask func(contract.CreateTaskInput) (contract.CreateTaskResponse, error)
	listLogs   func(contract.ListLogsInput) (contract.ListLogsResponse, error)
}

func (f *fakeOps) ListProjects(context.Context) ([]domain.Project, error) {
	return []domain.Project{{ID: 7, Name: "Alpha", Slug: "alpha"}}, nil
}

func (f *fakeOps) ListTasks(_ context.Context, input contract.ListTasksInput) (contract.ListTasksResponse, error) {
	return f.listTasks(input)
}

func (f *fakeOps) CreateTaskIntent(_ context.Context, input contract.CreateTaskInput) (contract.CreateTaskResponse, error) {
	return f.createTask(input)
}

func (f *fakeOps) ListLogs(_ context.Context, input contract.ListLogsInput) (contract.ListLogsResponse, error) {
	if f.listLogs == nil {
		return contract.ListLogsResponse{}, nil
	}
	return f.listLogs(input)
}

type fakeRuntimes struct {
	ops      *fakeOps
	projects map[string]int64
	catalog  *config.Catalog
}

func (f fakeRuntimes) Project(_ context.Context, slug string) (Operations, contract.ProjectSelector, error) {
	id, ok := f.projects[slug]
	if !ok {
		return nil, contract.ProjectSelector{}, domain.NewError(domain.ErrProjectNotFound, "project not found", nil)
	}
	return f.ops, contract.ProjectSelector{ProjectID: id}, nil
}

func (f fakeRuntimes) Global(context.Context) (Operations, error) { return f.ops, nil }

func (f fakeRuntimes) Catalog() *config.Catalog { return f.catalog }

type fakeLog struct {
	rows []contract.LogsRow
}

func (f fakeLog) EventsAfter(_ context.Context, afterID int64, limit int) ([]contract.LogsRow, error) {
	var out []contract.LogsRow
	for _, row := range f.rows {
		if row.ID > afterID && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func newTestServer(t *testing.T, ops *fakeOps, log fakeLog) (*Server, *Hub) {
	t.Helper()
	en := &config.Language{Code: "en", Keys: map[string]string{
		"http.error.unauthorized": "A valid Bearer token is required.",
		"denied.reason":           "Resolved denial reason.",
	}}
	hub := NewHub()
	server, err := New(Options{
		Token:    testToken,
		Version:  "test",
		Runtimes: fakeRuntimes{ops: ops, projects: map[string]int64{"alpha": 7, "beta": 8}, catalog: config.NewCatalog(nil, en)},
		Hub:      hub,
		Log:      log,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server, hub
}

func do(t *testing.T, handler http.Handler, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:9000"+target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	for key, value := range header {
		if value == "" {
			req.Header.Del(key)
		} else {
			req.Header.Set(key, value)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type envelope struct {
	OK      bool            `json:"ok"`
	Data    json.RawMessage `json:"data"`
	Code    string          `json:"code"`
	Message string          `json:"msg"`
	Details map[string]any  `json:"details"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return env
}

func TestOpenAPIDocumentMatchesGolden(t *testing.T) {
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	testutil.Golden(t, "openapi.json", string(server.spec))
}

func TestRouteSurfacesExistInCensus(t *testing.T) {
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	census := map[string]bool{}
	for _, entry := range config.CanonicalSurfaceCensus {
		census[entry.Slug] = true
	}
	ids := map[string]bool{}
	for _, rt := range server.routes() {
		if rt.slug != "" && !census[rt.slug] {
			t.Errorf("route %s names unknown surface slug %q", rt.id, rt.slug)
		}
		if ids[rt.id] {
			t.Errorf("duplicate operationId %q", rt.id)
		}
		ids[rt.id] = true
	}
}

func TestAuthenticationAndHostChecks(t *testing.T) {
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})

	if rec := do(t, server, http.MethodGet, "/health", "", map[string]string{"Authorization": ""}); rec.Code != http.StatusOK {
		t.Fatalf("health without token = %d, want 200", rec.Code)
	}
	rec := do(t, server, http.MethodGet, "/api/v1/projects", "", map[string]string{"Authorization": "Bearer wrong"})
	if env := decode(t, rec); rec.Code != http.StatusUnauthorized || env.Code != "unauthorized" || env.Message != "A valid Bearer token is required." {
		t.Fatalf("wrong token = %d %+v", rec.Code, env)
	}
	req := httptest.NewRequest(http.MethodGet, "http://evil.example/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	hostRec := httptest.NewRecorder()
	server.ServeHTTP(hostRec, req)
	if env := decode(t, hostRec); hostRec.Code != http.StatusForbidden || env.Code != "host_rejected" {
		t.Fatalf("foreign host = %d %+v", hostRec.Code, env)
	}
	unknown := do(t, server, http.MethodGet, "/api/v1/nope", "", nil)
	if env := decode(t, unknown); unknown.Code != http.StatusNotFound || env.Code != "route_not_found" || env.Message != "http.error.route_not_found" {
		t.Fatalf("unknown route = %d %+v (missing catalog key returns the key)", unknown.Code, env)
	}
}

func TestListTasksPinsPathProjectAndParsesFilters(t *testing.T) {
	var got contract.ListTasksInput
	ops := &fakeOps{listTasks: func(input contract.ListTasksInput) (contract.ListTasksResponse, error) {
		got = input
		return contract.ListTasksResponse{Tasks: []contract.TaskSummary{{ID: 3, Title: "x"}}}, nil
	}}
	server, _ := newTestServer(t, ops, fakeLog{})

	rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/tasks?bucket=dev&parent=root", "", nil)
	if env := decode(t, rec); rec.Code != http.StatusOK || !env.OK || !strings.Contains(string(env.Data), `"id":3`) {
		t.Fatalf("list tasks = %d %s", rec.Code, rec.Body.String())
	}
	if got.ProjectID != 7 || got.BucketKey != "dev" || !got.ParentID.Set || got.ParentID.Value != nil {
		t.Fatalf("input = %+v", got)
	}
	if rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/tasks?parent=x", "", nil); rec.Code != http.StatusBadRequest || decode(t, rec).Code != "invalid_parameter" {
		t.Fatalf("bad parent = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, server, http.MethodGet, "/api/v1/projects/ghost/tasks", "", nil); rec.Code != http.StatusNotFound || decode(t, rec).Code != "project_not_found" {
		t.Fatalf("unknown project = %d %s", rec.Code, rec.Body.String())
	}
}

func TestOperationErrorsMapToStatusAndResolvedMessages(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		msg    string
	}{
		{"denied", operation.OperationDenied{Surface: operation.SurfaceHTTP, Op: "task.list", Reason: "${{intl:denied.reason}}"}, http.StatusForbidden, "operation_denied", "Resolved denial reason."},
		{"guard", domain.NewError(domain.ErrGuardViolation, "blocked: ${{intl:denied.reason}}", nil), http.StatusUnprocessableEntity, "guard_violation", "blocked: Resolved denial reason."},
		{"missing task", domain.NewError(domain.ErrTaskNotFound, "task not found", nil), http.StatusNotFound, "task_not_found", "task not found"},
		{"transition", domain.NewError(domain.ErrWorkflowInvalidTransition, "no transition", nil), http.StatusConflict, "workflow_invalid_transition", "no transition"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops := &fakeOps{listTasks: func(contract.ListTasksInput) (contract.ListTasksResponse, error) {
				return contract.ListTasksResponse{}, tc.err
			}}
			server, _ := newTestServer(t, ops, fakeLog{})
			rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/tasks", "", nil)
			env := decode(t, rec)
			if rec.Code != tc.status || env.OK || env.Code != tc.code || env.Message != tc.msg {
				t.Fatalf("got %d %+v, want %d %s %q", rec.Code, env, tc.status, tc.code, tc.msg)
			}
		})
	}
}

func TestCreateTaskDecodesStrictBodyAndIgnoresClientSelector(t *testing.T) {
	var got contract.CreateTaskInput
	ops := &fakeOps{createTask: func(input contract.CreateTaskInput) (contract.CreateTaskResponse, error) {
		got = input
		return contract.CreateTaskResponse{Task: &contract.TaskSummary{ID: 9}}, nil
	}}
	server, _ := newTestServer(t, ops, fakeLog{})

	rec := do(t, server, http.MethodPost, "/api/v1/projects/beta/tasks", `{"title":"T","description":"D","confirmed":true}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	if got.ProjectID != 8 || got.Title != "T" || !got.Confirmed {
		t.Fatalf("input = %+v", got)
	}
	for _, body := range []string{`{"title":"T","description":"D","project_id":7}`, `{"description":"D"} {}`, `not json`} {
		rec := do(t, server, http.MethodPost, "/api/v1/projects/beta/tasks", body, nil)
		if rec.Code != http.StatusBadRequest || decode(t, rec).Code != "invalid_body" {
			t.Fatalf("body %q = %d %s, want invalid_body", body, rec.Code, rec.Body.String())
		}
	}
}

func TestListValuesAcceptsRepeatedAndCommaSeparated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?category=task,comment&category=plan&category=", nil)
	if got := listValues(req, "category"); !slices.Equal(got, []string{"task", "comment", "plan"}) {
		t.Fatalf("listValues = %v", got)
	}
}
