package cli

import (
	"sync"

	"omakiten/internal/config"
)

// pkgCatalog is the CLI-surface catalog shared with every helper that
// needs to resolve catalog keys outside a cobra command constructor
// (validators, slug parsers, error builders called from RunE closures).
// Package-level so helpers do not need to thread *runtimeOptions
// through their signatures; safe because CLI invocations are
// single-process and the pointer is set once via sync.Once before any
// RunE runs.
//
// Catalog.Get is nil-safe (returns the key literal), so the rare paths
// that hit this before NewRootCommand still produce a usable string.
var (
	pkgCatalog     *config.Catalog
	pkgCatalogOnce sync.Once
)

// ensurePkgCatalog lazily bootstraps pkgCatalog. Called from t() so
// package-level helpers exercised by tests (which often skip the
// NewRootCommand path) still see an English baseline instead of raw
// key literals. NewRootCommand also triggers this so the eager init
// stays available for production.
func ensurePkgCatalog() {
	pkgCatalogOnce.Do(func() {
		pkgCatalog = bootstrapCatalog()
	})
}

// t returns the CLI-surface catalog translation for key. Used by helper
// functions that do not have access to *runtimeOptions; command
// constructors should prefer opts.t for symmetry with the AC §10
// shape ("CLI cobra tree assigns Short/Long/usage from rt.Snapshot
// .Catalog(CLI) at construction time"). Both go through the same
// pointer, so callers see the same resolution.
func t(key string) string {
	ensurePkgCatalog()
	return pkgCatalog.Get(key)
}

// bootstrapCatalog uses application preferences without loading a workflow.
func bootstrapCatalog() *config.Catalog {
	baseline, err := config.LoadBundledLanguage("en")
	if err != nil {
		return nil
	}
	return config.NewCatalog(bootstrapActiveLanguage(&baseline), &baseline)
}

func bootstrapActiveLanguage(baseline *config.Language) *config.Language {
	preferences, err := config.LoadPreferences()
	if err != nil {
		return baseline
	}
	settings := preferences.Languages.Effective()
	code := settings.CLI
	if code == baseline.Code {
		return baseline
	}
	language, err := config.LoadBundledLanguage(code)
	if err != nil {
		return baseline
	}
	return &language
}
