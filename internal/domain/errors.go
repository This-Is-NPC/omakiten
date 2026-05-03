package domain

import "fmt"

type ErrorCode string

const (
	ErrConfigInvalid             ErrorCode = "config_invalid"
	ErrProjectNotFound           ErrorCode = "project_not_found"
	ErrProjectAmbiguous          ErrorCode = "project_ambiguous"
	ErrTaskNotFound              ErrorCode = "task_not_found"
	ErrWorkflowInvalidTransition ErrorCode = "workflow_invalid_transition"
	ErrBucketNotFound            ErrorCode = "bucket_not_found"
	ErrValidation                ErrorCode = "validation_error"
)

type CodedError struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"msg"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *CodedError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewError(code ErrorCode, message string, details map[string]any) *CodedError {
	return &CodedError{Code: code, Message: message, Details: details}
}
