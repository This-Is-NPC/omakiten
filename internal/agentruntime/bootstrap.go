package agentruntime

import (
	"context"

	"omakiten/internal/config"
	"omakiten/internal/configstore"
	"omakiten/internal/contract"
	"omakiten/internal/events"
	"omakiten/internal/sqlite"
)

// Bootstrap wires the shared event bus and the initial project runtime.
func Bootstrap(ctx context.Context, store *sqlite.Store, files *configstore.Adapter, path string, selector contract.ProjectSelector, bundle config.Bundle) (*BundleCache, events.Bus, *ProjectRuntime, error) {
	bus := events.NewInProcessBus(bundle.Config.Events)
	cache := NewBundleCache(store, bus, files)
	cache.SetProjectSelector(selector)
	runtime, err := cache.Resolve(ctx, selector.ProjectID, path)
	if err != nil {
		_ = cache.Close()
		return nil, nil, nil, err
	}
	return cache, bus, runtime, nil
}
