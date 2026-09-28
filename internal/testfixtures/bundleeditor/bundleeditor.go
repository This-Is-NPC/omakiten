// Package bundleeditor constructs an app.BundleEditor for tests without
// making the caller name an app type (D1 / 3.2). Like snapstore, it is
// intentionally small and lives outside the parent testfixtures package:
// internal/app's own tests import testfixtures for LoadBundle and
// CanonicalRegistry, so an app import in the parent package cycles them.
package bundleeditor

import "omakiten/internal/app"

// New constructs an app.BundleEditor without forcing TUI tests to
// import internal/app (D1 / 3.2).
func New(store app.BundleStore, path string) *app.BundleEditor {
	return app.NewBundleEditor(store, path)
}
