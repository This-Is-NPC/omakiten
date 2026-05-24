package sqlutil

import (
	"errors"
	"fmt"
	"strings"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ConstraintViolation enumerates the SQLite constraint kinds we
// classify at the storage edge. The zero value (ViolationUnknown) is
// intentionally reserved so a default-constructed ConstraintError
// surfaces as "we know it was a constraint, but not which one" rather
// than silently masquerading as unique. Callers should always switch
// on a non-zero ConstraintViolation.
type ConstraintViolation int

const (
	ViolationUnknown ConstraintViolation = iota
	ViolationUnique
	ViolationForeignKey
	ViolationCheck
	ViolationNotNull
)

// String returns a stable identifier for the violation, suitable for
// log lines and structured-error details payloads. Unknown values
// degrade to "constraint" so logging never panics on a future SQLite
// code we have not classified yet.
func (v ConstraintViolation) String() string {
	switch v {
	case ViolationUnique:
		return "unique"
	case ViolationForeignKey:
		return "foreign_key"
	case ViolationCheck:
		return "check"
	case ViolationNotNull:
		return "not_null"
	default:
		return "constraint"
	}
}

// ConstraintError is the typed wrapper MapSQLiteError returns for any
// recognized SQLITE_CONSTRAINT_* extended code. Table and Field are
// best-effort: SQLite emits them in messages like "UNIQUE constraint
// failed: tasks.slug" and "NOT NULL constraint failed: tasks.title",
// so we parse the trailing token. Foreign-key and check violations do
// not encode the column reliably, so Field stays empty there. Cause
// always holds the original driver error so the existing wrap chain
// (errors.Is for sentinels, %w formatting in callers) keeps working.
type ConstraintError struct {
	Violation ConstraintViolation
	Table     string
	Field     string
	Cause     error
}

func (e *ConstraintError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s constraint violation", e.Violation)
	}
	// We deliberately preserve the original driver message verbatim
	// after the prefix so existing error-string matches in tests and
	// logs still find "UNIQUE constraint failed: tasks.slug" inside
	// the wrapped form. The prefix is informational, not load-bearing.
	return fmt.Sprintf("%s constraint violation: %s", e.Violation, e.Cause.Error())
}

func (e *ConstraintError) Unwrap() error { return e.Cause }

// MapSQLiteError classifies a driver-level error into a typed
// ConstraintError when it carries one of the constraint extended
// result codes; non-constraint errors (or non-sqlite errors) are
// returned unchanged so callers can keep using errors.Is for
// sentinels like sql.ErrNoRows. nil maps to nil.
//
// The returned error wraps the original via Unwrap, so existing
// errors.Is / errors.As chains stay intact — callers can errors.As
// against *ConstraintError to take the typed path, or fall through
// to the original error for unrelated paths.
func MapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return err
	}
	switch sqliteErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE:
		return newConstraintError(ViolationUnique, err, "UNIQUE constraint failed: ")
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return newConstraintError(ViolationForeignKey, err, "")
	case sqlite3.SQLITE_CONSTRAINT_CHECK:
		return newConstraintError(ViolationCheck, err, "")
	case sqlite3.SQLITE_CONSTRAINT_NOTNULL:
		return newConstraintError(ViolationNotNull, err, "NOT NULL constraint failed: ")
	}
	return err
}

// newConstraintError builds the typed wrapper, parsing the trailing
// "table.column" token from the SQLite message when prefix is
// non-empty. SQLite emits stable phrasing for unique + not-null —
// "<KIND> constraint failed: tasks.slug" — and we only attempt the
// parse on those two branches. The split is forgiving: if the message
// shape ever drifts, Table/Field stay empty and the typed wrapper
// still works.
func newConstraintError(v ConstraintViolation, cause error, prefix string) *ConstraintError {
	ce := &ConstraintError{Violation: v, Cause: cause}
	if prefix == "" {
		return ce
	}
	msg := cause.Error()
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return ce
	}
	tail := strings.TrimSpace(msg[idx+len(prefix):])
	// SQLite may list multiple columns separated by ", " for composite
	// constraints. We take the first token for Table.Field so callers
	// get a usable hint even when the constraint spans columns.
	if comma := strings.Index(tail, ","); comma >= 0 {
		tail = tail[:comma]
	}
	// modernc.org/sqlite appends " (<extended-code>)" to the original
	// SQLite message — e.g. "tasks.slug (2067)". Trim it so the Field
	// is the bare column name. We split on the first space so any
	// future suffix shape (driver-version drift) still degrades into a
	// clean prefix rather than a leaky one.
	if sp := strings.Index(tail, " "); sp >= 0 {
		tail = tail[:sp]
	}
	if dot := strings.Index(tail, "."); dot > 0 {
		ce.Table = tail[:dot]
		ce.Field = tail[dot+1:]
	}
	return ce
}
