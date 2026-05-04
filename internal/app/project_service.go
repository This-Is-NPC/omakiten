package app

import (
	"context"
	"path/filepath"
	"strings"

	"omakiten/internal/activity"
	"omakiten/internal/domain"
)

type ProjectService struct {
	repo ProjectRepository
}

func NewProjectService(repo ProjectRepository) *ProjectService {
	return &ProjectService{repo: repo}
}

func (s *ProjectService) Init(ctx context.Context, name, slug, rootPath string) (project domain.Project, err error) {
	finish := activity.Track(ctx, "app.ProjectService.Init", domain.ProjectContext{}, map[string]any{"slug": slug, "root": rootPath})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return
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
		err = domain.NewError(domain.ErrValidation, "project slug is required", nil)
		return
	}

	project, err = s.repo.UpsertProject(ctx, name, slug, absRoot)
	return
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
