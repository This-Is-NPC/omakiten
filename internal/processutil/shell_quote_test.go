package processutil

import "testing"

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"self-branch": "self-branch",
		"a b":         "'a b'",
		"it's":        `'it'\''s'`,
		"":            "''",
	} {
		if got := ShellQuote(in); got != want {
			t.Fatalf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
