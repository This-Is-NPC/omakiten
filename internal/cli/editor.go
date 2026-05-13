package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/domain"
)

// openEditorAndReimport runs $EDITOR (or the user-resolved editor) against
// path, then re-loads + re-imports the bundle so the materialized SQLite store
// reflects whatever the user wrote.
func openEditorAndReimport(ctx context.Context, rt *runtime, path string) error {
	if path == "" {
		return nil
	}
	if err := runEditorCommand(path); err != nil {
		return domain.NewError(domain.ErrEditorFailed, err.Error(), map[string]any{"path": path})
	}
	editor := app.NewBundleEditor(rt.store, configstore.NewWithRepoLocal(rt.repoLocalDir), rt.configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		return err
	}
	return nil
}

func runEditorCommand(path string) error {
	editor := app.ResolveEditor()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return fmt.Errorf("editor not configured")
	}
	args := append(parts[1:], path)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q exited: %w", editor, err)
	}
	return nil
}

// resolveSkillSlug accepts either a bare slug or a numeric SQLite id. Numeric
// ids stay supported as a back-compat fallback for scripts that already track
// them; new code should prefer slugs.
func resolveSkillSlug(ctx context.Context, service *app.SkillService, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, "skill slug is required", nil)
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		skills, err := service.List(ctx)
		if err != nil {
			return "", err
		}
		for _, skill := range skills {
			if skill.ID == id {
				return skill.Key, nil
			}
		}
		return "", domain.NewError(domain.ErrSkillNotFound, "skill not found", map[string]any{"id": id})
	}
	if config.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, "skill slug must be lowercase, hyphenated", map[string]any{"slug": raw})
	}
	return raw, nil
}

func resolveLawSlug(ctx context.Context, service *app.LawService, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, "law slug is required", nil)
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		laws, err := service.List(ctx)
		if err != nil {
			return "", err
		}
		for _, law := range laws {
			if law.ID == id {
				return law.Key, nil
			}
		}
		return "", domain.NewError(domain.ErrLawNotFound, "law not found", map[string]any{"id": id})
	}
	if config.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, "law slug must be lowercase, hyphenated", map[string]any{"slug": raw})
	}
	return raw, nil
}

func resolvePersonaSlug(ctx context.Context, service *app.PersonaService, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, "persona slug is required", nil)
	}
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		personas, err := service.List(ctx)
		if err != nil {
			return "", err
		}
		for _, persona := range personas {
			if persona.ID == id {
				return persona.Key, nil
			}
		}
		return "", domain.NewError(domain.ErrPersonaNotFound, "persona not found", map[string]any{"id": id})
	}
	if config.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, "persona slug must be lowercase, hyphenated", map[string]any{"slug": raw})
	}
	return raw, nil
}
