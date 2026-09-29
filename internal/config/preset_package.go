package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"omakiten/internal/paths"
)

const PresetManifestFile = "preset.yaml"
const PresetSelectionFile = paths.PresetSelectionFilename
const MaxPresetBytes = 16 << 20

// PresetManifest identifies a self-contained workflow package and its origin.
type PresetManifest struct {
	SchemaVersion int          `yaml:"schema_version" json:"schema_version"`
	Name          string       `yaml:"name" json:"name"`
	Version       string       `yaml:"version" json:"version"`
	Config        string       `yaml:"config" json:"config"`
	Origin        PresetOrigin `yaml:"origin,omitempty" json:"origin"`
}

// PresetOrigin records the source snapshot against which edits are compared.
type PresetOrigin struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
	Hash   string `yaml:"hash,omitempty" json:"hash,omitempty"`
}

// PresetFile preserves an asset's bytes and executable permission.
type PresetFile struct {
	Content string `yaml:"content" json:"content"`
	Mode    uint32 `yaml:"mode" json:"mode"`
}

// MarshalJSON preserves opaque bytes in content identities.
func (file PresetFile) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Content []byte `json:"content"`
		Mode    uint32 `json:"mode"`
	}{[]byte(file.Content), file.Mode})
}

// PresetPackage is the complete portable representation of a preset.
type PresetPackage struct {
	Type     string                `yaml:"type" json:"type"`
	Manifest PresetManifest        `yaml:"omakiten" json:"manifest"`
	Files    map[string]PresetFile `yaml:"files" json:"files"`
}

// PresetInstallation describes a stored package using its content identity.
type PresetInstallation struct {
	ID       string         `json:"id"`
	Path     string         `json:"path"`
	Manifest PresetManifest `json:"manifest"`
	Dirty    bool           `json:"dirty"`
	Active   bool           `json:"active"`
}

func presetDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hashBytes(raw), nil
}

func (p PresetPackage) contentHash() (string, error) {
	return presetDigest(struct {
		Config string
		Files  map[string]PresetFile
	}{p.Manifest.Config, p.Files})
}

func (p PresetPackage) validate() error {
	if p.Type != "Omakiten Preset" || p.Manifest.SchemaVersion != 1 {
		return fmt.Errorf("preset requires type Omakiten Preset and schema_version 1")
	}
	if strings.TrimSpace(p.Manifest.Name) == "" || strings.TrimSpace(p.Manifest.Version) == "" {
		return fmt.Errorf("preset name and version are required")
	}
	if filepath.ToSlash(filepath.Dir(p.Manifest.Config)) != "config" {
		return fmt.Errorf("preset config must belong to config/")
	}
	if _, ok := p.Files[p.Manifest.Config]; !ok {
		return fmt.Errorf("preset config %q is missing", p.Manifest.Config)
	}
	total := 0
	for path, file := range p.Files {
		if err := validatePresetAsset(path, file); err != nil {
			return err
		}
		total += len(file.Content)
	}
	if total > MaxPresetBytes {
		return fmt.Errorf("preset exceeds %d bytes", MaxPresetBytes)
	}
	return nil
}

// EncodePreset produces one Markdown document containing every package asset.
func EncodePreset(p PresetPackage) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		return nil, err
	}
	document := JoinFrontmatter(raw, []byte("# "+p.Manifest.Name+"\n"))
	if len(document) > MaxPresetBytes {
		return nil, fmt.Errorf("preset document exceeds %d bytes", MaxPresetBytes)
	}
	return document, nil
}

// DecodePreset reads the package's strict frontmatter contract.
func DecodePreset(raw []byte) (PresetPackage, error) {
	var p PresetPackage
	if len(raw) > MaxPresetBytes {
		return p, fmt.Errorf("preset document exceeds %d bytes", MaxPresetBytes)
	}
	header, _, err := SplitFrontmatter(raw)
	if err != nil {
		return p, err
	}
	if err := decodeYAMLStrict(header, &p); err != nil {
		return p, err
	}
	return p, p.validate()
}

// WritePresetDirectory creates a complete package at an unoccupied destination.
func WritePresetDirectory(root string, p PresetPackage) error {
	if err := p.validate(); err != nil {
		return err
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		return err
	}
	for path, file := range p.Files {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(file.Content), os.FileMode(file.Mode)); err != nil {
			return err
		}
		if err := os.Chmod(target, os.FileMode(file.Mode)); err != nil {
			return err
		}
	}
	raw, err := yaml.Marshal(p.Manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, PresetManifestFile), raw, 0o644)
}

func validatePresetAsset(path string, file PresetFile) error {
	if !utf8.ValidString(path) || !filepath.IsLocal(filepath.FromSlash(path)) || strings.Contains(path, "\\") || path == PresetManifestFile || filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return fmt.Errorf("invalid preset asset path %q", path)
	}
	if strings.Contains("/"+path+"/", "/.git/") {
		return fmt.Errorf("preset assets cannot contain Git metadata: %s", path)
	}
	if strings.Contains("/"+path+"/", "/custom/") {
		return fmt.Errorf("preset assets belong directly in their entity folders: %s", path)
	}
	if file.Mode & ^uint32(0o777) != 0 {
		return fmt.Errorf("invalid preset asset permissions: %s", path)
	}
	return nil
}
