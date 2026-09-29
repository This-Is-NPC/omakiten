package testfixtures

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"omakiten/internal/config"
)

// PresetGitConfig serves the Omakase fixture through Git without network access.
func PresetGitConfig(root string) (string, error) {
	seed, err := config.SeedFixture(filepath.Join(root, "seed"), false)
	if err != nil {
		return "", err
	}
	p, err := config.SnapshotPreset(seed.Path, "omakase", "1.0.0")
	if err != nil {
		return "", err
	}
	repo := filepath.Join(root, "repository")
	if err := config.WritePresetDirectory(repo, p); err != nil {
		return "", err
	}
	for _, args := range [][]string{
		{"init", "--quiet", repo},
		{"-C", repo, "add", "."},
		{"-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "test: seed workflow fixture"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("fixture Git: %w: %s", err, output)
		}
	}
	path := filepath.Join(root, "gitconfig")
	url := "file://" + filepath.ToSlash(repo)
	data := fmt.Sprintf("[url %q]\n\tinsteadOf = https://github.com/This-Is-NPC/okt-workflow-omakase.git\n", url)
	return path, os.WriteFile(path, []byte(data), 0o600)
}
