package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"omakiten/defaults"
	"omakiten/internal/config"
	"omakiten/internal/paths"
)

// SkillResult reports the destination and outcome of a skill installation.
type SkillResult struct {
	Target  string `json:"target"`
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
}

func skillPath(root, target string) (string, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = home
	}
	var directory string
	switch target {
	case "agents":
		directory = ".agents"
	case "claude-code":
		directory = ".claude"
	default:
		return "", fmt.Errorf("unsupported skill destination %q", target)
	}
	return filepath.Join(root, directory, "skills", "omakiten", "SKILL.md"), nil
}

func ownedSkill(data []byte) bool {
	parts := bytes.SplitN(data, []byte("---\n"), 3)
	if len(parts) != 3 || len(parts[0]) != 0 {
		return false
	}
	var header struct {
		Name     string            `yaml:"name"`
		Metadata map[string]string `yaml:"metadata"`
	}
	return yaml.Unmarshal(parts[1], &header) == nil && header.Name == "omakiten" && header.Metadata["owner"] == "omakiten"
}

// InstallSkills publishes the embedded integration skill globally or below root.
// Existing foreign skills are never overwritten; update refreshes managed content.
func InstallSkills(root string, targets []string, update bool) ([]SkillResult, error) {
	data, err := defaults.FS.ReadFile("agent/omakiten/SKILL.md")
	if err != nil {
		return nil, err
	}
	results := make([]SkillResult, 0, len(targets))
	seen := map[string]bool{}
	for _, target := range targets {
		if seen[target] {
			continue
		}
		seen[target] = true
		result, err := installSkill(root, target, data, update)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// RemoveSkills removes only managed skill entrypoints, preserving neighboring files.
func RemoveSkills(root string) ([]string, error) {
	managed, err := managedSkills(root)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, skill := range managed {
		if err := os.Remove(skill.Path); err != nil {
			return nil, err
		}
		removed = append(removed, skill.Path)
	}
	return removed, nil
}

// RefreshSkills updates installed managed entrypoints without adding destinations.
func RefreshSkills(root string) ([]SkillResult, error) {
	managed, err := managedSkills(root)
	if err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(managed))
	for _, skill := range managed {
		targets = append(targets, skill.Target)
	}
	return InstallSkills(root, targets, true)
}

func managedSkills(root string) ([]SkillResult, error) {
	var managed []SkillResult
	for _, target := range SupportedHarnesses() {
		path, err := skillPath(root, target)
		if err != nil {
			return nil, err
		}
		if err := paths.ValidateNoSymlinkComponents(path); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !ownedSkill(data) {
			continue
		}
		managed = append(managed, SkillResult{Target: target, Path: path})
	}
	return managed, nil
}

func installSkill(root, target string, data []byte, update bool) (SkillResult, error) {
	path, err := skillPath(root, target)
	if err != nil {
		return SkillResult{}, err
	}
	if err := paths.ValidateNoSymlinkComponents(path); err != nil {
		return SkillResult{}, err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return SkillResult{}, err
	}
	if err == nil && !ownedSkill(existing) {
		return SkillResult{}, fmt.Errorf("skill destination belongs to the user: %s", path)
	}
	changed := os.IsNotExist(err) || (update && !bytes.Equal(existing, data))
	if changed {
		if err := config.WriteAtomic(path, data); err != nil {
			return SkillResult{}, err
		}
	}
	return SkillResult{Target: target, Path: path, Changed: changed}, nil
}
