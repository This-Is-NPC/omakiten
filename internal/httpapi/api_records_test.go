package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"recordError", http.MethodPost, "/api/v1/projects/alpha/errors", `{"description":"D","context":"C","tags":["a"]}`, "RecordError", contract.RecordErrorInput{
		ProjectSelector: alpha, Description: "D", Context: "C", Tags: []string{"a"},
	}},
})

func (f *fakeOps) RecordError(_ context.Context, in contract.RecordErrorInput) (contract.ErrorRecordResponse, error) {
	f.record("RecordError", in)
	return contract.ErrorRecordResponse{}, nil
}
