package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"omakiten/internal/contract"
)

func ptr[T any](v T) *T { return &v }

// mappingCase pins, for one JSON route, the operation it calls and the
// input built from the path, query, and body.
type mappingCase struct {
	id     string
	method string
	target string
	body   string
	call   string
	input  any
}

// mappingCases collects the cases each api_*_test.go file registers.
var mappingCases []mappingCase

func mapping(cases []mappingCase) bool {
	mappingCases = append(mappingCases, cases...)
	return true
}

// alpha is the selector the test runtimes resolve for the "alpha" slug.
var alpha = contract.ProjectSelector{ProjectID: 7}

// TestRoutesMapRequestsOntoOperationInputs runs every registered mapping
// case. The project always comes from the path.
func TestRoutesMapRequestsOntoOperationInputs(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range mappingCases {
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

// assertEveryJSONRouteCovered fails when an operation route is added without
// a mapping case. Routes outside the operation census test their own reads.
func assertEveryJSONRouteCovered(t *testing.T, covered map[string]bool) {
	t.Helper()
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	for _, rt := range server.routes() {
		if rt.stream == nil && !rt.public && rt.slug != "" && !covered[rt.id] {
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
