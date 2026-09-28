package testutil

import (
	"sync"

	"omakiten/internal/config"
	"omakiten/internal/domain"
)

var fixtureEventRegistry = sync.OnceValue(func() *domain.EventRegistry {
	r, err := config.BuildEventRegistry(config.MustLoadKitConfig().Events)
	if err != nil {
		panic(err)
	}
	return r
})

// EventRegistry returns immutable metadata from the bundled test kit.
func EventRegistry() *domain.EventRegistry { return fixtureEventRegistry() }
