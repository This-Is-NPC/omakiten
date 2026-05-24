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
	ErrPlanNotFound              ErrorCode = "plan_not_found"
	ErrPlanSlugConflict          ErrorCode = "plan_slug_conflict"
	ErrPlanWaveNotFound          ErrorCode = "plan_wave_not_found"
	ErrUninstallFailed           ErrorCode = "uninstall_failed"
	ErrUpdateFailed              ErrorCode = "update_failed"
	ErrEditorNotFound            ErrorCode = "editor_not_found"
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

// SafeError returns a user-facing summary of err with absolute
// filesystem paths and internal wrap chains stripped, suitable for
// surfacing in an error envelope's `details.error` field. Returns ""
// for nil so callers can skip the entry when there is no cause.
//
// The current pass keeps the top-level error string but drops every
// %w-wrapped tail, which is where most stack-trace + path noise
// surfaces. Callers that already use CodedError keep their own
// structured Message verbatim — SafeError only kicks in when the
// underlying error is opaque (errors.New, fmt.Errorf without %w, third-
// party). Future hardening can add path scrubbing here without
// touching the callers.
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	var coded *CodedError
	if asCoded(err, &coded) {
		return coded.Message
	}
	full := err.Error()
	// Strip everything after the first ": " in case the caller used
	// fmt.Errorf("ctx: %w", inner) — the inner already carries the
	// real surface message via its own Error().
	if idx := indexOfSeparator(full); idx >= 0 {
		return full[:idx]
	}
	return full
}

// asCoded is the local errors.As shim. Kept package-local so domain
// stays import-cycle free.
func asCoded(err error, target **CodedError) bool {
	for err != nil {
		if c, ok := err.(*CodedError); ok {
			*target = c
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func indexOfSeparator(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == ':' && s[i+1] == ' ' {
			return i
		}
	}
	return -1
}
