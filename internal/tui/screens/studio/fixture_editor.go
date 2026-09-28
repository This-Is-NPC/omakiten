package studio

import (
	"context"
	"fmt"
	"path/filepath"

	"omakiten/internal/config"
)

// fixtureBundleEditor is the fixture's own implementation of the screen's
// contract.BundleEditor port, reading and writing the materialised config root through
// internal/config — the package the screen already depends on for
// config.Bundle.
//
// It replaces a reach the fixture used to make for internal/configstore (a
// right-side adapter the screens have no business naming) wrapped in
// internal/testfixtures/bundleeditor (a shim whose whole purpose is to hand a
// caller an internal/app type without naming internal/app, which no file under
// internal/tui may import). Both arrived in the production build graph through
// this one non-test file, and both are gone: the fixture now satisfies the port
// the screen declares, the same way the live host does with the runtime's
// editor.
//
// Load and Hash go straight to the loader and the hasher the adapter delegated
// to anyway, so a recorded fixture reads exactly the bytes it did before.
type fixtureBundleEditor struct{ path string }

func newFixtureBundleEditor(path string) *fixtureBundleEditor {
	return &fixtureBundleEditor{path: path}
}

func (e *fixtureBundleEditor) Path() string        { return e.path }
func (e *fixtureBundleEditor) SetPath(path string) { e.path = path }
func (e *fixtureBundleEditor) ConfigDir() string   { return filepath.Dir(e.path) }

func (e *fixtureBundleEditor) RootDir() string {
	return config.ConfigRootFromYAMLPath(e.path)
}

func (e *fixtureBundleEditor) Load() (config.Bundle, error) {
	return config.LoadBundle(e.path)
}

func (e *fixtureBundleEditor) LoadPlan() (config.Bundle, string, map[string]string, error) {
	bundle, err := config.LoadBundle(e.path)
	if err != nil {
		return config.Bundle{}, "", nil, err
	}
	hash, err := config.HashFile(e.path)
	if err != nil {
		return config.Bundle{}, "", nil, err
	}
	return bundle, hash, map[string]string{e.path: hash}, nil
}

func (e *fixtureBundleEditor) Hash() (string, error) {
	return config.HashFile(e.path)
}

// Apply is the wiring-only commit: mutate the bundle as it is on disk, write
// it atomically, and re-load so the caller observes bytes that round-tripped
// through the validator.
func (e *fixtureBundleEditor) Apply(_ context.Context, bundle config.Bundle, sourceHashes map[string]string, mutate func(*config.Bundle) error) (config.Bundle, error) {
	currentHash, err := config.HashFile(e.path)
	if err != nil || currentHash != sourceHashes[e.path] {
		return config.Bundle{}, fmt.Errorf("%s", tr(nil, "bundle.changed_while_planning", "bundle changed while planning"))
	}
	if mutate != nil {
		if err := mutate(&bundle); err != nil {
			return config.Bundle{}, err
		}
	}
	if err := config.SaveBundle(e.path, bundle); err != nil {
		return config.Bundle{}, err
	}
	resolved, err := config.LoadBundle(e.path)
	if err != nil {
		return config.Bundle{}, err
	}
	return resolved, nil
}
