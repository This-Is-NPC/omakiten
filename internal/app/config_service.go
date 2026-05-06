package app

import (
	"context"
	"fmt"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type ConfigService struct {
	repo   ConfigRepository
	bundle BundleStore
}

func NewConfigService(repo ConfigRepository, bundle BundleStore) *ConfigService {
	return &ConfigService{repo: repo, bundle: bundle}
}

func (s *ConfigService) Import(ctx context.Context, path string) (bundle config.Bundle, hash string, err error) {
	finish := activity.Track(ctx, "app.ConfigService.Import", domain.ProjectContext{}, map[string]any{"path": path})
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = err.Error()
		}
		finish(status, errMsg)
	}()

	bundle, err = s.bundle.LoadBundle(path)
	if err != nil {
		err = configError(path, err)
		return
	}

	hash, err = s.bundle.HashFile(path)
	if err != nil {
		err = configError(path, err)
		return
	}

	if err = s.repo.ImportBundle(ctx, bundle, path, hash); err != nil {
		return
	}

	return
}

func configError(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
}
