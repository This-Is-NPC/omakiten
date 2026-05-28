package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"omakiten/defaults"
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
	data, err := defaults.FS.ReadFile("config/" + candidate + ".yaml")
	if err != nil {
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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w wiring
	if err := dec.Decode(&w); err != nil {
		return Settings{}, fmt.Errorf("parse embedded kit YAML %s: %w", candidate, err)
	}
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
