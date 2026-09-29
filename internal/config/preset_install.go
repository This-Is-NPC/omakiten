package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type presetSelection struct {
	Preset string `yaml:"preset"`
}

// ResolvePresetSelection resolves a project selection to its package config.
func ResolvePresetSelection(path string) (string, bool, error) {
	if filepath.Base(path) != PresetSelectionFile {
		return path, false, nil
	}
	raw, err := readFileBounded(path, MaxWiringFileBytes)
	if os.IsNotExist(err) {
		return path, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return resolvePresetSelectionRaw(path, raw)
}

func resolvePresetSelectionRaw(path string, raw []byte) (string, bool, error) {
	if filepath.Base(path) != PresetSelectionFile {
		return path, false, nil
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		return "", false, err
	}
	if _, ok := fields["preset"]; !ok {
		return path, false, nil
	}
	var selected presetSelection
	if err := decodeYAMLStrict(raw, &selected); err != nil {
		return "", false, err
	}
	if !filepath.IsLocal(filepath.FromSlash(selected.Preset)) {
		return "", false, fmt.Errorf("preset selection must be a relative package config path")
	}
	return filepath.Join(filepath.Dir(path), filepath.FromSlash(selected.Preset)), true, nil
}

// InstallPreset publishes a validated immutable snapshot in the selected scope.
func InstallPreset(root string, p PresetPackage) (PresetInstallation, error) {
	var installed PresetInstallation
	if err := p.validate(); err != nil {
		return installed, err
	}
	if p.Manifest.Origin.Hash == "" {
		hash, err := p.contentHash()
		if err != nil {
			return installed, err
		}
		p.Manifest.Origin.Hash = hash
	}
	dir := filepath.Join(root, "presets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return installed, err
	}
	stage, err := os.MkdirTemp(dir, ".stage-")
	if err != nil {
		return installed, err
	}
	defer os.RemoveAll(stage)
	packageRoot := filepath.Join(stage, "package")
	if err := WritePresetDirectory(packageRoot, p); err != nil {
		return installed, err
	}
	if _, err := LoadBundle(filepath.Join(packageRoot, filepath.FromSlash(p.Manifest.Config))); err != nil {
		return installed, err
	}
	id, err := presetDigest(p)
	if err != nil {
		return installed, err
	}
	target := filepath.Join(dir, id)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		if err := os.Rename(packageRoot, target); err != nil {
			return installed, err
		}
	} else if err != nil {
		return installed, err
	} else if _, err := inspectPresetSnapshot(target, id); err != nil {
		return installed, err
	}
	hash, err := p.contentHash()
	installed = PresetInstallation{ID: id, Path: target, Manifest: p.Manifest, Dirty: hash != p.Manifest.Origin.Hash}
	return installed, err
}

// InstalledPresets returns the package catalog for one explicit scope.
func InstalledPresets(root string) ([]PresetInstallation, error) {
	entries, err := os.ReadDir(filepath.Join(root, "presets"))
	if os.IsNotExist(err) {
		return []PresetInstallation{}, nil
	}
	if err != nil {
		return nil, err
	}
	active, _, err := ResolvePresetSelection(filepath.Join(root, PresetSelectionFile))
	if err != nil {
		return nil, err
	}
	items := []PresetInstallation{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '.' {
			continue
		}
		path := filepath.Join(root, "presets", entry.Name())
		item, err := inspectPresetSnapshot(path, entry.Name())
		if err != nil {
			return nil, err
		}
		item.Active = filepath.Join(path, filepath.FromSlash(item.Manifest.Config)) == active
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// ActivatePreset selects a stored package after validating its complete bundle.
func ActivatePreset(root, selector string) (PresetInstallation, error) {
	items, err := InstalledPresets(root)
	if err != nil {
		return PresetInstallation{}, err
	}
	var selected *PresetInstallation
	for i := range items {
		if items[i].ID != selector && items[i].Manifest.Name != selector {
			continue
		}
		if selected != nil {
			return PresetInstallation{}, fmt.Errorf("preset name %q is ambiguous; select its id", selector)
		}
		selected = &items[i]
	}
	if selected == nil {
		return PresetInstallation{}, fmt.Errorf("preset %q is not installed", selector)
	}
	path := filepath.Join(selected.Path, filepath.FromSlash(selected.Manifest.Config))
	if _, err := LoadBundle(path); err != nil {
		return PresetInstallation{}, err
	}
	if err := writePresetSelection(root, path); err != nil {
		return PresetInstallation{}, err
	}
	selected.Active = true
	return *selected, nil
}

func writePresetSelection(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	raw, err := yaml.Marshal(presetSelection{Preset: filepath.ToSlash(rel)})
	if err != nil {
		return err
	}
	return WriteAtomic(filepath.Join(root, PresetSelectionFile), raw)
}

// EditBundle validates a complete edited package before changing its selection.
func EditBundle(path string, edit func(string) error) error {
	physical, selected, err := ResolvePresetSelection(path)
	if err != nil {
		return err
	}
	if !selected {
		if err := validateEditablePresetConfig(path); err != nil {
			return err
		}
		return edit(path)
	}
	p, err := ReadPresetDirectory(ConfigRootFromYAMLPath(physical))
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(path), ".edit-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	stage := filepath.Join(dir, "package")
	if err := WritePresetDirectory(stage, p); err != nil {
		return err
	}
	if err := edit(filepath.Join(stage, filepath.FromSlash(p.Manifest.Config))); err != nil {
		return err
	}
	originalHash, err := p.contentHash()
	if err != nil {
		return err
	}
	p, err = ReadPresetDirectory(stage)
	if err != nil {
		return err
	}
	hash, err := p.contentHash()
	if err != nil {
		return err
	}
	if originalHash == p.Manifest.Origin.Hash && hash != originalHash {
		p.Manifest.Name += "-local"
	}
	installed, err := InstallPreset(filepath.Dir(path), p)
	if err != nil {
		return err
	}
	return writePresetSelection(filepath.Dir(path), filepath.Join(installed.Path, filepath.FromSlash(p.Manifest.Config)))
}

func inspectPresetSnapshot(path, id string) (PresetInstallation, error) {
	p, err := ReadPresetDirectory(path)
	if err != nil {
		return PresetInstallation{}, err
	}
	actual, err := presetDigest(p)
	if err != nil {
		return PresetInstallation{}, err
	}
	if actual != id {
		return PresetInstallation{}, fmt.Errorf("installed preset %s changed on disk; import the source again into a clean scope", id)
	}
	hash, err := p.contentHash()
	return PresetInstallation{ID: id, Path: path, Manifest: p.Manifest, Dirty: hash != p.Manifest.Origin.Hash}, err
}

func validateEditablePresetConfig(path string) error {
	root := ConfigRootFromYAMLPath(path)
	if filepath.Base(filepath.Dir(root)) == "presets" && isPresetRoot(root) {
		return fmt.Errorf("edit the active preset through %s", filepath.Join(filepath.Dir(filepath.Dir(root)), PresetSelectionFile))
	}
	return nil
}
