package httpapi

import (
	"context"
	"net/http"

	"omakiten/internal/contract"
)

var _ = mapping([]mappingCase{
	{"emitEvent", http.MethodPost, "/api/v1/projects/alpha/events", `{"name":"ci_failed","payload":{"branch":"main"}}`, "EmitEvent", contract.EmitEventInput{
		ProjectSelector: alpha, Name: "ci_failed", Payload: map[string]string{"branch": "main"},
	}},
})

func (f *fakeOps) EmitEvent(_ context.Context, in contract.EmitEventInput) (contract.EmitEventResponse, error) {
	f.record("EmitEvent", in)
	return contract.EmitEventResponse{}, nil
}
