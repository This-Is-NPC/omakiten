package domain

import (
	"errors"
	"fmt"
	"testing"
)

// TestSafeErrorStripsWrapChainTail asserts the redaction contract:
// SafeError returns the top-level error message but drops every
// `%w`-wrapped tail, which is where filesystem paths + stack info
// usually surface. Coded errors return their Message verbatim.
func TestSafeErrorStripsWrapChainTail(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil err", nil, ""},
		{"plain errors.New", errors.New("simple message"), "simple message"},
		{"fmt.Errorf with no wrap", fmt.Errorf("no wrap here"), "no wrap here"},
		{"fmt.Errorf wraps inner", fmt.Errorf("ctx: %w", errors.New("/private/path/leak")), "ctx"},
		{"deep wrap chain", fmt.Errorf("outer: %w", fmt.Errorf("mid: %w", errors.New("/etc/secret"))), "outer"},
		{"coded error returns Message verbatim", NewError(ErrValidation, "title is required", nil), "title is required"},
		{"coded error wrapped is unwrapped", fmt.Errorf("outer: %w", NewError(ErrTaskNotFound, "task not found", nil)), "task not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeError(tc.err)
			if got != tc.want {
				t.Fatalf("SafeError() = %q, want %q", got, tc.want)
			}
		})
	}
}
