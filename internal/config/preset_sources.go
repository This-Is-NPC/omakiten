package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadPresetDirectory captures a repository independently of its Git checkout.
func ReadPresetDirectory(root string) (PresetPackage, error) {
	p := PresetPackage{Type: "Omakiten Preset", Files: make(map[string]PresetFile)}
	raw, err := readFileBounded(filepath.Join(root, PresetManifestFile), MaxWiringFileBytes)
	if err != nil {
		return p, err
	}
	if err := decodeYAMLStrict(raw, &p.Manifest); err != nil {
		return p, err
	}
	total := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Name() == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() == ".git" || path == filepath.Join(root, PresetManifestFile) {
			return nil
		}
		size, err := capturePresetAsset(&p, root, path, entry, MaxPresetBytes-total)
		if err != nil {
			return err
		}
		total += size
		return nil
	})
	if err != nil {
		return p, err
	}
	return p, p.validate()
}

// SnapshotPreset flattens the active bundle and includes its resolved assets.
func SnapshotPreset(path, name, version string) (PresetPackage, error) {
	bundle, reader, err := loadBundlePlanSources(path)
	if err != nil {
		return PresetPackage{}, err
	}
	if name == "" {
		name = bundle.Kit.Key
	}
	if version == "" {
		version = "0.1.0"
	}
	wired, err := bundleToWiring(bundle)
	if err != nil {
		return PresetPackage{}, err
	}
	raw, err := marshalWiring(wired)
	if err != nil {
		return PresetPackage{}, err
	}
	p := PresetPackage{Type: "Omakiten Preset", Manifest: PresetManifest{
		SchemaVersion: 1, Name: name, Version: version, Config: "config/preset.yaml",
	}, Files: map[string]PresetFile{"config/preset.yaml": {Content: string(raw), Mode: 0o644}}}
	assets := presetBundleAssets(bundle)
	for target, source := range assets {
		data := reader.raw[source]
		p.Files[target] = PresetFile{Content: string(data), Mode: 0o644}
	}
	return p, p.validate()
}

func presetBundleAssets(bundle Bundle) map[string]string {
	assets := make(map[string]string)
	for _, entity := range bundle.Skills {
		assets["skills/"+entity.Slug+".md"] = entity.SourcePath
	}
	for _, entity := range bundle.Laws {
		assets["laws/"+entity.Slug+".md"] = entity.SourcePath
	}
	for _, entity := range bundle.Personas {
		assets["personas/"+entity.Slug+".md"] = entity.SourcePath
	}
	for _, entity := range bundle.Templates {
		assets["templates/"+entity.Slug+".md"] = entity.SourcePath
	}
	for _, language := range bundle.Languages {
		if language.SourcePath != "" {
			assets["languages/"+filepath.Base(language.SourcePath)] = language.SourcePath
		}
	}
	for _, notification := range bundle.Notifications {
		if notification.SourcePath != "" {
			assets["notifications/"+filepath.Base(notification.SourcePath)] = notification.SourcePath
		}
	}
	if bundle.ActiveThemePath != "" && bundle.ActiveThemeErr == nil {
		assets["themes/"+filepath.Base(bundle.ActiveThemePath)] = bundle.ActiveThemePath
	}
	return assets
}

func isPresetRoot(root string) bool {
	_, err := os.Stat(filepath.Join(root, PresetManifestFile))
	return err == nil
}

func capturePresetAsset(p *PresetPackage, root, path string, entry fs.DirEntry, remaining int) (int, error) {
	info, err := entry.Info()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("preset asset must be a regular file: %s", path)
	}
	data, err := readFileBounded(path, int64(remaining))
	if err != nil {
		return 0, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0, err
	}
	p.Files[filepath.ToSlash(rel)] = PresetFile{Content: string(data), Mode: uint32(info.Mode().Perm())}
	return len(data), nil
}
