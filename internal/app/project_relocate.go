package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// Relocate validates the new directory before changing its registration.
func (s *ProjectService) Relocate(ctx context.Context, id int64, previousRoot, root string) (domain.Project, error) {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return domain.Project{}, domain.NewError(domain.ErrValidation, "root_path must be an absolute directory path", nil)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return domain.Project{}, domain.NewError(domain.ErrValidation, "root_path must name an existing accessible directory", map[string]any{"root_path": root})
	}
	if err := paths.ValidateNoSymlinkComponents(root); err != nil {
		return domain.Project{}, domain.NewError(domain.ErrValidation, "root_path must not contain symlinks", map[string]any{"root_path": root})
	}
	return s.repo.UpdateProjectRootPath(ctx, id, previousRoot, root)
}
