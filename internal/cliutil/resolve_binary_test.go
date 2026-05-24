package cliutil

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestResolveBinary pins the shared contract consumed by the CLI
// editor surface and the hook exec action: absolute paths pass
// through (cleaned); bare names round-trip through exec.LookPath +
// filepath.Abs; relative paths with embedded separators are rejected;
// empty input returns ErrBinaryEmpty so callers can surface a
// "not configured" message instead of leaking a LookPath failure.
func TestResolveBinary(t *testing.T) {
	cases := []struct {
		name         string
		input        string
		wantErr      error // sentinel to check via errors.Is; nil = success
		wantAbsExact string
	}{
		{name: "empty", input: "", wantErr: ErrBinaryEmpty},
		{name: "whitespace only", input: "   ", wantErr: ErrBinaryEmpty},
		{name: "absolute passes through", input: "/usr/bin/env", wantAbsExact: "/usr/bin/env"},
		{name: "absolute cleaned", input: "/usr/bin/./env", wantAbsExact: "/usr/bin/env"},
		{name: "bare name resolves via PATH", input: "sh"},
		{name: "relative ./foo rejected", input: "./script.sh", wantErr: ErrBinaryRelativeWithSep},
		{name: "relative ../bin/foo rejected", input: "../bin/foo", wantErr: ErrBinaryRelativeWithSep},
		{name: "sub/dir/foo rejected", input: "sub/dir/foo", wantErr: ErrBinaryRelativeWithSep},
		{name: "missing on PATH errors", input: "definitely-not-a-real-cmd-xyz-omakiten", wantErr: ErrBinaryNotFoundOnPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBinary(tc.input)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got %q", tc.wantErr, got)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want errors.Is(%v)", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Fatalf("ResolveBinary returned non-absolute %q", got)
			}
			if tc.wantAbsExact != "" && got != tc.wantAbsExact {
				t.Fatalf("ResolveBinary(%q) = %q, want %q", tc.input, got, tc.wantAbsExact)
			}
		})
	}
}
