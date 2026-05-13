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

func (s *ConfigService) Import(ctx context.Context, path string) (bundle config.Bundle, hash string, registry *domain.EnumRegistry, err error) {
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

	// Build an instance-scoped EnumRegistry from the bundle's priority +
	// severity tables. Returned to the caller so each surface (CLI, TUI,
	// MCP agent) threads the registry into the services it constructs;
	// no process-global state is touched. The SQLite layer builds its own
	// registry from the bundle directly (see sqlite/bundles.go
	// buildRegistryFromBundle) for the law import path.
	registry = enumRegistryFromBundle(bundle)

	if err = s.repo.ImportBundle(ctx, bundle, path, hash); err != nil {
		return
	}

	return
}

// enumRegistryFromBundle builds an instance-scoped EnumRegistry from the
// bundle's priority and severity tables. No process-global state involved.
func enumRegistryFromBundle(bundle config.Bundle) *domain.EnumRegistry {
	priorityPairs := make([]domain.PriorityPair, len(bundle.Config.Priorities))
	for i, p := range bundle.Config.Priorities {
		priorityPairs[i] = domain.PriorityPair{ID: p.ID, Value: p.Value, Default: p.Default}
	}
	severityPairs := make([]domain.SeverityPair, len(bundle.Config.Severities))
	for i, s := range bundle.Config.Severities {
		severityPairs[i] = domain.SeverityPair{ID: s.ID, Value: s.Value, Default: s.Default}
	}
	return domain.NewEnumRegistry(priorityPairs, severityPairs)
}

func configError(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
}
