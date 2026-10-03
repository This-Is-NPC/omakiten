package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/operation"
	"omakiten/internal/output"
)

// maxBodyBytes caps a request body; application input caps still apply.
const maxBodyBytes = 1 << 20

// Adapter error codes. Their messages are catalog keys under http.error.
const (
	codeUnauthorized     = "unauthorized"
	codeHostRejected     = "host_rejected"
	codeRouteNotFound    = "route_not_found"
	codeInvalidBody      = "invalid_body"
	codeInvalidParameter = "invalid_parameter"
	codeInternal         = "internal_error"
)

// adapterError is a failure detected by the adapter before an operation runs.
type adapterError struct {
	status  int
	code    string
	details map[string]any
}

func (e *adapterError) Error() string { return e.code }

func invalidParameter(name, value string) error {
	return &adapterError{status: http.StatusBadRequest, code: codeInvalidParameter, details: map[string]any{"parameter": name, "value": value}}
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return &adapterError{status: http.StatusBadRequest, code: codeInvalidBody, details: map[string]any{"error": err.Error()}}
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return &adapterError{status: http.StatusBadRequest, code: codeInvalidBody, details: map[string]any{"error": "body must contain one JSON value"}}
	}
	return nil
}

// failure maps err onto the CLI's error envelope and an HTTP status. Every
// message passes through catalog: adapter messages are keys, and domain
// messages may carry ${{intl:...}} tokens.
func failure(err error, catalog *config.Catalog) (int, output.Envelope) {
	var adapter *adapterError
	if errors.As(err, &adapter) {
		return adapter.status, output.Failure(adapter.code, catalog.Get("http.error."+adapter.code), adapter.details)
	}
	var denied operation.OperationDenied
	var coded *domain.CodedError
	switch {
	case errors.As(err, &denied):
		coded = denied.Coded(catalog)
	case errors.As(err, &coded):
	default:
		return http.StatusInternalServerError, output.Failure(codeInternal, err.Error(), nil)
	}
	return statusFor(coded.Code), output.Failure(string(coded.Code), catalog.Resolve(coded.Message), coded.Details)
}

func statusFor(code domain.ErrorCode) int {
	switch code {
	case domain.ErrValidation, domain.ErrDependencyInvalid:
		return http.StatusBadRequest
	case domain.ErrOperationDenied:
		return http.StatusForbidden
	case domain.ErrProjectNotFound, domain.ErrTaskNotFound, domain.ErrBucketNotFound,
		domain.ErrLawNotFound, domain.ErrSkillNotFound, domain.ErrPersonaNotFound,
		domain.ErrTagNotFound, domain.ErrErrorNotFound, domain.ErrSolutionNotFound,
		domain.ErrPlanNotFound, domain.ErrPlanWaveNotFound:
		return http.StatusNotFound
	case domain.ErrProjectAmbiguous, domain.ErrWorkflowInvalidTransition,
		domain.ErrTagConflict, domain.ErrPlanSlugConflict:
		return http.StatusConflict
	case domain.ErrGuardViolation:
		return http.StatusUnprocessableEntity
	case domain.ErrRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func writeEnvelope(w http.ResponseWriter, status int, envelope output.Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = output.Write(w, envelope)
}
