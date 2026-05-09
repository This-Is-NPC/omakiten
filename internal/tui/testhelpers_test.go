package tui

import "regexp"

// ansiSequencePattern matches CSI escape sequences (ESC [ ... <final byte>).
// Used by tests that need to assert plain-text content against TUI output —
// glamour-rendered bodies wrap each word in its own SGR sequence, so a raw
// strings.Contains across word boundaries no longer hits.
var ansiSequencePattern = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// stripANSI removes every CSI/SGR escape sequence from s. Test-only helper.
func stripANSI(s string) string {
	return ansiSequencePattern.ReplaceAllString(s, "")
}
