package sqlutil

import "database/sql"

// Scanner is the narrow contract shared by `*sql.Row` and `*sql.Rows`:
// "give me my row, fill these destinations". Accepting the interface
// lets a single `decode` closure serve both single-row
// (`QueryRowContext`) and multi-row (`QueryContext`) call paths
// without duplicating the column list — historically a drift bug
// magnet across paired `scanFoo` / `scanFooRows` helpers.
type Scanner interface {
	Scan(dest ...any) error
}

// ScanRow runs the decode closure against a single Scanner — the
// QueryRowContext shape. The closure receives the Scanner's Scan
// method directly, so the destination pointers are passed straight
// through (no intermediate copy). On error the helper returns the
// zero value of T paired with the error, matching the existing
// scanFoo conventions in `internal/sqlite/`.
func ScanRow[T any](row Scanner, decode func(scan func(...any) error) (T, error)) (T, error) {
	return decode(row.Scan)
}

// ScanAll iterates rows, invoking decode for each row and collecting
// the results. The returned slice preserves iteration order. If
// decode returns an error for any row, iteration stops and the
// helper returns the partial work as a nil slice plus the error.
// rows.Err() is checked after the loop so deferred read errors
// surface to the caller exactly as the inline `for rows.Next()`
// pattern did.
//
// The caller still owns `rows.Close()` (typically via `defer`); the
// helper does not call Close so that callers retain control over
// lifecycle in error paths.
func ScanAll[T any](rows *sql.Rows, decode func(scan func(...any) error) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		item, err := decode(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
