package tui

import (
	"regexp"
	"sync"
	"testing"

	"omakiten/internal/config"
)

// ansiSequencePattern matches CSI escape sequences (ESC [ ... <final byte>).
// Used by tests that need to assert plain-text content against TUI output —
// glamour-rendered bodies wrap each word in its own SGR sequence, so a raw
// strings.Contains across word boundaries no longer hits.
var ansiSequencePattern = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// stripANSI removes every CSI/SGR escape sequence from s. Test-only helper.
func stripANSI(s string) string {
	return ansiSequencePattern.ReplaceAllString(s, "")
}

var (
	testCatalogOnce sync.Once
	testCatalog     *config.Catalog
	testCatalogErr  error
)

// newTestCatalog returns a singleton Catalog backed by defaults/languages/en.yaml.
// Tests assert against the rendered English literals; the catalog has to be
// wired so m.t(key) returns the literal rather than the key fallback.
func newTestCatalog(tb testing.TB) *config.Catalog {
	tb.Helper()
	testCatalogOnce.Do(func() {
		en, err := config.LoadBundledLanguage("en")
		if err != nil {
			testCatalogErr = err
			return
		}
		testCatalog = config.NewCatalog(&en, &en)
	})
	if testCatalogErr != nil {
		tb.Fatalf("load bundled en catalog: %v", testCatalogErr)
	}
	return testCatalog
}
