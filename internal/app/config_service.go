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

	// Wire the resolved priority + severity tables into the domain
	// registries BEFORE writing the bundle to SQLite. ImportBundle's
	// adapter (sqlite/bundles.go) resolves frontmatter severity labels
	// to ids via the registry; without this step the import errors
	// out on the first law. Validator already passed in LoadBundle
	// above, so the tables are guaranteed non-empty and well-formed.
	registry = registerEnumsFromBundle(bundle)

	if err = s.repo.ImportBundle(ctx, bundle, path, hash); err != nil {
		return
	}

	return
}

// registerEnumsFromBundle installs the bundle's priority + severity
// tables into the domain registries and returns an instance-scoped
// EnumRegistry. It still updates the process globals for backward
// compatibility with code that has not yet migrated to injected registries.
// The returned registry should be used by new code; the globals are
// deprecated and will be removed in a future refactor.
func registerEnumsFromBundle(bundle config.Bundle) *domain.EnumRegistry {
	priorityPairs := make([]domain.PriorityPair, len(bundle.Config.Priorities))
	for i, p := range bundle.Config.Priorities {
		priorityPairs[i] = domain.PriorityPair{ID: p.ID, Value: p.Value, Default: p.Default}
	}
	domain.RegisterPriorities(priorityPairs)

	severityPairs := make([]domain.SeverityPair, len(bundle.Config.Severities))
	for i, s := range bundle.Config.Severities {
		severityPairs[i] = domain.SeverityPair{ID: s.ID, Value: s.Value, Default: s.Default}
	}
	domain.RegisterSeverities(severityPairs)

	return domain.NewEnumRegistry(priorityPairs, severityPairs)
}

func configError(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
}
