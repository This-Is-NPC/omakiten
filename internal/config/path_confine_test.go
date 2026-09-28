package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePathRejectsAbsoluteAndTraversal(t *testing.T) {
	root := t.TempDir()
	for name, path := range map[string]string{
		"absolute": filepath.Join(root, "skill.md"),
		"parent":   "skills/../outside.md",
		"outside":  filepath.Join(root, "..", "outside.md"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePath(root, path); err == nil {
				t.Fatalf("ValidatePath(%q) succeeded, want rejection", path)
			}
		})
	}
}

func TestValidatePathAllowsMissingDescendant(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join("skills", "new.md")
	if err := ValidatePath(root, path); err != nil {
		t.Fatalf("ValidatePath(%q) = %v, want success", path, err)
	}
}

func TestValidatePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "skills")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := ValidatePath(root, filepath.Join("skills", "escape.md"))
	if err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("ValidatePath() error = %v, want physical escape rejection", err)
	}
}
