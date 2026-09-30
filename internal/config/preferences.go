package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"omakiten/internal/paths"
	"os"
	"path/filepath"
	"strings"
)

// LanguageSettings selects application locales and the agent's output directive.
type LanguageSettings struct {
	CLI         string `yaml:"cli,omitempty" json:"cli,omitempty"`
	TUI         string `yaml:"tui,omitempty" json:"tui,omitempty"`
	GUI         string `yaml:"gui,omitempty" json:"gui,omitempty"`
	AgentOutput string `yaml:"agent_output,omitempty" json:"agent_output,omitempty"`
}

// Effective applies the application defaults without modifying the preferences.
func (s LanguageSettings) Effective() LanguageSettings {
	s = s.normalized()
	if s.CLI == "" {
		s.CLI = "en"
	}
	if s.TUI == "" {
		s.TUI = "en"
	}
	if s.GUI == "" {
		s.GUI = "en"
	}
	return s
}

// Preferences contains user-wide Omakiten settings independent of workflows.
type Preferences struct {
	Languages LanguageSettings `yaml:"languages"`
}

// PreferencesPath locates application preferences under the user's config root.
func PreferencesPath() (string, error) {
	root, err := paths.ConfigRoot()
	return filepath.Join(root, "preferences.yaml"), err
}

// LoadPreferences reads application preferences; a missing file uses defaults.
func LoadPreferences() (Preferences, error) {
	path, err := PreferencesPath()
	if err != nil {
		return Preferences{}, err
	}
	raw, err := readFileBounded(path, MaxWiringFileBytes)
	if os.IsNotExist(err) {
		return Preferences{}, nil
	}
	var p Preferences
	if err == nil {
		err = decodeYAMLStrict(raw, &p)
	}
	if err == nil {
		p.Languages = p.Languages.normalized()
		err = ValidatePreferences(p)
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("application preferences %s: %w; run `okt config language reset`", path, err)
	}
	return p, nil
}

// ValidatePreferences checks application language selections against bundled locales.
func ValidatePreferences(p Preferences) error {
	bundledLanguageOnce.Do(readBundledLanguages)
	if bundledLanguageError != nil {
		return bundledLanguageError
	}
	return validateLanguageSettings(p.Languages, bundledLanguages)
}

// SavePreferences validates and atomically publishes application preferences.
func SavePreferences(p Preferences) error {
	p.Languages = p.Languages.normalized()
	if err := ValidatePreferences(p); err != nil {
		return err
	}
	path, err := PreferencesPath()
	if err != nil {
		return err
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		return err
	}
	return WriteAtomic(path, raw)
}

func (s LanguageSettings) normalized() LanguageSettings {
	s.CLI = strings.TrimSpace(s.CLI)
	s.TUI = strings.TrimSpace(s.TUI)
	s.GUI = strings.TrimSpace(s.GUI)
	s.AgentOutput = strings.TrimSpace(s.AgentOutput)
	return s
}
