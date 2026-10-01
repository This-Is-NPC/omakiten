package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/workfile"
)

// exportedTask is the document the fake ExportTask returns.
var exportedTask = domain.WorkDocument{Type: "Omakiten Task", Spec: domain.WorkSpec{Version: 1, Task: &domain.WorkTask{Key: "ship", Title: "Ship", Description: "Ship it."}}}

// taskMarkdown is exportedTask as OKF Markdown; parsedTask is what an
// import of it hands the operation.
var taskMarkdown, parsedTask = okf(exportedTask)

// planMarkdown is a two-task plan as OKF Markdown; parsedPlan is what an
// import of it hands the operation.
var planMarkdown, parsedPlan = okf(domain.WorkDocument{
	Type: "Omakiten Plan", Title: "Delivery", Body: "Ship the board.",
	Spec: domain.WorkSpec{Version: 1, Slug: "delivery", Tasks: []domain.WorkTask{{Key: "api", Title: "API"}, {Key: "ui", Title: "UI"}}},
})

var _ = mapping([]mappingCase{
	{"exportTask", http.MethodGet, "/api/v1/projects/alpha/tasks/5/export", "", "ExportTask", contract.ExportWorkInput{ProjectSelector: alpha, TaskID: 5}},
	{"exportPlan", http.MethodGet, "/api/v1/projects/alpha/plans/delivery/export", "", "ExportPlan", contract.ExportWorkInput{ProjectSelector: alpha, Slug: "delivery"}},
	{"importTask", http.MethodPost, "/api/v1/projects/alpha/tasks/import", `{"markdown":` + taskMarkdown + `,"dry_run":true,"confirmed":true}`, "ImportTask", contract.ImportWorkInput{ProjectSelector: alpha, Document: parsedTask, DryRun: true, Confirmed: true}},
	{"importPlan", http.MethodPost, "/api/v1/projects/alpha/plans/import", `{"markdown":` + planMarkdown + `}`, "ImportPlan", contract.ImportWorkInput{ProjectSelector: alpha, Document: parsedPlan}},
})

// okf encodes doc and returns it as a JSON string together with the
// document the codec parses back from it.
func okf(doc domain.WorkDocument) (string, domain.WorkDocument) {
	markdown, err := workfile.Encode(doc)
	if err != nil {
		panic(err)
	}
	parsed, err := workfile.Parse(strings.NewReader(string(markdown)))
	if err != nil {
		panic(err)
	}
	quoted, err := json.Marshal(string(markdown))
	if err != nil {
		panic(err)
	}
	return string(quoted), parsed
}

// TestExportAnswersTheEncodedDocument: the response carries the Markdown
// the CLI writes for the same document.
func TestExportAnswersTheEncodedDocument(t *testing.T) {
	server, _ := newTestServer(t, &fakeOps{}, fakeLog{})
	rec := do(t, server, http.MethodGet, "/api/v1/projects/alpha/tasks/5/export", "", nil)
	env := decode(t, rec)
	if rec.Code != http.StatusOK || !env.OK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var got WorkDocumentResponse
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	var want string
	if err := json.Unmarshal([]byte(taskMarkdown), &want); err != nil {
		t.Fatal(err)
	}
	if got.Type != exportedTask.Type || got.Markdown != want {
		t.Fatalf("got %+v, want type %q and markdown %q", got, exportedTask.Type, want)
	}
}

// TestImportRejectsMalformedMarkdown: a document the codec cannot parse is
// a validation error and no operation runs.
func TestImportRejectsMalformedMarkdown(t *testing.T) {
	for _, target := range []string{
		"/api/v1/projects/alpha/tasks/import",
		"/api/v1/projects/alpha/plans/import",
	} {
		t.Run(target, func(t *testing.T) {
			ops := &fakeOps{}
			server, _ := newTestServer(t, ops, fakeLog{})
			rec := do(t, server, http.MethodPost, target, `{"markdown":"no frontmatter"}`, nil)
			if env := decode(t, rec); rec.Code != http.StatusBadRequest || env.Code != string(domain.ErrValidation) {
				t.Fatalf("got %d %+v, want validation_error", rec.Code, env)
			}
			if len(ops.calls) != 0 {
				t.Fatalf("operation ran: %+v", ops.calls)
			}
		})
	}
}

func (f *fakeOps) ExportTask(_ context.Context, in contract.ExportWorkInput) (domain.WorkDocument, error) {
	f.record("ExportTask", in)
	return exportedTask, nil
}

func (f *fakeOps) ImportTask(_ context.Context, in contract.ImportWorkInput) (contract.ImportWorkResponse, error) {
	f.record("ImportTask", in)
	return contract.ImportWorkResponse{}, nil
}

func (f *fakeOps) ExportPlan(_ context.Context, in contract.ExportWorkInput) (domain.WorkDocument, error) {
	f.record("ExportPlan", in)
	return domain.WorkDocument{}, nil
}

func (f *fakeOps) ImportPlan(_ context.Context, in contract.ImportWorkInput) (contract.ImportWorkResponse, error) {
	f.record("ImportPlan", in)
	return contract.ImportWorkResponse{}, nil
}
