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
	ErrConfigTooLarge            ErrorCode = "config_too_large"
	ErrSearchIndexInvalid        ErrorCode = "search_index_invalid"
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

// SafeError returns a user-facing summary of err with the path-bearing
// outer wrap prefix stripped, suitable for surfacing in an error
// envelope's `details.error` field. Returns "" for nil so callers can
// skip the entry when there is no cause.
//
// The production wrap shape SafeError actually sees is
// `fmt.Errorf("read %s: %w", path, inner)` — config.LoadBundle
// propagates that via entity_loader / language_loader / saver, and the
// outer half is where every absolute filesystem path surfaces. The
// inner half carries the actionable parse / decode message and no
// path. So we keep everything after the first ": " and drop the
// leading wrap prefix.
//
// Callers that already use CodedError keep their own structured
// Message verbatim — SafeError short-circuits before the slice runs.
// Future hardening can add regex-based path scrubbing here without
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
	if idx := indexOfSeparator(full); idx >= 0 {
		return full[idx+2:]
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
