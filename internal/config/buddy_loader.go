package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadBuddy reads a single buddy YAML, decodes with KnownFields(true),
// and runs ValidateBuddy. The on-disk shape mirrors theme files —
// pure YAML, no frontmatter wrapping.
func LoadBuddy(path string) (Buddy, error) {
	file, err := os.Open(path)
	if err != nil {
		return Buddy{}, err
	}
	defer func() { _ = file.Close() }()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)

	var buddy Buddy
	if err := decoder.Decode(&buddy); err != nil {
		return Buddy{}, fmt.Errorf("decode %s: %w", path, err)
	}
	buddy.SourcePath = path
	if err := ValidateBuddy(buddy); err != nil {
		return Buddy{}, err
	}
	return buddy, nil
}

// LoadBuddies discovers every *.yaml under dir and dir/custom and
// returns them keyed by Buddy.Name (slug-lowercased filename in the
// theme tradition is NOT used — Buddy.Name from the YAML is the
// canonical identifier so the user sees the same string in
// `config.tui.buddy.active`). Custom files override defaults that
// share a name. Returns an empty map when dir is missing — the
// runtime treats "no buddies" as "no buddy.show available" and the
// hooks-validator step rejects buddy.show hooks accordingly.
func LoadBuddies(dir string) (map[string]Buddy, error) {
	files, err := listBuddyFiles(dir)
	if err != nil {
		return nil, err
	}

	byName := map[string]Buddy{}
	seen := map[string]string{}
	for _, file := range files {
		buddy, err := LoadBuddy(file.Path)
		if err != nil {
			return nil, err
		}
		buddy.IsCustom = file.IsCustom
		if previous, dup := seen[buddy.Name]; dup {
			previousIsCustom := byName[buddy.Name].IsCustom
			if previousIsCustom == file.IsCustom {
				return nil, fmt.Errorf("duplicate buddy name %q (also defined in %s)", buddy.Name, previous)
			}
			// Custom overrides default — only when the new file is
			// custom AND the existing entry was a default.
			if !file.IsCustom {
				continue
			}
		}
		seen[buddy.Name] = file.Path
		byName[buddy.Name] = buddy
	}
	return byName, nil
}

type buddyFile struct {
	Path     string
	IsCustom bool
}

func listBuddyFiles(dir string) ([]buddyFile, error) {
	defaults, err := readYAMLFilesIn(dir, false)
	if err != nil {
		return nil, err
	}
	customs, err := readYAMLFilesIn(filepath.Join(dir, "custom"), true)
	if err != nil {
		return nil, err
	}
	return append(defaults, customs...), nil
}

func readYAMLFilesIn(dir string, isCustom bool) ([]buddyFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []buddyFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
			continue
		}
		files = append(files, buddyFile{Path: filepath.Join(dir, name), IsCustom: isCustom})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
