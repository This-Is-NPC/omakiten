package app

import (
	"context"
	"fmt"

	"omakiten/internal/activity"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

type ConfigService struct {
	bundle BundleStore
}

// NewConfigService wires the read-only config parser. Import only loads
// and parses the bundle; per-project rebuild is driven by
// BundleCache.Reload triggered by the bundle's mtime change, never by a
// Store-side write.
func NewConfigService(bundle BundleStore) *ConfigService {
	return &ConfigService{bundle: bundle}
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
	// agent agent) threads the registry into the services it constructs;
	// no process-global state is touched.
	registry = config.BuildEnumRegistry(bundle)

	return
}

func configError(path string, err error) error {
	return domain.NewError(domain.ErrConfigInvalid, "config is invalid", map[string]any{"path": path, "error": fmt.Sprint(err)})
}
