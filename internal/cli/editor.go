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
	editor := app.NewBundleEditor(configstore.New(), rt.configPath)
	if _, err := editor.Apply(ctx, nil); err != nil {
		return err
	}
	return nil
}

func runEditorCommand(path string) error {
	editor := app.ResolveEditor()
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_configured"), nil)
	}
	resolved, err := resolveEditorBinary(parts[0])
	if err != nil {
		return err
	}
	args := append(parts[1:], path)
	cmd := exec.Command(resolved, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q exited: %w", editor, err)
	}
	return nil
}

// resolveEditorBinary mirrors the hook exec guard: pin argv[0] to an
// absolute on-disk path before spawning so a PATH lookup at fork time
// cannot pick up a shadow binary the user did not approve. Absolute
// paths pass through; bare names round-trip through exec.LookPath.
// Relative paths with embedded separators are rejected — the CLI
// resolves editor names against the user's PATH, never against CWD.
func resolveEditorBinary(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_configured"), nil)
	}
	if strings.ContainsRune(trimmed, os.PathSeparator) {
		// Absolute path — keep verbatim. Relative paths with embedded
		// separators are rejected outright so the editor surface does
		// not silently inherit CWD-relative resolution semantics.
		if !strings.HasPrefix(trimmed, string(os.PathSeparator)) {
			return "", domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_found"), map[string]any{"editor": trimmed})
		}
		return trimmed, nil
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", domain.NewError(domain.ErrEditorNotFound, t("cli.editor.not_found"), map[string]any{"editor": trimmed, "error": domain.SafeError(err)})
	}
	return resolved, nil
}

// resolveSkillSlug accepts either a bare slug or a numeric SQLite id. Numeric
// ids stay supported as a back-compat fallback for scripts that already track
// them; new code should prefer slugs.
func resolveSkillSlug(ctx context.Context, service *app.SkillService, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, t("cli.err.skill_slug_required"), nil)
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
		return "", domain.NewError(domain.ErrSkillNotFound, t("cli.err.skill_not_found"), map[string]any{"id": id})
	}
	if config.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, t("cli.err.skill_slug_bad_format"), map[string]any{"slug": raw})
	}
	return raw, nil
}

func resolveLawSlug(ctx context.Context, service *app.LawService, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, t("cli.err.law_slug_required"), nil)
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
		return "", domain.NewError(domain.ErrLawNotFound, t("cli.err.law_not_found"), map[string]any{"id": id})
	}
	if config.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, t("cli.err.law_slug_bad_format"), map[string]any{"slug": raw})
	}
	return raw, nil
}

func resolvePersonaSlug(ctx context.Context, service *app.PersonaService, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", domain.NewError(domain.ErrValidation, t("cli.err.persona_slug_required"), nil)
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
		return "", domain.NewError(domain.ErrPersonaNotFound, t("cli.err.persona_not_found"), map[string]any{"id": id})
	}
	if config.Slugify(raw) != raw {
		return "", domain.NewError(domain.ErrValidation, t("cli.err.persona_slug_bad_format"), map[string]any{"slug": raw})
	}
	return raw, nil
}
