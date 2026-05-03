package app

import (
	"context"
	"fmt"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type ConfigService struct {
	repo ConfigRepository
}

func NewConfigService(repo ConfigRepository) *ConfigService {
	return &ConfigService{repo: repo}
}

func (s *ConfigService) Import(ctx context.Context, path string) (config.Bundle, string, error) {
	bundle, err := config.LoadBundle(path)
	if err != nil {
		return config.Bundle{}, "", configError(path, err)
	}

	hash, err := config.HashFile(path)
	if err != nil {
		return config.Bundle{}, "", configError(path, err)
	}

	if err := s.repo.ImportBundle(ctx, bundle, path, hash); err != nil {
		return config.Bundle{}, "", err
	}

	return bundle, hash, nil
}

func configError(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
}
