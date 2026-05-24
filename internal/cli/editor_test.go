package cli

import (
	"errors"
	"strings"
	"testing"

	"omakiten/internal/domain"
)

// TestResolveEditorBinary mirrors the hooks exec guard: bare names go
// through exec.LookPath; absolute paths pass through; relative paths
// with embedded separators are rejected outright; empty input returns
// the coded EditorNotFound error.
func TestResolveEditorBinary(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantErr     bool
		wantCode    domain.ErrorCode
		wantAbsPath string
	}{
		{name: "empty input is coded not-configured", input: "", wantErr: true, wantCode: domain.ErrEditorNotFound},
		{name: "whitespace input is coded not-configured", input: "   ", wantErr: true, wantCode: domain.ErrEditorNotFound},
		{name: "bare name on PATH resolves", input: "sh", wantErr: false},
		{name: "absolute path passes through", input: "/usr/bin/env", wantErr: false, wantAbsPath: "/usr/bin/env"},
		{name: "relative ./editor rejected", input: "./vim", wantErr: true, wantCode: domain.ErrEditorNotFound},
		{name: "relative sub/dir/editor rejected", input: "bin/vim", wantErr: true, wantCode: domain.ErrEditorNotFound},
		{name: "missing editor on PATH is coded", input: "definitely-not-an-editor-xyz-omakiten", wantErr: true, wantCode: domain.ErrEditorNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveEditorBinary(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				var coded *domain.CodedError
				if !errors.As(err, &coded) {
					t.Fatalf("error is not a CodedError: %T %v", err, err)
				}
				if coded.Code != tc.wantCode {
					t.Fatalf("error code = %q, want %q", coded.Code, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(got, "/") {
				t.Fatalf("resolveEditorBinary returned non-absolute path %q", got)
			}
			if tc.wantAbsPath != "" && got != tc.wantAbsPath {
				t.Fatalf("got %q, want %q", got, tc.wantAbsPath)
			}
		})
	}
}
