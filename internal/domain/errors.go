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
	ErrDependencyInvalid         ErrorCode = "dependency_invalid"
	ErrValidation                ErrorCode = "validation_error"
	ErrLawNotFound               ErrorCode = "law_not_found"
	ErrSkillNotFound             ErrorCode = "skill_not_found"
	ErrPersonaNotFound           ErrorCode = "persona_not_found"
	ErrSkillReferenced           ErrorCode = "skill_referenced"
	ErrEditorFailed              ErrorCode = "editor_failed"
	ErrTagNotFound               ErrorCode = "tag_not_found"
	ErrTagConflict               ErrorCode = "tag_conflict"
	ErrGuardViolation            ErrorCode = "guard_violation"
	ErrErrorNotFound             ErrorCode = "error_not_found"
	ErrSolutionNotFound          ErrorCode = "solution_not_found"
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
