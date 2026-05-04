package domain

import (
	"errors"
	"testing"
)

func TestNewError(t *testing.T) {
	err := NewError(ErrValidation, "title required", map[string]any{"field": "title"})
	if err.Code != ErrValidation {
		t.Fatalf("NewError().Code = %q, want %q", err.Code, ErrValidation)
	}
	if err.Message != "title required" {
		t.Fatalf("NewError().Message = %q, want %q", err.Message, "title required")
	}
	if err.Details["field"] != "title" {
		t.Fatalf("NewError().Details = %#v, want field=title", err.Details)
	}
}

func TestCodedErrorError(t *testing.T) {
	err := NewError(ErrTaskNotFound, "missing", nil)
	want := "task_not_found: missing"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestCodedErrorAs(t *testing.T) {
	err := NewError(ErrProjectNotFound, "not found", nil)
	var coded *CodedError
	if !errors.As(err, &coded) {
		t.Fatal("errors.As failed for CodedError")
	}
	if coded.Code != ErrProjectNotFound {
		t.Fatalf("coded.Code = %q, want %q", coded.Code, ErrProjectNotFound)
	}
}
