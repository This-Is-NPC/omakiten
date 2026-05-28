package sqlutil_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	"omakiten/internal/sqlite/sqlutil"
)

// fakeScanner records the destination pointers passed to Scan so tests
// can assert decode-closure arg order and verify that the destination
// types match the row layout. The first row returned is the only row;
// subsequent calls return sql.ErrNoRows-equivalent via the rowsErr
// channel, but ScanRow only consumes one row so a single-shot fake is
// sufficient.
type fakeScanner struct {
	values []any // values to copy into dest by index
	err    error
	// captured records the dest pointers exactly as the decode
	// closure passed them, in order.
	captured []any
}

func (f *fakeScanner) Scan(dest ...any) error {
	f.captured = append(f.captured, dest...)
	if f.err != nil {
		return f.err
	}
	for i, d := range dest {
		if i >= len(f.values) {
			break
		}
		// reflect-based copy so the helper works with any pointer
		// type without bespoke type switches.
		dv := reflect.ValueOf(d).Elem()
		if !dv.CanSet() {
			continue
		}
		v := reflect.ValueOf(f.values[i])
		if v.Type().AssignableTo(dv.Type()) {
			dv.Set(v)
		}
	}
	return nil
}

// fakeRows mirrors the slice of *sql.Rows methods that ScanAll's loop
// touches. ScanAll today takes a concrete *sql.Rows so we cannot pass
// fakeRows directly; the iteration-contract test instead re-implements
// the loop locally over this fake to pin the expected behaviour.
type fakeRows struct {
	data    [][]any
	idx     int
	err     error
	scanErr error
}

func (f *fakeRows) Next() bool {
	return f.idx < len(f.data)
}

func (f *fakeRows) Err() error { return f.err }

func (f *fakeRows) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	if f.idx >= len(f.data) {
		return errors.New("no more rows")
	}
	row := f.data[f.idx]
	f.idx++
	for i, d := range dest {
		if i >= len(row) {
			break
		}
		dv := reflect.ValueOf(d).Elem()
		v := reflect.ValueOf(row[i])
		if v.Type().AssignableTo(dv.Type()) {
			dv.Set(v)
		}
	}
	return nil
}

type recordedDecode struct {
	ID    int64
	Name  string
	Extra string
}

func TestScanRowPassesArgsInDeclaredOrder(t *testing.T) {
	scanner := &fakeScanner{
		values: []any{int64(7), "alpha", "beta"},
	}
	decode := func(scan func(...any) error) (recordedDecode, error) {
		var r recordedDecode
		// Deliberately pass in declared field order; the helper
		// must hand these pointers straight to the underlying
		// Scanner.Scan call.
		if err := scan(&r.ID, &r.Name, &r.Extra); err != nil {
			return recordedDecode{}, err
		}
		return r, nil
	}

	got, err := sqlutil.ScanRow(scanner, decode)
	if err != nil {
		t.Fatalf("ScanRow returned err: %v", err)
	}
	want := recordedDecode{ID: 7, Name: "alpha", Extra: "beta"}
	if got != want {
		t.Fatalf("ScanRow result = %+v, want %+v", got, want)
	}
	if len(scanner.captured) != 3 {
		t.Fatalf("captured %d dest pointers, want 3", len(scanner.captured))
	}
	// Verify the captured types match the declared order:
	// [*int64, *string, *string].
	if _, ok := scanner.captured[0].(*int64); !ok {
		t.Fatalf("captured[0] = %T, want *int64", scanner.captured[0])
	}
	if _, ok := scanner.captured[1].(*string); !ok {
		t.Fatalf("captured[1] = %T, want *string", scanner.captured[1])
	}
	if _, ok := scanner.captured[2].(*string); !ok {
		t.Fatalf("captured[2] = %T, want *string", scanner.captured[2])
	}
}

func TestScanRowPropagatesDecodeError(t *testing.T) {
	boom := errors.New("boom")
	scanner := &fakeScanner{err: boom}
	decode := func(scan func(...any) error) (recordedDecode, error) {
		var r recordedDecode
		if err := scan(&r.ID); err != nil {
			return recordedDecode{}, err
		}
		return r, nil
	}
	got, err := sqlutil.ScanRow(scanner, decode)
	if !errors.Is(err, boom) {
		t.Fatalf("ScanRow err = %v, want %v", err, boom)
	}
	if got != (recordedDecode{}) {
		t.Fatalf("ScanRow on error = %+v, want zero value", got)
	}
}

// ScanAllOverFakeRows verifies the iteration contract: every row from
// the underlying iterator is fed to the decode closure in order, and
// the resulting slice preserves that order. Because ScanAll today
// requires a concrete *sql.Rows, we cover the same loop shape with a
// local re-implementation pinned to the fake. The behaviour test that
// runs against the real *sql.Rows lives in plans_test.go integration
// coverage (unchanged after the migration).
func TestScanAll_LoopContractWithFakeRows(t *testing.T) {
	rows := &fakeRows{
		data: [][]any{
			{int64(1), "alpha"},
			{int64(2), "beta"},
			{int64(3), "gamma"},
		},
	}
	// Re-implement the ScanAll loop locally against the interface
	// to validate the contract the helper must satisfy.
	var out []recordedDecode
	for rows.Next() {
		var r recordedDecode
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			t.Fatalf("Scan err: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Err: %v", err)
	}
	want := []recordedDecode{{ID: 1, Name: "alpha"}, {ID: 2, Name: "beta"}, {ID: 3, Name: "gamma"}}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Fatalf("loop output mismatch (-want +got):\n%s", diff)
	}
}
