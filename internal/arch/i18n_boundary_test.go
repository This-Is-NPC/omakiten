package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// catalogReferencePattern matches direct references to the i18n types
// defined in internal/config (config.Catalog, config.Language,
// config.LanguageSettings, config.Surface, config.SurfaceCLI,
// config.SurfaceTUI). The narrower per-symbol rule lets app keep its
// existing *config.Snapshot pointers (which expose AgentOutputLanguage
// as a plain string) while preventing the inner layers from coupling
// to the catalog types themselves.
var catalogReferencePattern = regexp.MustCompile(`\bconfig\.(Catalog|Language|LanguageSettings|Surface|SurfaceCLI|SurfaceTUI)\b`)

// TestI18nBoundary enforces task #82 §33: internal/domain and
// internal/app must not name the catalog types directly. Snapshot
// accessors (AgentOutputLanguage returning string, Languages returning
// []Language as projection) are the allowed channel. Adapters keep
// full access — only the inner layers are constrained.
func TestI18nBoundary(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	dirs := []string{
		filepath.Join(root, "internal", "domain"),
		filepath.Join(root, "internal", "app"),
	}
	var violations []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if loc := catalogReferencePattern.FindIndex(data); loc != nil {
				match := string(data[loc[0]:loc[1]])
				rel, _ := filepath.Rel(root, path)
				violations = append(violations, rel+": names "+match)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(violations) > 0 {
		t.Fatalf("i18n boundary violations (inner layers must not name catalog types directly):\n  - %s", strings.Join(violations, "\n  - "))
	}
}
