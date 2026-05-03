package app

import (
	"context"
	"path/filepath"
	"strings"

	"omakiten/internal/domain"
)

type ProjectService struct {
	repo ProjectRepository
}

func NewProjectService(repo ProjectRepository) *ProjectService {
	return &ProjectService{repo: repo}
}

func (s *ProjectService) Init(ctx context.Context, name, slug, rootPath string) (domain.Project, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return domain.Project{}, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(absRoot)
	}

	slug = normalizeSlug(slug)
	if slug == "" {
		slug = normalizeSlug(name)
	}
	if slug == "" {
		return domain.Project{}, domain.NewError(domain.ErrValidation, "project slug is required", nil)
	}

	return s.repo.UpsertProject(ctx, name, slug, absRoot)
}

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
