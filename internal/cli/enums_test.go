package cli

import (
	"strings"
	"testing"

	"omakiten/internal/domain"
)

func resetEnums(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		domain.RegisterPriorities(nil)
		domain.RegisterSeverities(nil)
	})
}

func TestParsePriority(t *testing.T) {
	resetEnums(t)
	domain.RegisterPriorities([]domain.PriorityPair{
		{ID: 1, Value: "low"},
		{ID: 2, Value: "med"},
		{ID: 3, Value: "high"},
	})

	cases := map[string]struct {
		in       string
		wantID   domain.Priority
		wantErr  string
	}{
		"numeric known":  {in: "2", wantID: domain.Priority(2)},
		"label known":    {in: "high", wantID: domain.Priority(3)},
		"numeric unknown": {in: "99", wantErr: "priority id is not in config.priorities"},
		"label unknown":  {in: "crit", wantErr: "unknown priority"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parsePriority(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parsePriority(%q) err = %v, want substring %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePriority(%q) err = %v", tc.in, err)
			}
			if got != tc.wantID {
				t.Fatalf("parsePriority(%q) = %d, want %d", tc.in, got, tc.wantID)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	resetEnums(t)
	domain.RegisterSeverities([]domain.SeverityPair{
		{ID: 1, Value: "info"},
		{ID: 2, Value: "warning"},
		{ID: 3, Value: "error"},
	})

	cases := map[string]struct {
		in       string
		wantID   domain.Severity
		wantErr  string
	}{
		"numeric known":   {in: "1", wantID: domain.Severity(1)},
		"label known":     {in: "error", wantID: domain.Severity(3)},
		"numeric unknown": {in: "99", wantErr: "severity id is not in config.severities"},
		"label unknown":   {in: "fatal", wantErr: "unknown severity"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseSeverity(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseSeverity(%q) err = %v, want substring %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSeverity(%q) err = %v", tc.in, err)
			}
			if got != tc.wantID {
				t.Fatalf("parseSeverity(%q) = %d, want %d", tc.in, got, tc.wantID)
			}
		})
	}
}

func TestExitErrorAndExitCode(t *testing.T) {
	t.Parallel()

	if got := (exitError{code: 7}).Error(); got != "" {
		t.Fatalf("exitError.Error() = %q, want empty (sentinel)", got)
	}

	code, ok := ExitCode(exitError{code: 42})
	if !ok || code != 42 {
		t.Fatalf("ExitCode(exitError{42}) = (%d, %v), want (42, true)", code, ok)
	}

	code, ok = ExitCode(nil)
	if ok || code != 0 {
		t.Fatalf("ExitCode(nil) = (%d, %v), want (0, false)", code, ok)
	}

	// Plain error, not the sentinel — must report not-an-exitError.
	code, ok = ExitCode(domain.NewError(domain.ErrValidation, "boom", nil))
	if ok || code != 0 {
		t.Fatalf("ExitCode(plain err) = (%d, %v), want (0, false)", code, ok)
	}
}
