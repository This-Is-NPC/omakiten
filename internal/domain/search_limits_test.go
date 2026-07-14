package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSearchQueryEnforcesByteAndLexicalTokenCaps(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"bytes":           strings.Repeat("x", SearchQueryMaxBytes+1),
		"lexical tokens":  strings.Repeat("term OR ", SearchQueryMaxTokens/2+1) + "term",
		"multibyte bytes": strings.Repeat("界", SearchQueryMaxBytes/3+1),
		"invalid UTF-8":   string([]byte{'v', 0xff}),
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateSearchQuery(query)
			var coded *CodedError
			if !errors.As(err, &coded) || coded.Code != ErrValidation || coded.Message != "search query exceeds limits" {
				t.Fatalf("ValidateSearchQuery error = %v, want stable validation error", err)
			}
		})
	}
}

func TestValidateSearchQueryAcceptsDocumentedBoundary(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"bytes":  strings.Repeat("x", SearchQueryMaxBytes),
		"tokens": strings.TrimSpace(strings.Repeat("x ", SearchQueryMaxTokens)),
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, err := ValidateSearchQuery(query); err != nil || got != query {
				t.Fatalf("ValidateSearchQuery(boundary) = len %d, %v", len(got), err)
			}
		})
	}
}
