package domain

import (
	"errors"
	"testing"
)

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
