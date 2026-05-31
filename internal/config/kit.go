package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"omakiten/defaults"
)

// kitCache memoizes resolved kit baselines by candidate key. The embedded
// defaults are immutable for the life of the process, so a preset resolves
// to the same Settings every time; every consumer treats the returned value
// as a read-only baseline and copies out before mutating (see
// internal/testfixtures.mergeKitDefaults and computeSettingsSources). Caching
// removes the per-call MkdirTemp + recursive copy + RemoveAll that otherwise
// ran on the main load path via buildSettingsSources.
var (
	kitCacheMu sync.Mutex
	kitCache   = map[string]Settings{}
)

// LoadKitConfig parses the embedded `defaults/config/omakase.yaml` and
// returns just the Settings block — the canonical defaults the kit ships
// with the binary. Production code does NOT consult this at runtime: the
// installer (`EnsureDefaultFiles`) materialises every embedded preset into
// the user's config root, and from then on the user's selected file is the
// runtime source. The validator rejects bundles that omit canonical fields,
// so any drift between the user's file and the kit shape surfaces
// immediately.
//
// Used by:
//   - `internal/testfixtures` to baseline-merge fixtures so test YAMLs
//     don't have to repeat the canonical blocks
//   - `okt config doctor` (future) to compare user vs. kit
func LoadKitConfig() (Settings, error) {
	return LoadKitConfigByKey("omakase")
}

// LoadKitConfigByKey reads the embedded `defaults/config/<key>.yaml` and
// returns just the Settings block. Unknown keys fall back to the omakase
// baseline so the source diff has *some* canonical comparison surface
// even when the bundle declares a custom kit key not shipped in the
// binary; that fallback keeps `EffectiveTuples().Source` populated
// rather than collapsing every leaf to SourceProject.
func LoadKitConfigByKey(key string) (Settings, error) {
	candidate := key
	if candidate == "" {
		candidate = "omakase"
	}

	// Probe that the preset exists before materialising a temp dir.
	if _, err := defaults.FS.ReadFile("config/" + candidate + ".yaml"); err != nil {
		if candidate == "omakase" {
			return Settings{}, fmt.Errorf("read embedded kit YAML: %w", err)
		}
		// Unknown / custom kit key: degrade to omakase rather than
		// abort the diff. The classifier still gives a useful answer
		// (default vs. project) for every leaf that exists in the
		// omakase preset; leaves unique to the user file are tagged
		// SourceProject by classifyLeaf's nil-kit branch.
		return LoadKitConfigByKey("omakase")
	}

	kitCacheMu.Lock()
	defer kitCacheMu.Unlock()
	if cached, ok := kitCache[candidate]; ok {
		return cached, nil
	}

	// Preset YAMLs use import directives (merge_from:, from:) that the
	// resolver must expand before strict decoding. Materialise config/
	// (including modules/) into a temp dir so the resolver can follow
	// relative paths, then read via the normal two-pass loader path.
	tmp, err := os.MkdirTemp("", "okt-kit-*")
	if err != nil {
		return Settings{}, fmt.Errorf("LoadKitConfigByKey: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := copyEmbeddedDirRecursive("config", filepath.Join(tmp, "config"), false); err != nil {
		return Settings{}, fmt.Errorf("materialise embedded config %s: %w", candidate, err)
	}

	w, _, _, err := readWiringDetailed(filepath.Join(tmp, "config", candidate+".yaml"))
	if err != nil {
		return Settings{}, fmt.Errorf("parse embedded kit YAML %s: %w", candidate, err)
	}
	kitCache[candidate] = w.Config
	return w.Config, nil
}

// MustLoadKitConfig is the panic-on-error variant for callers that
// know the embedded YAML cannot fail to parse (the build itself ships
// it; failure means corrupted binary). Reserved for `internal/
// testfixtures` and similar test helpers.
func MustLoadKitConfig() Settings {
	cfg, err := LoadKitConfig()
	if err != nil {
		panic(fmt.Errorf("kit YAML unparseable; binary is corrupt: %w", err))
	}
	return cfg
}
