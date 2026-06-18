package domain

import (
	"fmt"
	"unicode/utf8"
)

// Input length caps enforced at the domain boundary on every write path
// (CLI and MCP both reach a write through the app services that call
// these validators). They close security finding S2: a malicious or
// buggy client must not be able to push an unbounded blob into SQLite —
// storage bloat plus prompt-bloat fed back to agents.
//
// Over-cap input is REJECTED (ErrValidation), never silently truncated:
// authored content is never lost. The display-side truncateBody helper in
// the agent layer is a separate concern (it shortens a stored body for
// rendering); it does not enforce the write-side cap.
//
// One-line labels are measured in runes (naturally counted in characters).
// Long-form markdown is measured in bytes — it may be predominantly
// multi-byte, and the storage / prompt-bloat risk we are bounding is
// byte-sized.
const (
	// MaxTaskTitleRunes caps a task title at 512 runes.
	MaxTaskTitleRunes = 512
	// MaxTaskDescriptionBytes caps a task description at 64 KiB.
	MaxTaskDescriptionBytes = 64 * 1024
	// MaxPlanGoalBodyBytes caps a plan goal_body at 64 KiB.
	MaxPlanGoalBodyBytes = 64 * 1024
	// MaxCommentBodyBytes caps a comment body at 64 KiB, matching the
	// description cap — both are long-form markdown fields.
	MaxCommentBodyBytes = 64 * 1024
	// MaxCommentTitleRunes caps a comment title at 512 runes.
	MaxCommentTitleRunes = 512
	// MaxCommentKindRunes caps a comment kind at 512 runes.
	MaxCommentKindRunes = 512
)

// validateRuneCap rejects s when it exceeds max runes. field names the
// input for the error message and details. A value exactly at the cap
// passes; cap+1 fails. The check is independent of the empty/required
// check the caller runs separately.
func validateRuneCap(field, s string, max int) error {
	if n := utf8.RuneCountInString(s); n > max {
		return NewError(ErrValidation,
			fmt.Sprintf("%s exceeds maximum length of %d characters", field, max),
			map[string]any{"field": field, "length": n, "max": max, "unit": "runes"})
	}
	return nil
}

// validateByteCap rejects s when it exceeds max bytes. Mirrors
// validateRuneCap for the byte-measured long-form fields.
func validateByteCap(field, s string, max int) error {
	if n := len(s); n > max {
		return NewError(ErrValidation,
			fmt.Sprintf("%s exceeds maximum size of %d bytes", field, max),
			map[string]any{"field": field, "length": n, "max": max, "unit": "bytes"})
	}
	return nil
}

// ValidateTaskTitle enforces the title rune cap. Empty/required is the
// caller's separate concern; this validator only bounds the upper length.
func ValidateTaskTitle(title string) error {
	return validateRuneCap("task title", title, MaxTaskTitleRunes)
}

// ValidateTaskDescription enforces the description byte cap.
func ValidateTaskDescription(description string) error {
	return validateByteCap("task description", description, MaxTaskDescriptionBytes)
}

// ValidatePlanGoalBody enforces the plan goal_body byte cap on create/edit.
func ValidatePlanGoalBody(goalBody string) error {
	return validateByteCap("plan goal_body", goalBody, MaxPlanGoalBodyBytes)
}

// ValidateCommentBody enforces the comment body byte cap on the write path.
func ValidateCommentBody(body string) error {
	return validateByteCap("comment body", body, MaxCommentBodyBytes)
}

// ValidateCommentTitle enforces the comment title rune cap on the write path.
func ValidateCommentTitle(title string) error {
	return validateRuneCap("comment title", title, MaxCommentTitleRunes)
}

// ValidateCommentKind enforces the comment kind rune cap on the write path.
func ValidateCommentKind(kind string) error {
	return validateRuneCap("comment kind", kind, MaxCommentKindRunes)
}
