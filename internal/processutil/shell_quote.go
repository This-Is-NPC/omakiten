package processutil

import "strings"

// ShellQuote quotes s for a POSIX shell unless it is made only of characters
// that need no quoting.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, needsShellQuote) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func needsShellQuote(r rune) bool {
	plain := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:/@%+=", r)
	return !plain
}
