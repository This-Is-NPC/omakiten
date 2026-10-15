package guards

import (
	"context"
	"fmt"
	"strings"
)

// commentFix returns the `okt comment add` command that adds one comment to
// taskID, tagged with tag when it is non-empty. Agent callers get `-a agent`
// so the comment carries the same author the guard attributes the attempt to.
func commentFix(ctx context.Context, taskID int64, tag, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "okt comment add %d", taskID)
	if attemptedBy(ctx) == "agent" {
		b.WriteString(" -a agent")
	}
	if tag != "" {
		b.WriteString(" -T " + shellWord(tag))
	}
	b.WriteString(" -b " + shellWord(body))
	return b.String()
}

// continueFix returns the command that opens the task blocking the guard.
func continueFix(taskID int64) string {
	return fmt.Sprintf("okt task continue %d", taskID)
}

// shellWord quotes s for a POSIX shell unless it is made only of characters
// that need no quoting.
func shellWord(s string) string {
	if s != "" && strings.IndexFunc(s, needsShellQuote) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func needsShellQuote(r rune) bool {
	plain := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:/@%+=", r)
	return !plain
}
