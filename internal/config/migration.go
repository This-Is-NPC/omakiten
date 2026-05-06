package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"omakiten/defaults"
)

// configDefaultFilename mirrors paths.DefaultConfigFilename without forcing a
// dependency from the config package onto paths. Keep in sync.
const configDefaultFilename = "omakiten.yaml"

// MigrateLayout normalizes a config root from any prior layout to the current
// one, idempotently. Layout history:
//
//   - v0 (flat, XDG/default mode): omakiten.yaml + entity folders at <root>/.
//   - v1 (early OMAKITEN_HOME mode): yaml + entity folders all under <root>/config/.
//   - v2 (current): <root>/config/omakiten.yaml + entity folders at <root>/<kind>/
//     with a custom/ subtree under each.
//
// Effects on call:
//
//   - If <root>/omakiten.yaml exists, move it to <root>/config/omakiten.yaml.
//   - If <root>/config/<kind>/ exists for a known kind, move its contents up to
//     <root>/<kind>/ (creating the destination if needed).
//   - For each entity kind, files whose filename does not match an embedded
//     default are user-created and get moved into <root>/<kind>/custom/.
//     Default-slug files at the root are left untouched — they will be
//     refreshed by the next install/refresh; the user accepts that direct
//     edits to those files are lost.
//   - Missing custom/ subdirs are created (empty).
//
// Returns nil when the layout is already current (no work to do).
func MigrateLayout(rootDir string) error {
	if err := migrateYAML(rootDir); err != nil {
		return err
	}
	if err := migrateEntityFolders(rootDir); err != nil {
		return err
	}
	if err := segregateUserCustoms(rootDir); err != nil {
		return err
	}
	if err := segregateUserConfigProfiles(rootDir); err != nil {
		return err
	}
	if err := migrateLegacyTemplateBinding(rootDir); err != nil {
		return err
	}
	return nil
}

func migrateYAML(rootDir string) error {
	legacy := filepath.Join(rootDir, "omakiten.yaml")
	current := filepath.Join(rootDir, "config", "omakiten.yaml")

	if _, err := os.Stat(legacy); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(current); err == nil {
		// New location already populated — drop the stale flat copy.
		return os.Remove(legacy)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Join(rootDir, "config"), 0o755); err != nil {
		return fmt.Errorf("create config/: %w", err)
	}
	if err := os.Rename(legacy, current); err != nil {
		return fmt.Errorf("move omakiten.yaml: %w", err)
	}
	return nil
}

func migrateEntityFolders(rootDir string) error {
	for _, kind := range entityFolders {
		legacyDir := filepath.Join(rootDir, "config", kind)
		currentDir := filepath.Join(rootDir, kind)

		entries, err := os.ReadDir(legacyDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read legacy %s: %w", legacyDir, err)
		}
		if len(entries) == 0 {
			_ = os.Remove(legacyDir)
			continue
		}

		if err := os.MkdirAll(currentDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", currentDir, err)
		}
		for _, entry := range entries {
			from := filepath.Join(legacyDir, entry.Name())
			to := filepath.Join(currentDir, entry.Name())
			// New layout wins: if a same-named target already exists at the
			// current location, drop the legacy copy so the source-of-truth is
			// unambiguous. Sub-directories at the legacy root are recursively
			// removed (they belong to the old shape and have no role in v2).
			if _, err := os.Stat(to); err == nil {
				if err := os.RemoveAll(from); err != nil {
					return fmt.Errorf("remove redundant legacy %s: %w", from, err)
				}
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("move %s -> %s: %w", from, to, err)
			}
		}
		// Now that all entries have been either moved or removed, the legacy dir
		// should be empty. Best-effort cleanup; ignore errors.
		_ = os.Remove(legacyDir)
	}
	return nil
}

func segregateUserCustoms(rootDir string) error {
	for _, kind := range entityFolders {
		dir := filepath.Join(rootDir, kind)
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}

		defaultsSet, err := embeddedDefaultFilenames(kind)
		if err != nil {
			return err
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read %s: %w", dir, err)
		}
		customDir := filepath.Join(dir, "custom")
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if _, isDefault := defaultsSet[name]; isDefault {
				continue
			}
			// User-created entry → move into custom/.
			if err := os.MkdirAll(customDir, 0o755); err != nil {
				return fmt.Errorf("create %s/custom: %w", dir, err)
			}
			from := filepath.Join(dir, name)
			to := filepath.Join(customDir, name)
			if _, err := os.Stat(to); err == nil {
				// Custom already has a file with this name — leave both alone.
				continue
			} else if !os.IsNotExist(err) {
				return err
			}
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("move %s -> custom/: %w", from, err)
			}
		}
		// Always make sure custom/ exists, even when nothing got moved, so the
		// user has a clear place to drop new files.
		if err := os.MkdirAll(customDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", customDir, err)
		}
	}
	return nil
}

// migrateLegacyTemplateBinding rewrites the v3 template binding (frontmatter
// `default: task` on the template file itself) from the v2 binding
// (`config.templates.task: <slug>` in omakiten.yaml). Idempotent: if the
// yaml has no legacy key, returns nil. Permissive YAML parse is used here
// because the strict loader would reject the unknown `templates` field
// after the schema removal.
func migrateLegacyTemplateBinding(rootDir string) error {
	yamlPath := filepath.Join(rootDir, "config", "omakiten.yaml")
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", yamlPath, err)
	}

	slug, found := extractLegacyTemplateBinding(raw)
	if !found {
		return nil
	}

	// Locate the template file in either templates/<slug>.md (default) or
	// templates/custom/<slug>.md (user). The migration writes back to
	// whichever exists; if both exist, custom wins to mirror the loader.
	candidates := []string{
		filepath.Join(rootDir, "templates", "custom", slug+".md"),
		filepath.Join(rootDir, "templates", slug+".md"),
	}
	target := ""
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			target = c
			break
		}
	}
	if target == "" {
		// No template file matches the legacy binding — drop the key and
		// skip the frontmatter rewrite. Bundle validation later will report
		// the dangling reference if anything else points at the slug.
		return rewriteWithoutLegacyTemplateBinding(yamlPath, raw)
	}

	if err := writeDefaultIntoTemplateFrontmatter(target, "task"); err != nil {
		return err
	}
	return rewriteWithoutLegacyTemplateBinding(yamlPath, raw)
}

// extractLegacyTemplateBinding scans the raw yaml for the v2-era
// `config.templates.task: <slug>` line. Returns the slug (trimmed) and true
// when found. Permissive on whitespace and quoting; works on the formatted
// output our saver produces.
func extractLegacyTemplateBinding(raw []byte) (string, bool) {
	inConfig := false
	inTemplates := false
	configIndent := -1
	templatesIndent := -1

	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		indent := leadingSpaces(trimmed)
		stripped := strings.TrimSpace(trimmed)
		if strings.HasPrefix(stripped, "#") {
			continue
		}
		if !inConfig {
			if indent == 0 && strings.HasPrefix(stripped, "config:") {
				inConfig = true
				configIndent = indent
			}
			continue
		}
		if indent <= configIndent {
			inConfig = false
			inTemplates = false
			// Re-evaluate this line as a top-level entry.
			if indent == 0 && strings.HasPrefix(stripped, "config:") {
				inConfig = true
				configIndent = indent
			}
			continue
		}
		if !inTemplates {
			if strings.HasPrefix(stripped, "templates:") {
				inTemplates = true
				templatesIndent = indent
			}
			continue
		}
		if indent <= templatesIndent {
			inTemplates = false
			continue
		}
		if strings.HasPrefix(stripped, "task:") {
			value := strings.TrimSpace(strings.TrimPrefix(stripped, "task:"))
			value = strings.Trim(value, "\"'")
			if value != "" {
				return value, true
			}
		}
	}
	return "", false
}

func leadingSpaces(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

// rewriteWithoutLegacyTemplateBinding strips the `templates:` block (and its
// `task:` child) from the `config:` section and writes the result back. Done
// line-by-line rather than yaml.Marshal so comments and field order in the
// rest of the file are preserved.
func rewriteWithoutLegacyTemplateBinding(yamlPath string, raw []byte) error {
	lines := strings.Split(string(raw), "\n")
	out := make([]string, 0, len(lines))
	inTemplates := false
	templatesIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if inTemplates {
			indent := leadingSpaces(trimmed)
			stripped := strings.TrimSpace(trimmed)
			if stripped == "" {
				out = append(out, trimmed)
				continue
			}
			if indent > templatesIndent {
				continue // child of templates, drop
			}
			inTemplates = false
		}
		stripped := strings.TrimSpace(trimmed)
		if strings.HasPrefix(stripped, "templates:") && !strings.HasPrefix(stripped, "templates: [") && !strings.HasPrefix(stripped, "template_defaults:") {
			indent := leadingSpaces(trimmed)
			// Only treat as the legacy block when indent > 0 (under config:).
			if indent > 0 {
				inTemplates = true
				templatesIndent = indent
				continue
			}
		}
		out = append(out, trimmed)
	}
	return WriteAtomic(yamlPath, []byte(strings.Join(out, "\n")))
}

// writeDefaultIntoTemplateFrontmatter mutates the .md file in place,
// inserting/updating the `default:` field in its YAML frontmatter. Body
// content and other frontmatter keys are preserved.
func writeDefaultIntoTemplateFrontmatter(path, defaultKind string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	fm, body, err := SplitFrontmatter(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	// Replace any existing default: line; otherwise append at the end of the
	// frontmatter block. Preserve other keys exactly so user-authored
	// formatting (comments, ordering) survives.
	lines := strings.Split(strings.TrimRight(string(fm), "\n"), "\n")
	wrote := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "default:") {
			lines[i] = fmt.Sprintf("default: %s", defaultKind)
			wrote = true
			break
		}
	}
	if !wrote {
		lines = append(lines, fmt.Sprintf("default: %s", defaultKind))
	}

	updated := JoinFrontmatter([]byte(strings.Join(lines, "\n")+"\n"), body)
	return WriteAtomic(path, updated)
}

// segregateUserConfigProfiles relocates yaml profiles other than the
// canonical default into <config-dir>/custom, mirroring the entity folder
// convention. The canonical default `omakiten.yaml` stays at the root (it
// is overwritten by every refresh); state files (`.active`) and any non-yaml
// content at the root are left untouched.
func segregateUserConfigProfiles(rootDir string) error {
	configDir := filepath.Join(rootDir, "config")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", configDir, err)
	}
	customDir := filepath.Join(configDir, "custom")
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == configDefaultFilename {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".yaml") {
			continue
		}
		if err := os.MkdirAll(customDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", customDir, err)
		}
		from := filepath.Join(configDir, name)
		to := filepath.Join(customDir, name)
		if _, err := os.Stat(to); err == nil {
			// Custom already has a profile with this name — leave the root copy
			// where it is to avoid surprising the user. Cleanup is manual.
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return fmt.Errorf("move %s -> custom/: %w", from, err)
		}
	}
	// Always make sure custom/ exists so the user has somewhere obvious to drop
	// new profiles, even when nothing got moved.
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", customDir, err)
	}
	return nil
}

func embeddedDefaultFilenames(kind string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	entries, err := defaults.FS.ReadDir(kind)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return out, nil
		}
		return nil, fmt.Errorf("read embedded %s: %w", kind, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		out[entry.Name()] = struct{}{}
	}
	return out, nil
}
