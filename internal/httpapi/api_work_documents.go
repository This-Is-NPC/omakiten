package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/workfile"
)

// WorkDocumentOperations moves tasks and plans in and out as portable OKF
// documents.
type WorkDocumentOperations interface {
	ExportTask(ctx context.Context, input contract.ExportWorkInput) (domain.WorkDocument, error)
}

// WorkDocumentResponse carries one exported OKF Markdown document.
type WorkDocumentResponse struct {
	Type     string `json:"type"`
	Markdown string `json:"markdown"`
}

func (s *Server) workDocumentRoutes() []route {
	return []route{
		query("exportTask", http.MethodGet, taskPath+"/export", "task.export", "A task as a portable OKF Markdown document.", []param{projectParam, taskParam}, s.exportTask),
	}
}

func (s *Server) exportTask(r *http.Request) (WorkDocumentResponse, error) {
	ops, selector, taskID, err := s.task(r)
	if err != nil {
		return WorkDocumentResponse{}, err
	}
	return encodeWorkDocument(ops.ExportTask(r.Context(), contract.ExportWorkInput{ProjectSelector: selector, TaskID: taskID}))
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
