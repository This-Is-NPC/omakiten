package httpapi

import (
	"context"
	"strings"

	"net/http"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/workfile"
)

// WorkDocumentOperations moves tasks and plans in and out as portable OKF
// documents.
type WorkDocumentOperations interface {
	ExportTask(ctx context.Context, input contract.ExportWorkInput) (domain.WorkDocument, error)
	ImportTask(ctx context.Context, input contract.ImportWorkInput) (contract.ImportWorkResponse, error)
	ExportPlan(ctx context.Context, input contract.ExportWorkInput) (domain.WorkDocument, error)
}

// WorkDocumentResponse carries one exported OKF Markdown document.
type WorkDocumentResponse struct {
	Type     string `json:"type"`
	Markdown string `json:"markdown"`
}

// ImportWorkBody carries one OKF Markdown document to import.
type ImportWorkBody struct {
	Markdown string `json:"markdown"`
	// DryRun validates and previews the import without writing.
	DryRun bool `json:"dry_run,omitempty"`
	// Confirmed imports even when similar work exists.
	Confirmed bool `json:"confirmed,omitempty"`
}

func (s *Server) workDocumentRoutes() []route {
	return []route{
		query("exportTask", http.MethodGet, taskPath+"/export", "task.export", "A task as a portable OKF Markdown document.", []param{projectParam, taskParam}, s.exportTask),
		command("importTask", http.MethodPost, projectPath+"/tasks/import", "task.import", "Import a task from an OKF Markdown document.", []param{projectParam}, s.importTask),
		query("exportPlan", http.MethodGet, planPath+"/export", "plan.export", "A plan as a portable OKF Markdown document.", []param{projectParam, planParam}, s.exportPlan),
	}
}

func (s *Server) exportTask(r *http.Request) (WorkDocumentResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return WorkDocumentResponse{}, err
	}
	return encodeWorkDocument(ops.ExportTask(r.Context(), contract.ExportWorkInput{ProjectSelector: selector, TaskID: taskID}))
}

func (s *Server) exportPlan(r *http.Request) (WorkDocumentResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return WorkDocumentResponse{}, err
	}
	return encodeWorkDocument(ops.ExportPlan(r.Context(), contract.ExportWorkInput{ProjectSelector: selector, Slug: r.PathValue("plan")}))
}

func encodeWorkDocument(doc domain.WorkDocument, err error) (WorkDocumentResponse, error) {
	if err != nil {
		return WorkDocumentResponse{}, err
	}
	markdown, err := workfile.Encode(doc)
	if err != nil {
		return WorkDocumentResponse{}, err
	}
	return WorkDocumentResponse{Type: doc.Type, Markdown: string(markdown)}, nil
}

func (s *Server) importTask(r *http.Request, body ImportWorkBody) (contract.ImportWorkResponse, error) {
	ops, input, err := s.importInput(r, body)
	if err != nil {
		return contract.ImportWorkResponse{}, err
	}
	return ops.ImportTask(r.Context(), input)
}

// importInput resolves the project, then parses the document as the CLI
// does: any syntax failure is a validation error.
func (s *Server) importInput(r *http.Request, body ImportWorkBody) (Operations, contract.ImportWorkInput, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return nil, contract.ImportWorkInput{}, err
	}
	doc, err := workfile.Parse(strings.NewReader(body.Markdown))
	if err != nil {
		return nil, contract.ImportWorkInput{}, err
	}
	return ops, contract.ImportWorkInput{ProjectSelector: selector, Document: doc, DryRun: body.DryRun, Confirmed: body.Confirmed}, nil
}
