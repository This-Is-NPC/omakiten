package cli

import (
	"omakiten/internal/config"
	"omakiten/internal/paths"
)

// bootstrapCatalog loads a best-effort Catalog for early-boot CLI tree
// construction. The cobra tree assigns Short, Long, flag usage, and
// user-facing error strings at construction time — well before any
// per-call runtime is opened — so the catalog must be resolvable
// without touching SQLite or the full ProjectRuntime path.
//
// Resolution order:
//  1. Try LoadBundle against the user-global ConfigFile (the common
//     case after `okt init` has materialized the install).
//  2. On any failure (missing file, parse error, validator rejection),
//     fall back to loading the bundled embed-only language pack so the
//     CLI tree still renders en help text instead of raw key literals.
//  3. If even that fails, return nil; Catalog.Get on nil returns the
//     key, which keeps the tree usable and surfaces missing wiring
//     visibly to anyone running an unconfigured binary.
//
// Active-language selection: when LoadBundle succeeds, the user's
// configured `languages.cli` setting drives surface selection. The
// fallback embed-only path always renders en — fixing this would
// require an alternate config reader and is not worth the complexity
// for the rare "no install yet" CLI invocation (the user is about to
// run `okt config init` anyway, after which subsequent invocations
// see the configured language).
//
// The returned Catalog is process-lifetime; BundleCache.Reload does
// not propagate here because the cobra tree is only built once.
// Subsequent runtime operations (TUI, MCP) build their own catalogs
// from the live Snapshot, so a language change visible via Reload is
// observed by those long-running surfaces on their next read.
func bootstrapCatalog(surface config.Surface) *config.Catalog {
	if cat := bootstrapFromUserConfig(surface); cat != nil {
		return cat
	}
	return bootstrapFromEmbed()
}

func bootstrapFromUserConfig(surface config.Surface) *config.Catalog {
	path, err := paths.ConfigFile()
	if err != nil {
		return nil
	}
	bundle, err := config.LoadBundle(path)
	if err != nil {
		return nil
	}
	snap := config.BuildSnapshot(bundle)
	return snap.Catalog(surface)
}

// bootstrapFromEmbed loads only defaults/languages/en.yaml from the
// embed FS and returns it as both active and baseline. Used when the
// user has no installed config yet (fresh checkout, `okt --help`
// before `okt config init`) so the help screen still renders English
// instead of raw catalog keys.
func bootstrapFromEmbed() *config.Catalog {
	lang, err := config.LoadBundledLanguage("en")
	if err != nil {
		return nil
	}
	return config.NewCatalog(&lang, &lang)
}
