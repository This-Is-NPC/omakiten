package sqlutil_test

import (
	"database/sql"
	"testing"
	"time"

	"omakiten/internal/sqlite/sqlutil"
)

func TestNullStringOr(t *testing.T) {
	cases := []struct {
		name     string
		in       sql.NullString
		fallback string
		want     string
	}{
		{name: "valid returns wrapped value", in: sql.NullString{String: "hello", Valid: true}, fallback: "fallback", want: "hello"},
		{name: "valid empty string still wins over fallback", in: sql.NullString{String: "", Valid: true}, fallback: "fallback", want: ""},
		{name: "invalid returns fallback", in: sql.NullString{String: "ignored", Valid: false}, fallback: "fallback", want: "fallback"},
		{name: "invalid returns empty fallback", in: sql.NullString{Valid: false}, fallback: "", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sqlutil.NullStringOr(tc.in, tc.fallback)
			if got != tc.want {
				t.Fatalf("NullStringOr(%+v, %q) = %q, want %q", tc.in, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestNullInt64Ptr(t *testing.T) {
	t.Run("valid returns pointer to wrapped value", func(t *testing.T) {
		in := sql.NullInt64{Int64: 42, Valid: true}
		got := sqlutil.NullInt64Ptr(in)
		if got == nil {
			t.Fatalf("NullInt64Ptr(valid) = nil, want pointer to 42")
		}
		if *got != 42 {
			t.Fatalf("NullInt64Ptr(valid) deref = %d, want 42", *got)
		}
	})
	t.Run("invalid returns nil", func(t *testing.T) {
		in := sql.NullInt64{Int64: 99, Valid: false}
		got := sqlutil.NullInt64Ptr(in)
		if got != nil {
			t.Fatalf("NullInt64Ptr(invalid) = %v, want nil", got)
		}
	})
	t.Run("returned pointer does not alias caller mutation", func(t *testing.T) {
		// Defensive: the helper must not let the caller mutate
		// the source NullInt64.Int64 field by writing through the
		// returned pointer. We assert the returned *int64 points
		// to a value equal to the snapshot at call time, even if
		// the source struct is mutated afterwards.
		in := sql.NullInt64{Int64: 7, Valid: true}
		got := sqlutil.NullInt64Ptr(in)
		in.Int64 = 100
		if got == nil || *got != 7 {
			t.Fatalf("NullInt64Ptr returned pointer aliased to caller mutation: got=%v", got)
		}
	})
}

func TestNullTimePtr(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	t.Run("valid returns pointer to wrapped time", func(t *testing.T) {
		in := sql.NullTime{Time: now, Valid: true}
		got := sqlutil.NullTimePtr(in)
		if got == nil {
			t.Fatalf("NullTimePtr(valid) = nil, want pointer")
		}
		if !got.Equal(now) {
			t.Fatalf("NullTimePtr(valid) = %v, want %v", got, now)
		}
	})
	t.Run("invalid returns nil", func(t *testing.T) {
		in := sql.NullTime{Time: now, Valid: false}
		got := sqlutil.NullTimePtr(in)
		if got != nil {
			t.Fatalf("NullTimePtr(invalid) = %v, want nil", got)
		}
	})
}
