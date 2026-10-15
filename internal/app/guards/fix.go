package guards

import (
	"context"
	"fmt"
	"strings"

	"omakiten/internal/processutil"
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
		b.WriteString(" -T " + processutil.ShellQuote(tag))
	}
	b.WriteString(" -b " + processutil.ShellQuote(body))
	return b.String()
}

// continueFix returns the command that opens the task blocking the guard.
func continueFix(taskID int64) string {
	return fmt.Sprintf("okt task continue %d", taskID)
}
