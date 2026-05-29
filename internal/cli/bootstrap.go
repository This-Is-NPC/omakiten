package cli

import (
	"sync"

	"omakiten/internal/config"
	"omakiten/internal/paths"
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
		pkgCatalog = bootstrapCatalog(config.SurfaceCLI)
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

// bootstrapCatalog loads the Catalog used for cobra-tree construction
// (Short, Long, flag usage, CLI-owned error strings). The cobra tree is
// assigned at process start — well before any per-call runtime opens
// SQLite — so the resolver must be cheap and side-effect-free.
//
// The baseline always comes from the embed FS (defaults/languages/
// <code>.yaml shipped with the binary). On top of that the user's
// configured `languages.cli` selection overrides the active pack so
// help text follows the language selection without forcing a restart
// of every CLI invocation. The active pack is sourced from the user's
// installed languages/ tree when present, falling back to embed when
// the installed pack lacks the configured code.
//
// Strict use of embed for the baseline insulates fresh installs and
// stale materialized trees from missing-key fallbacks: a newly-added
// catalog key is always resolvable through the bundled `en` baseline
// even when the user's installed `en.yaml` has not been refreshed by
// `okt config init --force` since the build.
func bootstrapCatalog(surface config.Surface) *config.Catalog {
	baseline, err := config.LoadBundledLanguage("en")
	if err != nil {
		return nil
	}
	active := bootstrapActiveLanguage(surface, &baseline)
	return config.NewCatalog(active, &baseline)
}

// bootstrapActiveLanguage selects the active language pack for the
// requested surface. Resolution: read the user-global omakiten.yaml to
// pick up `languages.cli` / `languages.tui`; load that code from the
// embed FS (always present for shipped codes) or fall back to the
// baseline when the user picked a custom code we cannot resolve from
// embed alone. Catalog.Get falls back to baseline → key literal when
// active is nil or missing a key, so any failure here is non-fatal.
func bootstrapActiveLanguage(surface config.Surface, baseline *config.Language) *config.Language {
	path, err := paths.ConfigFile()
	if err != nil {
		return baseline
	}
	// Probe first (#370): read languages.{cli,tui} without running
	// ValidateBundle so a broken bundle — the exact moment the repair
	// hint matters most — still renders errors in the user's locale.
	// Same `path` the LoadBundle branch consumes below, so the two can
	// never read different files. The probe wins only when it yields a
	// non-empty surface code that resolves through the embed FS; an empty
	// or unresolvable code falls through to the LoadBundle path below,
	// preserving custom-pack support on healthy bundles.
	if probe, ok := config.ProbeLanguageSetting(path); ok {
		code := probe.CLI
		if surface == config.SurfaceTUI {
			code = probe.TUI
		}
		if code != "" && code != baseline.Code {
			if embed, err := config.LoadBundledLanguage(code); err == nil {
				return &embed
			}
		}
	}
	bundle, err := config.LoadBundle(path)
	if err != nil {
		return baseline
	}
	eff := bundle.Config.EffectiveLanguages()
	code := eff.CLI
	if surface == config.SurfaceTUI {
		code = eff.TUI
	}
	if code == "" || code == baseline.Code {
		return baseline
	}
	for _, lang := range bundle.Languages {
		if lang.Code == code {
			return &lang
		}
	}
	if embed, err := config.LoadBundledLanguage(code); err == nil {
		return &embed
	}
	return baseline
}
