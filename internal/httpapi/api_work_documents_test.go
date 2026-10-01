package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/workfile"
)

var _ = mapping([]mappingCase{
	{"exportTask", http.MethodGet, "/api/v1/projects/alpha/tasks/5/export", "", "ExportTask", contract.ExportWorkInput{ProjectSelector: alpha, TaskID: 5}},
})

// exportedTask is the document the fake ExportTask returns.
var exportedTask = domain.WorkDocument{Type: "Omakiten Task", Spec: domain.WorkSpec{Version: 1, Task: &domain.WorkTask{Key: "ship", Title: "Ship", Description: "Ship it."}}}

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
	want, err := workfile.Encode(exportedTask)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != exportedTask.Type || got.Markdown != string(want) {
		t.Fatalf("got %+v, want type %q and markdown %q", got, exportedTask.Type, want)
	}
}

func (f *fakeOps) ExportTask(_ context.Context, in contract.ExportWorkInput) (domain.WorkDocument, error) {
	f.record("ExportTask", in)
	return exportedTask, nil
}
