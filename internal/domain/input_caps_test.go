package domain

import (
	"errors"
	"strings"
	"testing"
)

// assertValidationKind asserts err is a CodedError of kind ErrValidation
// (the boundary contract: over-cap input is rejected, not truncated).
func assertValidationKind(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %s", ErrValidation)
	}
	var coded *CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error = %T %v, want CodedError", err, err)
	}
	if coded.Code != ErrValidation {
		t.Fatalf("CodedError.Code = %q, want %q", coded.Code, ErrValidation)
	}
}

func TestValidateTaskTitleCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		runes   int
		wantErr bool
	}{
		{"at cap passes", MaxTaskTitleRunes, false},
		{"over cap rejects", MaxTaskTitleRunes + 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTaskTitle(strings.Repeat("a", tc.runes))
			if tc.wantErr {
				assertValidationKind(t, err)
				return
			}
			if err != nil {
				t.Fatalf("ValidateTaskTitle(len=%d) error = %v, want nil", tc.runes, err)
			}
		})
	}
}

// Multi-byte runes prove the title cap counts runes, not bytes: 512
// three-byte runes (1536 bytes) must still pass.
func TestValidateTaskTitleCountsRunesNotBytes(t *testing.T) {
	t.Parallel()
	if err := ValidateTaskTitle(strings.Repeat("あ", MaxTaskTitleRunes)); err != nil {
		t.Fatalf("ValidateTaskTitle(512 multi-byte runes) error = %v, want nil", err)
	}
	if err := ValidateTaskTitle(strings.Repeat("あ", MaxTaskTitleRunes+1)); err == nil {
		t.Fatal("ValidateTaskTitle(513 multi-byte runes) error = nil, want validation error")
	}
}

func TestValidateTaskDescriptionCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		bytes   int
		wantErr bool
	}{
		{"at cap passes", MaxTaskDescriptionBytes, false},
		{"over cap rejects", MaxTaskDescriptionBytes + 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateTaskDescription(strings.Repeat("a", tc.bytes))
			if tc.wantErr {
				assertValidationKind(t, err)
				return
			}
			if err != nil {
				t.Fatalf("ValidateTaskDescription(len=%d) error = %v, want nil", tc.bytes, err)
			}
		})
	}
}

func TestValidateCommentBodyCap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		bytes   int
		wantErr bool
	}{
		{"at cap passes", MaxCommentBodyBytes, false},
		{"over cap rejects", MaxCommentBodyBytes + 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCommentBody(strings.Repeat("a", tc.bytes))
			if tc.wantErr {
				assertValidationKind(t, err)
				return
			}
			if err != nil {
				t.Fatalf("ValidateCommentBody(len=%d) error = %v, want nil", tc.bytes, err)
			}
		})
	}
}
