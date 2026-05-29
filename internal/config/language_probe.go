package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// ProbeLanguageSetting reads only the `config.languages` block from the
// YAML profile at path and returns it as a LanguageSettings. Unlike
// LoadBundle it does NOT run ValidateBundle (nor any entity discovery),
// so it succeeds whenever the file is readable and the top-level YAML
// parses — regardless of whether the bundle is schema-valid downstream.
//
// This exists to break the chicken-and-egg trap in the early-boot
// catalog path (#370): the user-facing error that fires when
// ValidateBundle rejects a bundle is exactly the message we want
// rendered in the user's locale, but the normal resolution path needs
// the same bundle to load successfully first. The probe lets the
// bootstrap read languages.{cli,tui} from a broken bundle so the repair
// hint renders in the configured language instead of the EN baseline.
//
// The second return value reports whether the probe ran to completion:
// false on an I/O error (missing/unreadable file) or malformed top-level
// YAML; true otherwise — including when the file parses but carries no
// `config.languages` block, in which case the returned LanguageSettings
// is the zero value.
//
// Limitations:
//   - Embedded packs only. The probe-positive branch in
//     bootstrapActiveLanguage resolves the returned code through
//     LoadBundledLanguage, which reads the embed FS. A custom installed
//     pack at <root>/languages/<code>.yaml still requires a loadable
//     bundle; on a broken bundle the user falls back to the EN baseline.
//   - Permissive decoding. The anonymous wrapper declares only the keys
//     it consumes; the default yaml.v3 decoder ignores unknown fields.
//     A future migration to a strict decoder (rejecting unknown fields)
//     would break this and is the trigger to revisit.
//
// The leaf field shape is the canonical LanguageSettings struct — reused
// by name so a rename of its CLI/TUI fields (or their yaml tags)
// propagates here at compile time rather than drifting silently.
func ProbeLanguageSetting(path string) (LanguageSettings, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LanguageSettings{}, false
	}
	var doc struct {
		Config struct {
			Languages LanguageSettings `yaml:"languages"`
		} `yaml:"config"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return LanguageSettings{}, false
	}
	return doc.Config.Languages, true
}
