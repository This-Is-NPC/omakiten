// Package sqlutil hosts small, dependency-free helpers shared by the
// `internal/sqlite` adapters. Today it consolidates two patterns the
// adapters had been re-implementing inline:
//
//  1. NULL coercion: turning a `sql.NullX` into a plain value with an
//     explicit fallback, or into a typed pointer when the domain model
//     distinguishes "absent" from "zero".
//  2. Row decoding: a single decode closure shared by single-row
//     (`QueryRowContext`) and multi-row (`QueryContext`) callsites so the
//     two scan paths can no longer drift apart silently.
//
// Helpers here MUST stay behaviour-equivalent to the inline patterns they
// replace — they are extracted refactors, not new policy. Adding a new
// fallback rule (e.g. "treat empty string as NULL") belongs in the caller.
package sqlutil

import (
	"database/sql"
	"time"
)

// NullStringOr returns the wrapped string when v is valid, otherwise the
// caller-supplied fallback. Mirrors the inline
//
//	if v.Valid { out = v.String } else { out = fallback }
//
// pattern. The fallback is intentionally a parameter so callers can
// preserve their existing default ("" for most, sentinel values for the
// rest) without homogenising behaviour across unrelated sites.
func NullStringOr(v sql.NullString, fallback string) string {
	if v.Valid {
		return v.String
	}
	return fallback
}

// NullInt64Or returns the wrapped int64 when v is valid, otherwise the
// caller-supplied fallback. The int64 analogue of NullStringOr, for
// callers that want a plain value with an explicit default rather than a
// pointer.
func NullInt64Or(v sql.NullInt64, fallback int64) int64 {
	if v.Valid {
		return v.Int64
	}
	return fallback
}

// NullInt64Ptr lifts a nullable int64 into a `*int64`, returning nil for
// the NULL case. Used by domain types that distinguish "no parent" /
// "no related task" (nil) from "id 0" (a real but unlikely id). The
// returned pointer addresses a fresh local copy so callers cannot
// observe later mutation of the source NullInt64.
func NullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

// NullTimePtr is the time.Time analogue of NullInt64Ptr: nullable
// timestamp into a `*time.Time` so the caller can distinguish "never
// happened" (nil) from the zero time. Defensive copy semantics match
// NullInt64Ptr.
func NullTimePtr(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	out := v.Time
	return &out
}
