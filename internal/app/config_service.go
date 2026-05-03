package app

import (
	"context"

	"omakiten/internal/config"
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
		return config.Bundle{}, "", err
	}

	hash, err := config.HashFile(path)
	if err != nil {
		return config.Bundle{}, "", err
	}

	if err := s.repo.ImportBundle(ctx, bundle, path, hash); err != nil {
		return config.Bundle{}, "", err
	}

	return bundle, hash, nil
}
