package app

import (
	"os"
	"strings"
)

// ResolveEditor returns the user's preferred editor command, falling back to
// nano. Order: $EDITOR, $VISUAL, "nano". The returned value may include
// arguments separated by spaces (e.g. "code --wait"); callers are responsible
// for splitting it appropriately for their exec context.
func ResolveEditor() string {
	for _, env := range []string{"EDITOR", "VISUAL"} {
		value := strings.TrimSpace(os.Getenv(env))
		if value != "" {
			return value
		}
	}
	return "nano"
}
