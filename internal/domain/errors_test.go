package domain

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestSafeErrorStripsLoaderPathPrefix asserts the redaction contract
// against the wrap shape SafeError actually sees in production:
// `fmt.Errorf("read %s: %w", path, inner)` — the path lives in the
// OUTER prefix (config.LoadBundle propagates that shape via
// entity_loader / language_loader / saver). SafeError must surface
// the inner cause (which carries the actionable parse / decode
// message) and strip the path-bearing outer prefix.
//
// Coded errors short-circuit before any slicing runs — they keep
// their structured Message verbatim.
func TestSafeErrorStripsLoaderPathPrefix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil err", nil, ""},
		{"plain errors.New", errors.New("simple message"), "simple message"},
		{"fmt.Errorf with no wrap", fmt.Errorf("no wrap here"), "no wrap here"},
		{
			name: "loader wrap: path in outer, inner survives",
			err:  fmt.Errorf("read /tmp/some/abs/path/foo.yaml: %w", errors.New("unknown field bar")),
			want: "unknown field bar",
		},
		{
			name: "loader wrap: deep chain keeps inner ctx",
			err:  fmt.Errorf("read /etc/x.yaml: %w", fmt.Errorf("decode: %w", errors.New("yaml: line 5"))),
			want: "decode: yaml: line 5",
		},
		{"coded error returns Message verbatim", NewError(ErrValidation, "title is required", nil), "title is required"},
		{"coded error wrapped is unwrapped", fmt.Errorf("read /tmp/x: %w", NewError(ErrTaskNotFound, "task not found", nil)), "task not found"},
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

// TestSafeErrorNeverLeaksOuterPath is the regression guard for the
// inverted-redaction bug: every loader-shape wrap must never return
// a string containing the absolute path from the outer prefix.
// Asserts intent independently of the exact slice direction so a
// future regex-scrub rewrite passes without amending the test.
func TestSafeErrorNeverLeaksOuterPath(t *testing.T) {
	path := "/home/user/.config/omakiten/secrets.yaml"
	inner := errors.New("unknown field foo")
	err := fmt.Errorf("read %s: %w", path, inner)
	got := SafeError(err)
	if strings.Contains(got, path) {
		t.Fatalf("SafeError() = %q leaked outer path %q", got, path)
	}
}
