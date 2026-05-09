package config

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"omakiten/defaults"
)

// LoadKitConfig parses the embedded `defaults/omakiten.yaml` and returns
// just the Settings block — the canonical defaults the kit ships with
// the binary. Production code does NOT consult this at runtime: the
// installer (`EnsureDefaultFiles`) materialises the kit YAML into the
// user's config root, and from then on the user's file is the runtime
// source. The validator rejects bundles that omit canonical fields, so
// any drift between user's file and kit shape surfaces immediately.
//
// Used by:
//   - `internal/testfixtures` to baseline-merge fixtures so test YAMLs
//     don't have to repeat the canonical blocks
//   - `okt config doctor` (future) to compare user vs. kit
func LoadKitConfig() (Settings, error) {
	data, err := defaults.FS.ReadFile("omakiten.yaml")
	if err != nil {
		return Settings{}, fmt.Errorf("read embedded kit YAML: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var w wiring
	if err := dec.Decode(&w); err != nil {
		return Settings{}, fmt.Errorf("parse embedded kit YAML: %w", err)
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
