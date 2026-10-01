package httpapi

import (
	"context"

	"net/http"
	"omakiten/internal/contract"
)

// RecordOperations records errors, their solutions, and task progress.
type RecordOperations interface {
	RecordError(ctx context.Context, input contract.RecordErrorInput) (contract.ErrorRecordResponse, error)
}

// ErrorBody is the recordError request body.
type ErrorBody struct {
	Description string   `json:"description"`
	Context     string   `json:"context,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func (s *Server) recordRoutes() []route {
	return []route{
		command("recordError", http.MethodPost, projectPath+"/errors", "error.record", "Record an error met while working.", []param{projectParam}, s.recordError),
	}
}

func (s *Server) recordError(r *http.Request, body ErrorBody) (contract.ErrorRecordResponse, error) {
	ops, selector, err := s.project(r)
	if err != nil {
		return contract.ErrorRecordResponse{}, err
	}
	return ops.RecordError(r.Context(), contract.RecordErrorInput{ProjectSelector: selector, Description: body.Description, Context: body.Context, Tags: body.Tags})
}
